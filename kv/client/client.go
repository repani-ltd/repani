// Package client provides the ASCON-KV v1 client: Put/Get/Delete on a
// local encrypted store, plus a paged sync against a server. Every frame
// it sends is a single AEAD seal; reconciliation is PROBE / PULL / PUSH
// as defined in PROTOCOL.t.
package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"repani.com/ascon"
	"repani.com/kv/store"
	"repani.com/kv/vault"
	"repani.com/kv/wire"
)

// fmtRaw is the value-encoding tag for a verbatim value (CLIENT.t).
const fmtRaw = 0x00

// FmtRow is the value-encoding tag for a ROW v1 row (CLIENT.t,
// repani.com/row): PutFmt(key, row.Encode(...), FmtRow).
const FmtRow = 0x01

// WrapValue prepends the CLIENT.t format tag to a value, producing
// the bytes stored inside an entry. Exported for owners that seal
// entries directly (vault.Seal) rather than through a Client.
func WrapValue(fmtTag byte, value []byte) []byte {
	wrapped := make([]byte, 1+len(value))
	wrapped[0] = fmtTag
	copy(wrapped[1:], value)
	return wrapped
}

// ParseValue splits a stored entry value into its format tag and
// payload. An empty stored value is a tombstone (no tag). The inverse
// of WrapValue.
func ParseValue(stored []byte) (fmtTag byte, value []byte, tomb bool) {
	if len(stored) == 0 {
		return 0, nil, true
	}
	return stored[0], stored[1:], false
}

// Transport sends a marshaled request frame and returns the response
// bytes (a sealed RESP frame, or a single pre-channel error byte).
type Transport interface {
	Do(ctx context.Context, request []byte) (response []byte, err error)
}

// Client is the writer + reader + syncer for one vault.
type Client struct {
	aead    *ascon.AEAD
	authKey vault.AuthKey
	vid     vault.VaultID
	c2s     vault.ChannelKey
	s2c     vault.ChannelKey
	store   store.Store
	tran    Transport

	nowMs    func() uint64
	regChan  *vault.ChannelKey // K_reg; nil when the client holds no RegisterKey
	windowMs uint64            // reconcile look-back window
	trace    func(string)      // nil = off

	mu         sync.Mutex
	lastTs     uint64
	registered bool          // belief that the server holds the vault
	recent     []vault.Entry // entries written since the last sync (fast-path push)

	viewMu sync.Mutex
	view   map[string]keyValue // the merged view, by key (foldLocked)
	folded int                 // how many of the store's entries view has taken
}

// Options configure a Client. The zero value holds no RegisterKey,
// registers before its first sync only if it holds one, uses the
// default window, and traces nothing.
type Options struct {
	// RegisterKey seals a REGISTER. Without one, a client syncs only
	// with a vault that already exists on the server.
	RegisterKey *vault.RegisterKey
	// Registered says the server is known to hold the vault, sparing
	// the first REGISTER; Sync registers anyway on UNKNOWN_VAULT.
	Registered bool
	// SyncWindow is the reconcile look-back (CLIENT.t); 0 is 10 minutes.
	SyncWindow time.Duration
	// Trace receives one line per piece of sync traffic
	// (REGISTER/PROBE/PULL/PUSH) and a closing STATS line.
	Trace func(string)
}

// defaultSyncWindowMs is the reconcile look-back: on a plain sync a
// client walks [frontier - window, ∞). 10 minutes is ample for an
// auto-syncing vault; anything older falls back to a full walk.
const defaultSyncWindowMs = 10 * 60 * 1000

