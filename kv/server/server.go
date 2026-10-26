// Package server implements the ASCON-KV v1 server: a blind relay that
// holds a content-addressed set of entries per VaultID and serves the
// paged sync protocol defined in PROTOCOL.t. It opens every frame
// under a channel key, never under AEAD_Key, so it never sees plaintext.
package server

import (
	"cmp"
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"repani.com/durable"
	"repani.com/kv/store"
	"repani.com/kv/vault"
	"repani.com/kv/wire"
)

// Quotas configure a server's limits. A zero field takes its
// DefaultQuotas value.
type Quotas struct {
	MaxEntries     uint64
	MaxVaultBytes  uint64
	MaxVaults      uint64
	TSSkewMs       int64
	EphemeralTTLMs int64 // an ephemeral vault's lifetime from its creation
	WaitMaxMs      int64 // the longest a WAIT is held before it answers OK
}

// withDefaults returns q with each zero field set to its default.
func (q Quotas) withDefaults() Quotas {
	d := DefaultQuotas()
	return Quotas{
		MaxEntries:     cmp.Or(q.MaxEntries, d.MaxEntries),
		MaxVaultBytes:  cmp.Or(q.MaxVaultBytes, d.MaxVaultBytes),
		MaxVaults:      cmp.Or(q.MaxVaults, d.MaxVaults),
		TSSkewMs:       cmp.Or(q.TSSkewMs, d.TSSkewMs),
		EphemeralTTLMs: cmp.Or(q.EphemeralTTLMs, d.EphemeralTTLMs),
		WaitMaxMs:      cmp.Or(q.WaitMaxMs, d.WaitMaxMs),
	}
}

// logLockWait bounds how long a vault open waits for the entry-log
// lock. Contention here means another server is going down (a restart
// overlap) or a misconfigured second server; waiting briefly smooths
// the former without hanging on the latter.
const logLockWait = 5 * time.Second

// DefaultQuotas are limits a small host can honour. Every accepted
// byte stays in memory (LogStore wraps a MemStore), so they are a
// memory budget first; a larger host raises them through kvserve's
// flags. DESIGN.t, Quotas, has the sizing and the gap they leave.
func DefaultQuotas() Quotas {
	return Quotas{
		MaxEntries:     10_000,
		MaxVaultBytes:  8 << 20, // 8 MiB
		MaxVaults:      1_000,
		TSSkewMs:       24 * 60 * 60 * 1000,
		EphemeralTTLMs: 60 * 60 * 1000,
		WaitMaxMs:      wire.WaitMaxMs,
	}
}

// Options configure a Server.
//
// A REGISTER sealed under any of RegisterKeys is accepted, so each
// application can hold its own key and lose it alone. With none,
// in-band registration is disabled: every REGISTER frame is rejected
// with BAD_AUTH, and vaults can only be created by a co-resident owner
// via EnsureVault. There is never permissionless registration.
type Options struct {
	DataDir      string
	Quotas       Quotas
	Clock        func() int64
	RegisterKeys []vault.RegisterKey

	// OnPush, if non-nil, is called after a PUSH frame's entries have
	// been validated and persisted, once per PUSH that added at least
	// one new entry. It runs outside the vault locks, on the goroutine
	// serving the frame, so it must not block for long. Entries added
	// via AddEntries do NOT fire it.
	OnPush func(vid vault.VaultID)
}

// Server is a vault-aware HTTP handler. Use New.
type Server struct {
	dataDir string
	quotas  Quotas
	clock   func() int64
	regKeys []vault.ChannelKey // K_reg per RegisterKey; none = registration disabled
	onPush  func(vault.VaultID)
	mux     *http.ServeMux // the protocol routes, built once

	// mu guards vaults and revoked. Lock order: mu, then a vaultState's
	// mu; never the other way. revoked holds every vault whose record
	// says ended, and every one whose record cannot be read: none is
	// served or registered again (see record.go).
	mu      sync.Mutex
	vaults  map[vault.VaultID]*vaultState
	revoked map[vault.VaultID]bool

	// closing is closed by Drain, or by Close if Drain was not called,
	// so that every WAIT held open answers at once (see handleWait).
	closing   chan struct{}
	closeOnce sync.Once
}

