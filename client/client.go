// Package client provides the ASCON-KV v1 client: Put/Get/Delete on a
// local encrypted store, plus a paged sync against a server. Every frame
// it sends is a single AEAD seal; reconciliation is PROBE / PULL / PUSH
// as defined in PROTOCOL.txt.
package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/pavlos/kv/ascon"
	"github.com/pavlos/kv/store"
	"github.com/pavlos/kv/vault"
	"github.com/pavlos/kv/wire"
)

// fmtRaw is the value-encoding tag for a verbatim value (CLIENT.txt).
const fmtRaw = 0x00

// Transport sends a marshaled request frame and returns the response
// bytes (a sealed RESP frame, or a single pre-channel error byte).
type Transport interface {
	Do(ctx context.Context, request []byte) (response []byte, err error)
}

// Client is the writer + reader + syncer for one vault.
type Client struct {
	rk      vault.RootKey
	aead    *ascon.AEAD
	authKey vault.AuthKey
	vid     vault.VaultID
	c2s     vault.ChannelKey
	s2c     vault.ChannelKey
	store   store.Store
	tran    Transport

	nowMs func() uint64

	mu         sync.Mutex
	lastTs     uint64
	registered bool
	hasReg     bool
	regChan    vault.ChannelKey // K_reg, valid iff hasReg
	dirty      bool
}

// New returns a Client backed by the given store and transport.
func New(rk vault.RootKey, s store.Store, t Transport) (*Client, error) {
	aead, err := rk.AEAD()
	if err != nil {
		return nil, err
	}
	ak := vault.DeriveAuthKey(rk)
	return &Client{
		rk:      rk,
		aead:    aead,
		authKey: ak,
		vid:     vault.DeriveVaultID(rk),
		c2s:     vault.DeriveC2S(ak),
		s2c:     vault.DeriveS2C(ak),
		store:   s,
		tran:    t,
		nowMs:   func() uint64 { return uint64(time.Now().UnixMilli()) },
	}, nil
}

// VaultID returns the public vault handle.
func (c *Client) VaultID() vault.VaultID { return c.vid }

// Store returns the backing store (read-only inspection).
func (c *Client) Store() store.Store { return c.store }

// Registered reports the client's belief that it has registered.
func (c *Client) Registered() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.registered
}

// SetRegistered sets the registration belief (the CLI persists it).
func (c *Client) SetRegistered(v bool) {
	c.mu.Lock()
	c.registered = v
	c.mu.Unlock()
}

// SetRegisterKey installs the register-gate secret used to seal a
// REGISTER frame. Pass nil to clear.
func (c *Client) SetRegisterKey(rk *vault.RegisterKey) {
	c.mu.Lock()
	if rk == nil {
		c.hasReg = false
		c.regChan = vault.ChannelKey{}
	} else {
		c.hasReg = true
		c.regChan = vault.DeriveRegKey(*rk)
	}
	c.mu.Unlock()
}

// --- local writes ---

// Put writes (key, value). The stored value is tagged fmt(1)||value.
func (c *Client) Put(key, value []byte) error {
	if len(value) == 0 {
		return errors.New("client: empty value is a tombstone; use Delete")
	}
	wrapped := make([]byte, 1+len(value))
	wrapped[0] = fmtRaw
	copy(wrapped[1:], value)

	ts := c.nextTs()
	e, err := vault.Seal(c.aead, ts, key, wrapped)
	if err != nil {
		return err
	}
	if _, err := c.store.Add(e); err != nil {
		return err
	}
	c.mu.Lock()
	c.dirty = true
	c.mu.Unlock()
	return nil
}