// New returns a Client backed by the given store and transport.
func New(rk vault.RootKey, s store.Store, t Transport, opts Options) (*Client, error) {
	aead, err := rk.AEAD()
	if err != nil {
		return nil, err
	}
	ak := vault.DeriveAuthKey(rk)
	c := &Client{
		aead:       aead,
		authKey:    ak,
		vid:        vault.DeriveVaultID(rk),
		c2s:        vault.DeriveC2S(ak),
		s2c:        vault.DeriveS2C(ak),
		store:      s,
		tran:       t,
		nowMs:      func() uint64 { return uint64(time.Now().UnixMilli()) },
		windowMs:   defaultSyncWindowMs,
		trace:      opts.Trace,
		registered: opts.Registered,
		view:       make(map[string]keyValue),
	}
	if opts.RegisterKey != nil {
		k := vault.DeriveRegKey(*opts.RegisterKey)
		c.regChan = &k
	}
	if opts.SyncWindow > 0 {
		c.windowMs = uint64(opts.SyncWindow.Milliseconds())
	}
	return c, nil
}

// VaultID returns the public vault handle.
func (c *Client) VaultID() vault.VaultID { return c.vid }

// Store returns the backing store (read-only inspection).
func (c *Client) Store() store.Store { return c.store }

func (c *Client) tracef(format string, args ...any) {
	if c.trace != nil {
		c.trace(fmt.Sprintf(format, args...))
	}
}

// syncStats accounts one Sync call, for the tracer's closing line.
type syncStats struct {
	RoundTrips  int // frames sent
	ReqBytes    int // total request frame bytes
	RespBytes   int // total response bytes
	Pushed      int // entries sent to the server (incl. dedup no-ops)
	PushedBytes int // marshaled bytes of those entries
	Pulled      int // entries received from the server
	PulledBytes int // marshaled bytes of those entries
}

// --- local writes ---

// Put writes (key, value) with the raw format tag. The stored value is
// fmt(1)||value.
func (c *Client) Put(key, value []byte) error {
	return c.PutFmt(key, value, fmtRaw)
}

// PutFmt writes (key, value) under an explicit format tag (CLIENT.t:
// 0x00 raw and 0x01 FmtRow are assigned). The tag is
// stored inside the entry ciphertext and surfaces on reads as
// LiveEntry.Fmt.
func (c *Client) PutFmt(key, value []byte, fmtTag byte) error {
	if len(value) == 0 {
		return errors.New("client: empty value is a tombstone; use Delete")
	}
	wrapped := WrapValue(fmtTag, value)

	ts := c.nextTs()
	e, err := vault.Seal(c.aead, ts, key, wrapped)
	if err != nil {
		return err
	}
	return c.record(e)
}

// PutMany writes every key in kvs with its value, raw, as one durable
// batch: one write and one fsync however many keys -- the bulk form of
// Put, for imports. Nothing is written if any value is empty.
func (c *Client) PutMany(kvs map[string][]byte) error {
	es := make([]vault.Entry, 0, len(kvs))
	for k, v := range kvs {
		if len(v) == 0 {
			return fmt.Errorf("client: %q: empty value is a tombstone; use Delete", k)
		}
		e, err := vault.Seal(c.aead, c.nextTs(), []byte(k), WrapValue(fmtRaw, v))
		if err != nil {
			return fmt.Errorf("client: %q: %w", k, err)
		}
		es = append(es, e)
	}
	return c.record(es...)
}

// Delete writes a tombstone (an empty value, with no fmt tag).
func (c *Client) Delete(key []byte) error {
	ts := c.nextTs()
	e, err := vault.Seal(c.aead, ts, key, nil)
	if err != nil {
		return err
	}
	return c.record(e)
}

// record adds locally-created entries to the store, as one durable
// batch, and remembers them for the next sync's fast-path push.
func (c *Client) record(es ...vault.Entry) error {
	if _, err := c.store.Add(es...); err != nil {
		return err
	}
	c.mu.Lock()
	c.recent = append(c.recent, es...)
	c.mu.Unlock()
	return nil
}

// nextTs returns a strictly-increasing millisecond timestamp per Client.
func (c *Client) nextTs() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.nowMs()
	if now <= c.lastTs {
		now = c.lastTs + 1
	}
	c.lastTs = now
	return now
}

// --- reads (last-writer-wins merge) ---