type vaultState struct {
	mu      sync.Mutex
	s       store.Store
	authKey vault.AuthKey
	c2s     vault.ChannelKey
	s2c     vault.ChannelKey
	bytes   uint64
	dir     string
	noPush  bool // refuse wire PUSH; owner AddEntries still writes

	ephemeral bool  // fixed at creation; see endLocked
	created   int64 // ms, to the second, as its record holds it
	gone      bool  // dropped or ended: its store is closed (mu held to read)
	closed    bool  // the server closed its store (Close); mu held to read

	// changed is closed and replaced whenever the vault's set changes
	// or the vault ends, waking every WAIT held on it. mu held to read
	// or replace it; see notifyLocked.
	changed chan struct{}
}

// notifyLocked wakes the WAITs held on the vault: the set changed, or
// the vault is going away. st.mu held.
func (st *vaultState) notifyLocked() {
	close(st.changed)
	st.changed = make(chan struct{})
}

// expired reports an ephemeral vault past its lifetime.
func (s *Server) expired(st *vaultState) bool {
	return st.ephemeral && s.clock()-st.created >= s.quotas.EphemeralTTLMs
}

// New constructs a Server.
func New(opt Options) (*Server, error) {
	var regKeys []vault.ChannelKey // none = registration disabled
	for _, rk := range opt.RegisterKeys {
		regKeys = append(regKeys, vault.DeriveRegKey(rk))
	}
	if opt.DataDir != "" {
		if err := os.MkdirAll(opt.DataDir, 0o700); err != nil {
			return nil, err
		}
	}
	clk := opt.Clock
	if clk == nil {
		clk = func() int64 { return time.Now().UnixMilli() }
	}
	s := &Server{
		dataDir: opt.DataDir,
		quotas:  opt.Quotas.withDefaults(),
		clock:   clk,
		regKeys: regKeys,
		revoked: make(map[vault.VaultID]bool),
		onPush:  opt.OnPush,
		vaults:  make(map[vault.VaultID]*vaultState),
		closing: make(chan struct{}),
	}
	s.mux = s.routes()
	if opt.DataDir != "" {
		if err := s.loadPersistedVaults(); err != nil {
			s.Close() // the logs already opened, and their locks
			return nil, err
		}
	}
	return s, nil
}