// Delete writes a tombstone (an empty value, with no fmt tag).
func (c *Client) Delete(key []byte) error {
	ts := c.nextTs()
	e, err := vault.Seal(c.aead, ts, key, nil)
	if err != nil {
		return err
	}
	if _, err := c.store.Add(e); err != nil {
		return err
	}
	c.mu.Lock()
	c.dirty = true
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

type keyValue struct {
	value []byte
	tomb  bool
}

// replay decrypts every entry in canonical order; later entries override
// earlier ones. An empty stored value is a tombstone; a non-empty one is
// fmt(1)||payload, and payload is returned. Entries that fail to open are
// silently skipped (they remain in the store so the set still matches
// the server's).
func (c *Client) replay() map[string]keyValue {
	sorted := c.store.Sorted()
	out := make(map[string]keyValue, len(sorted))
	for _, e := range sorted {
		k, sv, err := vault.Open(c.aead, e)
		if err != nil {
			continue
		}
		if len(sv) == 0 {
			out[string(k)] = keyValue{tomb: true}
			continue
		}
		out[string(k)] = keyValue{value: sv[1:]} // strip fmt tag
	}
	return out
}

// Get returns the current value for key.
func (c *Client) Get(key []byte) (value []byte, ok bool, err error) {
	kv, found := c.replay()[string(key)]
	if !found || kv.tomb {
		return nil, false, nil
	}
	return kv.value, true, nil
}

// List returns the sorted live keys.
func (c *Client) List() ([]string, error) {
	m := c.replay()
	out := make([]string, 0, len(m))
	for k, kv := range m {
		if !kv.tomb {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

// LiveEntry is a live (key, value) pair.
type LiveEntry struct {
	Key   []byte
	Value []byte
}

// All returns every live (key, value) pair, sorted by key.
func (c *Client) All() ([]LiveEntry, error) {
	m := c.replay()
	keys := make([]string, 0, len(m))
	for k, kv := range m {
		if !kv.tomb {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]LiveEntry, len(keys))
	for i, k := range keys {
		out[i] = LiveEntry{Key: []byte(k), Value: m[k].value}
	}
	return out, nil
}

// --- sync ---

// ErrSyncStuck is returned if sync fails to converge within its round
// budget (e.g. persistent concurrent churn, or a server that keeps
// forgetting the registration).
var ErrSyncStuck = errors.New("client: sync did not converge within round budget")

// Sync reconciles the client's vault with the server: register if
// needed, PROBE, and on divergence PULL the whole set then PUSH the
// whole set (dedup-tolerant), looping until a PROBE matches.
func (c *Client) Sync(ctx context.Context) error {
	const maxRounds = 64
	for range maxRounds {
		c.mu.Lock()
		registered := c.registered
		c.mu.Unlock()

		if !registered {
			if err := c.register(ctx); err != nil {
				return err
			}
		}

		inSync, pre, err := c.probe(ctx)
		if err != nil {
			return err
		}
		if pre == wire.ErrUnknownVault {
			c.SetRegistered(false)
			continue
		}
		if pre != 0 {
			return preChannelErr("probe", pre)
		}
		if inSync {
			c.mu.Lock()
			c.dirty = false
			c.mu.Unlock()
			return nil
		}

		if pre, err := c.pullAll(ctx); err != nil {
			return err
		} else if pre == wire.ErrUnknownVault {
			c.SetRegistered(false)
			continue
		} else if pre != 0 {
			return preChannelErr("pull", pre)
		}

		if pre, err := c.pushAll(ctx); err != nil {
			return err
		} else if pre == wire.ErrUnknownVault {
			c.SetRegistered(false)
			continue
		} else if pre != 0 {
			return preChannelErr("push", pre)
		}
		// Loop back to PROBE to confirm convergence (and catch any
		// entries a concurrent writer added mid-walk).
	}
	return ErrSyncStuck
}

// register establishes the channel by adopting the client's AuthKey on
// the server, sealed under K_reg.
func (c *Client) register(ctx context.Context) error {
	c.mu.Lock()
	hasReg := c.hasReg
	regChan := c.regChan
	c.mu.Unlock()
	if !hasReg {
		return errors.New("client: no register key; cannot register with a gated server")
	}
	resp, pre, err := c.roundtrip(ctx, wire.KindRegister, regChan, c.authKey[:])
	if err != nil {
		return err
	}
	if pre == wire.ErrBadAuth {
		return errors.New("client: registration rejected — wrong register key")
	}
	if pre != 0 {
		return preChannelErr("register", pre)
	}
	if resp.Status != wire.StatusOK {
		return fmt.Errorf("client: register status %s", wire.StatusText(resp.Status))
	}
	c.SetRegistered(true)
	return nil
}

func (c *Client) probe(ctx context.Context) (inSync bool, pre byte, err error) {
	payload := wire.SyncReq{Op: wire.OpProbe, SetHash: c.store.SetHash()}.Marshal()
	resp, pre, err := c.roundtrip(ctx, wire.KindSync, c.c2s, payload)
	if err != nil || pre != 0 {
		return false, pre, err
	}
	switch resp.Status {
	case wire.StatusOK:
		return true, 0, nil
	case wire.StatusDiffer:
		return false, 0, nil
	default:
		return false, 0, fmt.Errorf("client: probe status %s", wire.StatusText(resp.Status))
	}
}

// maxPullPages bounds a single pull walk. Loop protection: an honest
// server terminates the walk with an empty page once its set is
// exhausted, and the cursor advances strictly on every page, so this cap
// is only reached by a server fabricating an endless stream — an
// absolute backstop against a hung client.
const maxPullPages = 1 << 20

// pullAll walks the server's set from the beginning, committing each
// page. It stores every returned entry verbatim (even one that fails to
// open) so the client's set matches the server's. The pull cursor MUST
// advance on every non-empty page; a server that returns a page not
// beyond the cursor is misbehaving and is rejected rather than looped on.
func (c *Client) pullAll(ctx context.Context) (byte, error) {
	var curTs uint64
	var curID vault.EntryID
	for page := 0; ; page++ {
		if page >= maxPullPages {
			return 0, errors.New("client: pull exceeded page budget (misbehaving server)")
		}
		payload := wire.SyncReq{Op: wire.OpPull, CurTs: curTs, CurID: curID}.Marshal()
		resp, pre, err := c.roundtrip(ctx, wire.KindSync, c.c2s, payload)
		if err != nil || pre != 0 {
			return pre, err
		}
		if resp.Status != wire.StatusOK {
			return 0, fmt.Errorf("client: pull status %s", wire.StatusText(resp.Status))
		}
		if len(resp.Entries) == 0 {
			return 0, nil
		}
		last := resp.Entries[len(resp.Entries)-1]
		if !posAfter(last.TsMs, last.ID(), curTs, curID) {
			return 0, errors.New("client: server pull did not advance the cursor")
		}
		for _, e := range resp.Entries {
			if _, err := c.store.Add(e); err != nil {
				return 0, err
			}
		}
		curTs, curID = last.TsMs, last.ID()
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

// pushAll walks the client's own set in byte-bounded batches and pushes
// each. It is dedup-tolerant: the server drops what it already holds.
func (c *Client) pushAll(ctx context.Context) (byte, error) {
	sorted := c.store.Sorted()
	i := 0
	for i < len(sorted) {
		batch := make([]vault.Entry, 0, 64)
		used := 0
		for i < len(sorted) {
			sz := sorted[i].Size()
			if len(batch) > 0 && used+sz > wire.PageBytes {
				break
			}
			batch = append(batch, sorted[i])
			used += sz
			i++
		}
		payload := wire.SyncReq{Op: wire.OpPush, Entries: batch}.Marshal()
		resp, pre, err := c.roundtrip(ctx, wire.KindSync, c.c2s, payload)
		if err != nil || pre != 0 {
			return pre, err
		}
		if resp.Status != wire.StatusOK {
			return 0, fmt.Errorf("client: push status %s", wire.StatusText(resp.Status))
		}
	}
	return 0, nil
}

// roundtrip seals a request frame under sealK, sends it, and opens the
// response under K_s2c. It verifies the response echoes the request
// nonce (anti-stitch) before returning. A 1-byte response is an
// unauthenticated pre-channel error, returned in pre.
func (c *Client) roundtrip(ctx context.Context, kind byte, sealK vault.ChannelKey, payload []byte) (resp wire.Resp, pre byte, err error) {
	nonce, err := vault.FrameNonce(c.vid)
	if err != nil {
		return wire.Resp{}, 0, err
	}
	frame, err := wire.SealFrame(kind, c.vid, nonce, sealK, payload)
	if err != nil {
		return wire.Resp{}, 0, err
	}
	respBytes, err := c.tran.Do(ctx, frame)
	if err != nil {
		return wire.Resp{}, 0, err
	}
	if len(respBytes) == 1 {
		return wire.Resp{}, respBytes[0], nil
	}
	rkind, rvid, rnonce, sealed, err := wire.ParseHeader(respBytes)
	if err != nil {
		return wire.Resp{}, 0, fmt.Errorf("client: parse response: %w", err)
	}
	if rkind != wire.KindResp || rvid != c.vid {
		return wire.Resp{}, 0, errors.New("client: unexpected response frame")
	}
	rpayload, err := wire.OpenFrame(c.s2c, wire.KindResp, rvid, rnonce, sealed)
	if err != nil {
		return wire.Resp{}, 0, fmt.Errorf("client: response did not open: %w", err)
	}
	resp, err = wire.ParseResp(rpayload)
	if err != nil {
		return wire.Resp{}, 0, err
	}
	if resp.ReqNonce != nonce {
		return wire.Resp{}, 0, errors.New("client: response nonce does not match request")
	}
	return resp, 0, nil
}

func preChannelErr(stage string, b byte) error {
	return fmt.Errorf("client: %s: server returned %s", stage, wire.PreChannelText(b))
}