// keyValue is a key's winning entry in the view. An empty stored value
// is a tombstone; a non-empty one is fmt(1)||payload, and value is the
// payload.
type keyValue struct {
	value []byte
	fmt   byte
	ts    uint64
	id    vault.EntryID
	tomb  bool
}

// foldLocked brings the view up to the store: the Merge of PROTOCOL.t,
// kept rather than rebuilt. It decrypts only the entries the store
// gained since the last read (Store.Appended), each taking its key when
// it comes later in canonical order than the key's current winner --
// the same result as replaying the whole set, since a key's value
// depends only on its latest entry. An entry that fails to open is
// skipped, stays in the store so the set still matches the server's,
// and is never tried again. c.viewMu held.
func (c *Client) foldLocked() {
	for _, e := range c.store.Appended(c.folded) {
		c.folded++
		k, sv, err := vault.Open(c.aead, e)
		if err != nil {
			continue
		}
		id := e.ID()
		if cur, ok := c.view[string(k)]; ok && !posAfter(e.TsMs, id, cur.ts, cur.id) {
			continue
		}
		fmtTag, value, tomb := ParseValue(sv)
		c.view[string(k)] = keyValue{value: value, fmt: fmtTag, ts: e.TsMs, id: id, tomb: tomb}
	}
}

// Get returns the current value for key.
func (c *Client) Get(key []byte) (value []byte, ok bool, err error) {
	c.viewMu.Lock()
	defer c.viewMu.Unlock()
	c.foldLocked()
	kv, found := c.view[string(key)]
	if !found || kv.tomb {
		return nil, false, nil
	}
	return bytes.Clone(kv.value), true, nil
}

// List returns the sorted live keys.
func (c *Client) List() ([]string, error) {
	all, err := c.All()
	out := make([]string, len(all))
	for i, e := range all {
		out[i] = string(e.Key)
	}
	return out, err
}

// LiveEntry is a live (key, value) pair. Fmt is the value's format tag
// (CLIENT.t); Value is the payload with the tag already stripped; Ts
// is the winning entry's ts_ms.
type LiveEntry struct {
	Key   []byte
	Value []byte
	Fmt   byte
	Ts    uint64
}

// All returns every live (key, value) pair, sorted by key.
func (c *Client) All() ([]LiveEntry, error) {
	c.viewMu.Lock()
	defer c.viewMu.Unlock()
	c.foldLocked()
	var out []LiveEntry
	for _, k := range slices.Sorted(maps.Keys(c.view)) {
		if kv := c.view[k]; !kv.tomb {
			out = append(out, LiveEntry{Key: []byte(k), Value: bytes.Clone(kv.value), Fmt: kv.fmt, Ts: kv.ts})
		}
	}
	return out, nil
}

// --- sync ---

// ErrSyncStuck is returned if sync fails to converge within its round
// budget (e.g. persistent concurrent churn, or a server that keeps
// forgetting the registration).
var ErrSyncStuck = errors.New("client: sync did not converge within round budget")

// ErrNoRegisterKey is returned when a sync needs to register (the
// server does not know the vault) but no register key is installed.
// On a registration-disabled server this means the vault has not been
// created by the owner.
var ErrNoRegisterKey = errors.New("client: no register key; cannot register with a gated server")