func (s *Server) loadPersistedVaults() error {
	dirs, err := os.ReadDir(s.dataDir)
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		vid, err := vault.ParseVaultID(d.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(s.dataDir, d.Name())
		r, err := readRecord(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue // a creation that crashed before its record: never served
		}
		var ak vault.AuthKey
		if err == nil && r.Ended == nil {
			ak, err = r.authKey()
		}
		if err != nil {
			// A single damaged vault must not take the whole server
			// offline: skip it loudly and serve the rest. It is refused
			// too, not left for a REGISTER to create afresh, since the
			// record it lost may have said ended.
			log.Printf("server: refusing vault %s until its record is mended: %v", d.Name(), err)
			s.revoked[vid] = true
			continue
		}
		if r.Ended != nil {
			// Revoked for good. Finish a deletion a crash interrupted.
			s.revoked[vid] = true
			if err := removeLog(dir); err != nil {
				log.Printf("server: ended vault %s: %v", d.Name(), err)
			}
			continue
		}
		ls, err := store.OpenLogStore(logPath(dir), logLockWait)
		if err != nil {
			log.Printf("server: skipping vault %s: %v", d.Name(), err)
			continue
		}
		st := newVaultState(ls, ak, dir)
		for _, e := range ls.Sorted() {
			st.bytes += uint64(e.Size())
		}
		st.ephemeral, st.created = r.Ephemeral, r.Created.UnixMilli()
		s.vaults[vid] = st
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sweepLocked()
}

// sweepLocked ends every ephemeral vault past its lifetime. s.mu held.
func (s *Server) sweepLocked() error {
	for vid, st := range s.vaults {
		if s.expired(st) {
			if err := s.endLocked(vid, st, noteExpired); err != nil {
				return err
			}
		}
	}
	return nil
}

// expireIfDue ends vid if it is an ephemeral vault past its lifetime,
// reporting whether it ended now.
func (s *Server) expireIfDue(vid vault.VaultID, st *vaultState) (bool, error) {
	if !s.expired(st) {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vaults[vid] != st {
		return true, nil // another request ended it
	}
	return true, s.endLocked(vid, st, noteExpired)
}

// The notes an ephemeral vault's record ends with.
const (
	noteExpired = "ephemeral: expired"
	noteDropped = "ephemeral: dropped by its holder"
)

// endLocked ends a vault for good: an ephemeral one as PROTOCOL.t
// (Ephemeral vaults) has it, any one at its owner's DropVault. It is
// revoked, its store closed, and its entries deleted. On disk the
// record is rewritten as ended, with note, first, so a crash at any
// point leaves a vault that the next start treats as ended. An error
// means the record could not be written and nothing changed; once it
// is, the vault has ended, and an entry log left behind is logged and
// deleted at the next start. s.mu held, st.mu not.
func (s *Server) endLocked(vid vault.VaultID, st *vaultState, note string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.dir != "" {
		if err := writeRecord(st.dir, st.record().ended(s.clock(), note)); err != nil {
			return err
		}
	}
	s.revoked[vid] = true
	delete(s.vaults, vid)
	st.gone = true
	st.notifyLocked()
	if c, ok := st.s.(io.Closer); ok {
		if err := c.Close(); err != nil {
			log.Printf("server: ended vault %s: close: %v", vid, err)
		}
	}
	if st.dir != "" {
		if err := removeLog(st.dir); err != nil {
			log.Printf("server: ended vault %s: %v (deleted at the next start)", vid, err)
		}
	}
	return nil
}

func newVaultState(s store.Store, ak vault.AuthKey, dir string) *vaultState {
	return &vaultState{
		s:       s,
		authKey: ak,
		c2s:     vault.DeriveC2S(ak),
		s2c:     vault.DeriveS2C(ak),
		dir:     dir,
		changed: make(chan struct{}),
	}
}

// Drain answers every WAIT held open with OK, and any WAIT that
// arrives after it at once: an HTTP server's drain waits for every
// request in flight, so it is called as shutdown starts, before that
// wait (kiosk.Config.Drain), and a stop is not held up by WAIT_MAX_MS.
// Every other operation is served as before. It does not block.
func (s *Server) Drain() {
	s.closeOnce.Do(func() { close(s.closing) })
}

// Close releases every vault's backing store, including the advisory
// file locks its log holds, and drains (Drain) if that was not done.
// The server must not be used afterwards; it is the clean-shutdown
// counterpart to New.
func (s *Server) Close() error {
	s.closeOnce.Do(func() { close(s.closing) })
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for _, st := range s.vaults {
		st.mu.Lock()
		st.closed = true
		if c, ok := st.s.(io.Closer); ok {
			if err := c.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		st.mu.Unlock()
	}
	return firstErr
}

// HandleFrame processes one request frame and returns a response: either
// a sealed RESP frame, or a single pre-channel error byte. A non-nil
// error is an I/O problem that prevents any response. A WAIT is held
// for up to the server's WaitMaxMs; HandleFrameContext lets the caller
// end it sooner.
func (s *Server) HandleFrame(req []byte) ([]byte, error) {
	return s.HandleFrameContext(context.Background(), req)
}

// HandleFrameContext is HandleFrame bounded by ctx: a WAIT held open
// answers as soon as ctx ends (the HTTP handler passes the request's
// context, so a client that gives up frees its goroutine). No other
// operation watches ctx.
func (s *Server) HandleFrameContext(ctx context.Context, req []byte) ([]byte, error) {
	h, sealed, err := wire.ParseHeader(req)
	if err != nil {
		if errors.Is(err, wire.ErrVersion) {
			return wire.ErrBadVersion.Reply(), nil
		}
		return wire.ErrBadFrame.Reply(), nil
	}
	switch h.Kind {
	case wire.KindRegister:
		return s.handleRegister(h, sealed)
	case wire.KindSync:
		return s.handleSync(ctx, h, sealed)
	default:
		return wire.ErrBadKind.Reply(), nil
	}
}

func (s *Server) handleRegister(h wire.Header, sealed []byte) ([]byte, error) {
	vid, nonce := h.VaultID, h.Nonce
	var payload []byte
	opened := false // no keys = registration disabled
	for _, k := range s.regKeys {
		if p, err := wire.OpenFrame(k, h, sealed); err == nil {
			payload, opened = p, true
			break
		}
	}
	if !opened {
		return wire.ErrBadAuth.Reply(), nil // gate: no RegisterKey opens it
	}
	ak, ephemeral, err := wire.ParseRegister(payload)
	if err != nil {
		return wire.ErrBadFrame.Reply(), nil
	}

	s.mu.Lock()
	st, exists := s.vaults[vid]
	if exists && s.expired(st) {
		if err := s.endLocked(vid, st, noteExpired); err != nil {
			s.mu.Unlock()
			log.Printf("server: end vault %s: %v", vid, err)
			return wire.ErrTryLater.Reply(), nil
		}
		exists = false
	}
	if s.revoked[vid] {
		s.mu.Unlock()
		return wire.ErrBadAuth.Reply(), nil // revoked: never again
	}
	if exists {
		s.mu.Unlock()
		if subtle.ConstantTimeCompare(st.authKey[:], ak[:]) != 1 {
			return wire.ErrBadAuth.Reply(), nil // rebind attempt
		}
		return s.sealResp(vid, st.s2c, nonce, wire.StatusOK, nil) // its mark stays as created
	}
	// Expired ephemeral vaults give their places back before the cap
	// is judged.
	if err := s.sweepLocked(); err != nil {
		s.mu.Unlock()
		log.Printf("server: sweep: %v", err)
		return wire.ErrTryLater.Reply(), nil
	}
	if uint64(len(s.vaults)) >= s.quotas.MaxVaults {
		s.mu.Unlock()
		return s.sealResp(vid, vault.DeriveS2C(ak), nonce, wire.StatusQuota, nil)
	}
	st, err = s.createVaultLocked(vid, ak, ephemeral)
	s.mu.Unlock()
	if err != nil {
		log.Printf("server: create vault %s: %v", vid, err)
		return wire.ErrTryLater.Reply(), nil
	}
	return s.sealResp(vid, st.s2c, nonce, wire.StatusOK, nil)
}

func (s *Server) handleSync(ctx context.Context, h wire.Header, sealed []byte) ([]byte, error) {
	vid, nonce := h.VaultID, h.Nonce
	s.mu.Lock()
	st, ok := s.vaults[vid]
	s.mu.Unlock()
	if !ok {
		return wire.ErrUnknownVault.Reply(), nil
	}
	payload, err := wire.OpenFrame(st.c2s, h, sealed)
	if err != nil {
		return wire.ErrBadAuth.Reply(), nil
	}
	if ended, err := s.expireIfDue(vid, st); ended {
		if err != nil {
			log.Printf("server: end vault %s: %v", vid, err)
			return wire.ErrTryLater.Reply(), nil
		}
		return wire.ErrUnknownVault.Reply(), nil
	}
	req, err := wire.ParseSync(payload)
	if err != nil {
		return s.sealResp(vid, st.s2c, nonce, wire.StatusBadRequest, nil)
	}
	if req.Op == wire.OpDrop {
		return s.handleDrop(vid, st, nonce)
	}
	if req.Op == wire.OpWait {
		return s.handleWait(ctx, vid, st, nonce, req.SetHash)
	}

	// The op runs under the vault lock; OnPush fires after it is
	// released so the hook can safely call back into the server.
	resp, pushedNew, err := func() ([]byte, bool, error) {
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.gone { // ended or dropped after the lookup above
			return wire.ErrUnknownVault.Reply(), false, nil
		}

		status := wire.StatusOK
		var page []vault.Entry
		pushed := 0

		switch req.Op {
		case wire.OpProbe:
			if st.s.SetHash() != req.SetHash {
				status = wire.StatusDiffer
			}
		case wire.OpPull:
			page = st.s.EntriesAfter(req.CurTs, req.CurID, wire.PageBytes)
		case wire.OpPush:
			newEntries, verr := s.validateNew(st, req.Entries, true)
			if verr == nil && st.noPush && len(newEntries) > 0 {
				// A no-push vault accepts nothing new over the wire: its
				// published credential grants read and nothing else. A
				// dedup no-op (re-pushing entries the vault already
				// holds) still acks OK, so a read-only client's
				// pull-then-push walk converges instead of erroring.
				verr = ErrQuota
			}
			if verr != nil {
				status = wire.StatusBadRequest
				if errors.Is(verr, ErrQuota) {
					status = wire.StatusQuota
				}
				break
			}
			var err error
			pushed, err = addValidatedLocked(st, newEntries)
			if err != nil {
				return nil, false, err
			}
		default:
			status = wire.StatusBadRequest
		}

		resp, err := s.sealResp(vid, st.s2c, nonce, status, page)
		return resp, pushed > 0, err
	}()
	if err == nil && pushedNew && s.onPush != nil {
		s.onPush(vid)
	}
	return resp, err
}

// handleWait answers a WAIT (PROTOCOL.t, Wait): DIFFER at once if the
// vault's set already differs from the client's hash, else it is held
// until the set changes, the vault ends, the server closes, ctx ends or
// WaitMaxMs passes, and then answers DIFFER if the set differs and OK
// if it does not. It holds no lock while waiting, so pushes and other
// waits proceed; what it holds is one goroutine and its connection.
func (s *Server) handleWait(ctx context.Context, vid vault.VaultID, st *vaultState, nonce [vault.NonceSize]byte, want [16]byte) ([]byte, error) {
	st.mu.Lock()
	if st.gone {
		st.mu.Unlock()
		return wire.ErrUnknownVault.Reply(), nil
	}
	if st.closed {
		st.mu.Unlock()
		return s.sealResp(vid, st.s2c, nonce, wire.StatusOK, nil)
	}
	if st.s.SetHash() != want {
		st.mu.Unlock()
		return s.sealResp(vid, st.s2c, nonce, wire.StatusDiffer, nil)
	}
	changed := st.changed
	st.mu.Unlock()

	timer := time.NewTimer(time.Duration(s.quotas.WaitMaxMs) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-changed:
	case <-timer.C:
	case <-ctx.Done():
	case <-s.closing:
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.gone {
		return wire.ErrUnknownVault.Reply(), nil
	}
	status := wire.StatusOK
	if !st.closed && st.s.SetHash() != want {
		status = wire.StatusDiffer
	}
	return s.sealResp(vid, st.s2c, nonce, status, nil)
}

// handleDrop ends an ephemeral vault at its holder's request; any
// other vault answers BAD_REQUEST and is untouched. The OK is sealed
// under the channel the vault had.
func (s *Server) handleDrop(vid vault.VaultID, st *vaultState, nonce [vault.NonceSize]byte) ([]byte, error) {
	if !st.ephemeral {
		return s.sealResp(vid, st.s2c, nonce, wire.StatusBadRequest, nil)
	}
	s.mu.Lock()
	if s.vaults[vid] != st {
		s.mu.Unlock()
		return wire.ErrUnknownVault.Reply(), nil // ended meanwhile
	}
	err := s.endLocked(vid, st, noteDropped)
	s.mu.Unlock()
	if err != nil {
		log.Printf("server: drop vault %s: %v", vid, err)
		return wire.ErrTryLater.Reply(), nil
	}
	return s.sealResp(vid, st.s2c, nonce, wire.StatusOK, nil)
}

// addValidatedLocked adds entries that already passed validateNew,
// updating the vault's byte accounting. st.mu held. Returns the
// number actually added. Shared by the PUSH path and AddEntries so
// quota accounting cannot diverge between them.
//
// validateNew guarantees entries are new and mutually distinct, so
// the whole batch lands as one durable unit (one fsync on a
// log-backed store) and its byte total is exactly the sum of sizes.
func addValidatedLocked(st *vaultState, entries []vault.Entry) (int, error) {
	added, err := st.s.Add(entries...)
	if err != nil {
		return added, err
	}
	for _, e := range entries[:added] {
		st.bytes += uint64(e.Size())
	}
	if added > 0 {
		st.notifyLocked()
	}
	return added, nil
}

// validateNew enforces quota -- and, when checkSkew is set, timestamp
// skew -- on the entries the server does not already hold; re-pushing
// an accepted entry is a no-op and not re-judged. Skew bounds only the
// future: an entry written offline days ago, or replayed onto a
// rebuilt server, is honest and accepted however old. The wire PUSH
// path checks skew; the owner API, trusted, does not. Returns the new
// entries, or ErrQuota or errSkew.
func (s *Server) validateNew(st *vaultState, pushes []vault.Entry, checkSkew bool) ([]vault.Entry, error) {
	now := s.clock()
	skew := s.quotas.TSSkewMs
	newEntries := make([]vault.Entry, 0, len(pushes))
	var newBytes uint64
	seen := make(map[vault.EntryID]bool, len(pushes))
	for _, e := range pushes {
		id := e.ID()
		// The in-page dedup keeps newEntries mutually distinct, which
		// addValidatedLocked's byte accounting relies on (and stops a
		// duplicated push entry counting twice against the quota).
		if seen[id] || st.s.Has(id) {
			continue
		}
		seen[id] = true
		// Unsigned, so a ts_ms of 2^63 or more cannot wrap below now.
		if checkSkew && e.TsMs > uint64(now)+uint64(skew) {
			return nil, errSkew
		}
		newEntries = append(newEntries, e)
		newBytes += uint64(e.Size())
	}
	if uint64(st.s.Len())+uint64(len(newEntries)) > s.quotas.MaxEntries {
		return nil, ErrQuota
	}
	if st.bytes+newBytes > s.quotas.MaxVaultBytes {
		return nil, ErrQuota
	}
	return newEntries, nil
}

// errSkew rejects a pushed entry whose ts_ms is further ahead of the
// server's clock than TSSkewMs.
var errSkew = errors.New("server: entry timestamp too far ahead of the server's clock")

// sealResp builds a RESP frame under s2c, echoing reqNonce.
func (s *Server) sealResp(vid vault.VaultID, s2c vault.ChannelKey, reqNonce [vault.NonceSize]byte, status wire.Status, entries []vault.Entry) ([]byte, error) {
	respNonce, err := vault.FrameNonce(vid)
	if err != nil {
		return nil, err
	}
	payload := wire.Resp{Status: status, ReqNonce: reqNonce, Entries: entries}.Marshal()
	return wire.SealFrame(wire.Header{Kind: wire.KindResp, VaultID: vid, Nonce: respNonce}, s2c, payload)
}

// createVaultLocked constructs and persists a new vaultState: its
// record first, in one write, then its entry log. s.mu held.
func (s *Server) createVaultLocked(vid vault.VaultID, ak vault.AuthKey, ephemeral bool) (*vaultState, error) {
	created := recordTime(s.clock()).UnixMilli()
	if s.dataDir == "" {
		st := newVaultState(store.NewMemStore(), ak, "")
		st.ephemeral, st.created = ephemeral, created
		s.vaults[vid] = st
		return st, nil
	}
	dir := filepath.Join(s.dataDir, vid.String())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	st := newVaultState(nil, ak, dir)
	st.ephemeral, st.created = ephemeral, created
	if err := writeRecord(dir, st.record()); err != nil {
		return nil, err
	}
	ls, err := store.OpenLogStore(logPath(dir), logLockWait)
	if err != nil {
		return nil, err
	}
	st.s = ls
	s.vaults[vid] = st
	return st, nil
}

// ErrVaultInUse reports a vault whose entry log a running server holds.
var ErrVaultInUse = errors.New("server: vault is held by a running server")

// DropVaultDir is DropVault for a server that is not running, and for
// any vault id, held or not: it rewrites vid's record under dataDir as
// ended, with note, and deletes its entry log. A vault this server has
// never held gets a record that says only that it ended, so it can
// never be registered here. already reports a vault that had ended
// before, whose record is left as it was.
//
// It takes the entry log's lock first, so it refuses with
// ErrVaultInUse while a server is serving the vault: a server keeps
// every vault's log open from start to stop, and changing the files
// under it changes nothing it serves until it restarts.
func DropVaultDir(dataDir string, vid vault.VaultID, note string) (already bool, err error) {
	dir := filepath.Join(dataDir, vid.String())
	if _, err := os.Stat(logPath(dir)); err == nil {
		ls, err := store.OpenLogStore(logPath(dir), 0)
		if errors.Is(err, store.ErrLocked) {
			return false, ErrVaultInUse
		}
		if err != nil {
			return false, err
		}
		defer ls.Close()
	}
	now := time.Now().UnixMilli()
	r, err := readRecord(dir)
	switch {
	case err == nil && r.Ended != nil:
		already = true
	case err == nil:
		err = writeRecord(dir, r.ended(now, note))
	case errors.Is(err, os.ErrNotExist):
		if err = os.MkdirAll(dir, 0o700); err == nil {
			err = writeRecord(dir, vaultRecord{Created: recordTime(now)}.ended(now, note))
		}
	}
	if err != nil {
		return false, err
	}
	return already, removeLog(dir)
}

// removeLog deletes an ended vault's entry log, if it has one.
func removeLog(dir string) error {
	if err := os.Remove(logPath(dir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return durable.SyncDir(dir)
}

// --- HTTP ---

// routes builds the server's route table: wire.SyncPath alone. The
// path and method live in the pattern, so http.ServeMux answers a
// wrong method with 405 and a correct Allow header.
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+wire.SyncPath, s.serveSync)
	return mux
}

// ServeHTTP serves the protocol routes, so a Server can be mounted
// anywhere an http.Handler goes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// serveSync exchanges one sealed frame.
func (s *Server) serveSync(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, wire.MaxFrameBytes))
	if err != nil {
		log.Printf("server: read body: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	resp, err := s.HandleFrameContext(r.Context(), body)
	if err != nil {
		log.Printf("server: handle frame: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(resp)
}