// Sync reconciles the client's vault with the server. Registration is
// reactive: a client that believes itself unregistered registers first
// only when a register key is installed; without one it proceeds
// optimistically -- correct against a vault that already exists on the
// server (e.g. a published read-only vault) -- and fails with
// ErrNoRegisterKey only if the server answers UNKNOWN_VAULT. Then:
//   - Fast path: pushes exactly the entries written since the last sync
//     and PROBEs. If those were the only divergence the probe matches
//     and sync is done — no pull, no walk.
//   - Otherwise it walks a suffix window (PULL then dedup-tolerant PUSH),
//     widening from a recent window to a full walk across rounds until a
//     PROBE matches.
//
// Windowing is client-local: the server only ever sees the cursor the
// client chooses to send.
//
// A failed Sync loses nothing: the entries it was to fast-push go back
// ahead of any written since, for the next Sync's fast path.
func (c *Client) Sync(ctx context.Context) (err error) {
	const maxRounds = 64
	st := &syncStats{}
	defer c.reportStats(st)

	c.mu.Lock()
	recent := c.recent
	c.recent = nil
	c.mu.Unlock()
	defer func() {
		if err != nil && len(recent) > 0 {
			c.mu.Lock()
			c.recent = append(recent, c.recent...)
			c.mu.Unlock()
		}
	}()

	for round := range maxRounds {
		inSync, err := c.round(ctx, round, recent, st)
		if errors.Is(err, wire.ErrUnknownVault) {
			if err := c.unknownVault(); err != nil {
				return err
			}
			continue
		}
		if err != nil || inSync {
			return err
		}
		// Loop back to PROBE to confirm convergence (and catch any
		// entries a concurrent writer added mid-walk).
	}
	return ErrSyncStuck
}

// round runs one round of the reference sync (CLIENT.t): REGISTER when
// needed, the fast-path push on round 0, PROBE, and on a difference
// the window's PULL and PUSH. It reports whether the PROBE matched.
func (c *Client) round(ctx context.Context, round int, recent []vault.Entry, st *syncStats) (inSync bool, err error) {
	c.mu.Lock()
	registered := c.registered
	c.mu.Unlock()
	if !registered && c.regChan != nil {
		if err := c.register(ctx, st); err != nil {
			return false, err
		}
	}
	// Fast path: push just-written entries before the first probe.
	if round == 0 && len(recent) > 0 {
		if err := c.pushEntries(ctx, recent, st); err != nil {
			return false, err
		}
	}
	if inSync, err := c.probe(ctx, st); err != nil || inSync {
		return inSync, err
	}
	anchorTs, anchorID := c.reconcileAnchor(round)
	c.tracef("RECONCILE round %d: window from ts=%d", round, anchorTs)
	if err := c.pullFrom(ctx, anchorTs, anchorID, st); err != nil {
		return false, err
	}
	// The pull is often the whole difference (another device wrote): a
	// PROBE, one fixed-size frame, then spares pushing the window back.
	if inSync, err := c.probe(ctx, st); err != nil || inSync {
		return inSync, err
	}
	return false, c.pushFrom(ctx, anchorTs, anchorID, st)
}

// reconcileAnchor returns the walk anchor for a reconcile round: round 0
// looks back one window below the client's frontier (its newest
// canonical position); any later round is a full walk from zero. A fresh
// or small-timestamp store also anchors at zero.
func (c *Client) reconcileAnchor(round int) (uint64, vault.EntryID) {
	if round != 0 {
		return 0, vault.EntryID{}
	}
	c.mu.Lock()
	w := c.windowMs
	c.mu.Unlock()
	if newest, ok := c.store.Newest(); ok && newest.TsMs > w {
		return newest.TsMs - w, vault.EntryID{}
	}
	return 0, vault.EntryID{}
}

// unknownVault handles a pre-channel UNKNOWN_VAULT: with a register
// key the next round re-registers; without one the sync cannot
// proceed.
func (c *Client) unknownVault() error {
	if c.regChan == nil {
		return ErrNoRegisterKey
	}
	c.mu.Lock()
	c.registered = false
	c.mu.Unlock()
	return nil
}

// register establishes the channel by adopting the client's AuthKey on
// the server, sealed under K_reg.
func (c *Client) register(ctx context.Context, st *syncStats) error {
	if c.regChan == nil {
		return ErrNoRegisterKey
	}
	resp, err := c.roundtrip(ctx, "register", wire.KindRegister, *c.regChan, c.authKey[:], st)
	if errors.Is(err, wire.ErrBadAuth) {
		return errors.New("client: registration rejected — wrong register key")
	}
	if err != nil {
		return err
	}
	if resp.Status != wire.StatusOK {
		return fmt.Errorf("client: register status %s", resp.Status)
	}
	c.tracef("REGISTER -> ok")
	c.mu.Lock()
	c.registered = true
	c.mu.Unlock()
	return nil
}

func (c *Client) probe(ctx context.Context, st *syncStats) (inSync bool, err error) {
	payload := wire.SyncReq{Op: wire.OpProbe, SetHash: c.store.SetHash()}.Marshal()
	resp, err := c.roundtrip(ctx, "probe", wire.KindSync, c.c2s, payload, st)
	if err != nil {
		return false, err
	}
	switch resp.Status {
	case wire.StatusOK:
		c.tracef("PROBE -> in sync")
		return true, nil
	case wire.StatusDiffer:
		c.tracef("PROBE -> differ")
		return false, nil
	default:
		return false, fmt.Errorf("client: probe status %s", resp.Status)
	}
}

// maxPullPages bounds a single pull walk. Loop protection: an honest
// server terminates the walk with an empty page once its set is
// exhausted, and the cursor advances strictly on every page, so this cap
// is only reached by a server fabricating an endless stream — an
// absolute backstop against a hung client.
const maxPullPages = 1 << 20

// pullFrom walks the server's set from the anchor cursor, committing each
// page. It stores every returned entry verbatim (even one that fails to
// open) so the client's set matches the server's. The pull cursor MUST
// advance on every non-empty page; a server that returns a page not
// beyond the cursor is misbehaving and is rejected rather than looped on.
func (c *Client) pullFrom(ctx context.Context, anchorTs uint64, anchorID vault.EntryID, st *syncStats) error {
	curTs, curID := anchorTs, anchorID
	for page := 0; ; page++ {
		if page >= maxPullPages {
			return errors.New("client: pull exceeded page budget (misbehaving server)")
		}
		payload := wire.SyncReq{Op: wire.OpPull, CurTs: curTs, CurID: curID}.Marshal()
		resp, err := c.roundtrip(ctx, "pull", wire.KindSync, c.c2s, payload, st)
		if err != nil {
			return err
		}
		if resp.Status != wire.StatusOK {
			return fmt.Errorf("client: pull status %s", resp.Status)
		}
		if len(resp.Entries) == 0 {
			return nil
		}
		last := resp.Entries[len(resp.Entries)-1]
		lastID := last.ID()
		if !posAfter(last.TsMs, lastID, curTs, curID) {
			return errors.New("client: server pull did not advance the cursor")
		}
		bytesIn := 0
		for _, e := range resp.Entries {
			bytesIn += e.Size()
		}
		// One durable batch per page: one fsync instead of one per
		// entry, which is what makes a first sync of a large vault
		// seconds rather than minutes.
		if _, err := c.store.Add(resp.Entries...); err != nil {
			return err
		}
		st.Pulled += len(resp.Entries)
		st.PulledBytes += bytesIn
		c.tracef("PULL -> %d entries (%d B)", len(resp.Entries), bytesIn)
		curTs, curID = last.TsMs, lastID
	}
}

// posAfter reports whether canonical position (ts, id) is strictly after
// (baseTs, baseID).
func posAfter(ts uint64, id vault.EntryID, baseTs uint64, baseID vault.EntryID) bool {
	if ts != baseTs {
		return ts > baseTs
	}
	return bytes.Compare(id[:], baseID[:]) > 0
}

// pushFrom walks the client's own entries from the anchor cursor in
// byte-bounded batches and pushes each. It is dedup-tolerant: the server
// drops what it already holds.
func (c *Client) pushFrom(ctx context.Context, anchorTs uint64, anchorID vault.EntryID, st *syncStats) error {
	curTs, curID := anchorTs, anchorID
	for {
		batch := c.store.EntriesAfter(curTs, curID, wire.PageBytes)
		if len(batch) == 0 {
			return nil
		}
		if err := c.sendPush(ctx, batch, "PUSH", st); err != nil {
			return err
		}
		last := batch[len(batch)-1]
		curTs, curID = last.TsMs, last.ID()
	}
}

// pushEntries pushes an explicit list of entries in byte-bounded batches
// — the fast path for freshly-written local entries.
func (c *Client) pushEntries(ctx context.Context, entries []vault.Entry, st *syncStats) error {
	i := 0
	for i < len(entries) {
		batch := make([]vault.Entry, 0, 64)
		used := 0
		for i < len(entries) {
			sz := entries[i].Size()
			if len(batch) > 0 && used+sz > wire.PageBytes {
				break
			}
			batch = append(batch, entries[i])
			used += sz
			i++
		}
		if err := c.sendPush(ctx, batch, "PUSH(new)", st); err != nil {
			return err
		}
	}
	return nil
}

// sendPush pushes one batch, accounting and tracing it.
func (c *Client) sendPush(ctx context.Context, batch []vault.Entry, label string, st *syncStats) error {
	payload := wire.SyncReq{Op: wire.OpPush, Entries: batch}.Marshal()
	resp, err := c.roundtrip(ctx, "push", wire.KindSync, c.c2s, payload, st)
	if err != nil {
		return err
	}
	if resp.Status != wire.StatusOK {
		return fmt.Errorf("client: push status %s", resp.Status)
	}
	bytesOut := 0
	for _, e := range batch {
		bytesOut += e.Size()
	}
	st.Pushed += len(batch)
	st.PushedBytes += bytesOut
	c.tracef("%s -> %d entries (%d B)", label, len(batch), bytesOut)
	return nil
}

// roundtrip seals a request frame under sealK, sends it, and opens the
// response under K_s2c. It verifies the response echoes the request
// nonce (anti-stitch) before returning. A 1-byte response is the
// server's pre-channel error, returned as a wire.PreChannel wrapped
// with stage, the operation that drew it.
func (c *Client) roundtrip(ctx context.Context, stage string, kind byte, sealK vault.ChannelKey, payload []byte, st *syncStats) (wire.Resp, error) {
	nonce, err := vault.FrameNonce(c.vid)
	if err != nil {
		return wire.Resp{}, err
	}
	frame, err := wire.SealFrame(wire.Header{Kind: kind, VaultID: c.vid, Nonce: nonce}, sealK, payload)
	if err != nil {
		return wire.Resp{}, err
	}
	respBytes, err := c.tran.Do(ctx, frame)
	if err != nil {
		return wire.Resp{}, err
	}
	st.RoundTrips++
	st.ReqBytes += len(frame)
	st.RespBytes += len(respBytes)
	if len(respBytes) == 1 {
		return wire.Resp{}, fmt.Errorf("client: %s: %w", stage, wire.PreChannel(respBytes[0]))
	}
	h, sealed, err := wire.ParseHeader(respBytes)
	if err != nil {
		return wire.Resp{}, fmt.Errorf("client: parse response: %w", err)
	}
	if h.Kind != wire.KindResp || h.VaultID != c.vid {
		return wire.Resp{}, errors.New("client: unexpected response frame")
	}
	rpayload, err := wire.OpenFrame(c.s2c, h, sealed)
	if err != nil {
		return wire.Resp{}, fmt.Errorf("client: response did not open: %w", err)
	}
	resp, err := wire.ParseResp(rpayload)
	if err != nil {
		return wire.Resp{}, err
	}
	if resp.ReqNonce != nonce {
		return wire.Resp{}, errors.New("client: response nonce does not match request")
	}
	return resp, nil
}

// reportStats traces a sync's closing summary.
func (c *Client) reportStats(st *syncStats) {
	c.tracef("STATS: pushed %d entries (%d B), pulled %d entries (%d B); %d round-trips, req %d B, resp %d B",
		st.Pushed, st.PushedBytes, st.Pulled, st.PulledBytes, st.RoundTrips, st.ReqBytes, st.RespBytes)
}
