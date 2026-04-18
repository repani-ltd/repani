// Package client provides the ASCON-KV client side: Put/Get/Delete on
// a local encrypted store, plus a sync cycle against a server.
package client

import (
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

// Transport sends a marshaled request and returns the marshaled
// response. It is implementable by a real HTTP client, an in-process
// handler for tests, or any other carrier.
type Transport interface {
	Do(ctx context.Context, request []byte) (response []byte, err error)
}

// Client is the writer + reader + syncer for one vault.
type Client struct {
	rk      vault.RootKey
	aead    *ascon.AEAD
	authKey vault.AuthKey
	vid     vault.VaultID
	store   store.Store
	tran    Transport

	nowMs func() uint64

	mu          sync.Mutex
	pending     map[vault.EntryID]struct{}
	lastTs      uint64              // monotonic-nondecreasing per-client write clock
	registered  bool                // TOFU registration state with the current server
	registerKey *vault.RegisterKey  // nil → no admin MAC; server must be in permissive mode
}

// New returns a Client backed by the given store and transport.
func New(rk vault.RootKey, s store.Store, t Transport) (*Client, error) {
	aead, err := rk.AEAD()
	if err != nil {
		return nil, err
	}
	return &Client{
		rk:      rk,
		aead:    aead,
		authKey: vault.DeriveAuthKey(rk),
		vid:     vault.DeriveVaultID(rk),
		store:   s,
		tran:    t,
		nowMs:   func() uint64 { return uint64(time.Now().UnixMilli()) },
		pending: make(map[vault.EntryID]struct{}),
	}, nil
}

// VaultID returns the public vault handle.
func (c *Client) VaultID() vault.VaultID { return c.vid }

// Store returns the backing store. Intended for read-only inspection
// by callers that need to see canonical order or ladder state.
// Mutating the store from outside the client voids the client's
// pending-push tracking and should be avoided outside of tests.
func (c *Client) Store() store.Store { return c.store }

// Registered reports whether the client believes it has registered
// with the server (TOFU bit). The CLI persists this; in-process tests
// typically ignore it.
func (c *Client) Registered() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.registered
}

// SetRegistered sets the TOFU bit. The CLI calls this after reading a
// persisted marker file so the first Sync after restart does not
// needlessly re-register.
func (c *Client) SetRegistered(v bool) {
	c.mu.Lock()
	c.registered = v
	c.mu.Unlock()
}

// SetRegisterKey installs the server-side register-gate secret so the
// next REGISTER request includes a valid admin MAC. Pass nil to
// clear. A client with no register key is only compatible with a
// server in legacy permissive TOFU mode; otherwise the server will
// return BadMAC on REGISTER.
func (c *Client) SetRegisterKey(rk *vault.RegisterKey) {
	c.mu.Lock()
	c.registerKey = rk
	c.mu.Unlock()
}

// Put writes (key, value). An empty key is rejected.
func (c *Client) Put(key, value []byte) error {
	ts := c.nextTs()
	e, err := vault.Seal(c.aead, ts, key, value)
	if err != nil {
		return err
	}
	if _, err := c.store.Add(e); err != nil {
		return err
	}
	c.mu.Lock()
	c.pending[e.ID()] = struct{}{}
	c.mu.Unlock()
	return nil
}

// nextTs returns a strictly-increasing millisecond timestamp per
// Client, so rapid Put/Delete pairs on the same key resolve in
// program order under LWW merge.
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

// Delete is shorthand for Put(key, nil): writes a tombstone entry.
func (c *Client) Delete(key []byte) error {
	return c.Put(key, nil)
}

// Get returns the current value for key. ok=false means the key has
// never been written or has been tombstoned.
func (c *Client) Get(key []byte) (value []byte, ok bool, err error) {
	m, err := c.replay()
	if err != nil {
		return nil, false, err
	}
	kv, found := m[string(key)]
	if !found || kv.tomb {
		return nil, false, nil
	}
	return kv.value, true, nil
}

// List returns the sorted list of live (non-tombstoned) keys.
func (c *Client) List() ([]string, error) {
	m, err := c.replay()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(m))
	for k, kv := range m {
		if !kv.tomb {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

// LiveEntry is a live (non-tombstoned) key/value pair returned by
// All. Values are held in plaintext in memory; callers are
// responsible for scrubbing if that matters.
type LiveEntry struct {
	Key   []byte
	Value []byte
}

// All returns every live (key, value) pair in the vault, sorted by
// key. It replays and decrypts the full local store and is the basis
// of client-side search. Tombstoned entries are omitted.
func (c *Client) All() ([]LiveEntry, error) {
	m, err := c.replay()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(m))
	for k, kv := range m {
		if kv.tomb {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]LiveEntry, len(keys))
	for i, k := range keys {
		out[i] = LiveEntry{Key: []byte(k), Value: m[k].value}
	}
	return out, nil
}

// --- merge (LWW) ---

type keyValue struct {
	value []byte
	tomb  bool
}

// replay decrypts every entry in canonical order; later entries
// override earlier ones, and a zero-length value is a tombstone.
// Entries that fail AEAD open are silently skipped — per PROTOCOL.txt
// a failing entry is never a fatal condition. Callers get a clean map
// of openable plaintexts.
func (c *Client) replay() (map[string]keyValue, error) {
	sorted := c.store.Sorted()
	out := make(map[string]keyValue, len(sorted))
	for _, e := range sorted {
		k, v, err := vault.Open(c.aead, e)
		if err != nil {
			continue
		}
		out[string(k)] = keyValue{
			value: v,
			tomb:  len(v) == 0,
		}
	}
	return out, nil
}

// --- sync ---

// ErrSyncNotConverged is returned if Sync exceeds its round budget.
var ErrSyncNotConverged = errors.New("client: sync did not converge within round budget")

// Sync performs sync rounds against the server until the server reports
// window = 0 and the client has no pending pushes. Converges in one
// round in the steady state and O(log N) rounds after a full server
// wipe.
func (c *Client) Sync(ctx context.Context) error {
	const maxRounds = 96 // log2(2^63) with headroom
	for range maxRounds {
		done, err := c.syncOnce(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return ErrSyncNotConverged
}

// syncOnce performs exactly one round trip.
func (c *Client) syncOnce(ctx context.Context) (done bool, err error) {
	sorted := c.store.Sorted()
	n := uint32(len(sorted))
	ladder := vault.ComputeLadder(sorted)
	terminal := vault.TerminalHash(sorted)

	// snapshot pending ids + registration bit + register key.
	c.mu.Lock()
	pushIDs := make([]vault.EntryID, 0, len(c.pending))
	for id := range c.pending {
		pushIDs = append(pushIDs, id)
	}
	registered := c.registered
	var regKeySnap *vault.RegisterKey
	if c.registerKey != nil {
		rk := *c.registerKey
		regKeySnap = &rk
	}
	c.mu.Unlock()

	// resolve ids to entries using a quick map.
	idx := make(map[vault.EntryID]vault.Entry, len(sorted))
	for _, e := range sorted {
		idx[e.ID()] = e
	}
	pushes := make([]vault.Entry, 0, len(pushIDs))
	for _, id := range pushIDs {
		if e, ok := idx[id]; ok {
			pushes = append(pushes, e)
		}
	}

	req := wire.Request{
		VaultID:  c.vid,
		N:        n,
		Ladder:   ladder,
		Terminal: terminal,
		Pushes:   pushes,
	}
	if !registered {
		req.Flags |= wire.FlagRegister
		req.RegAuth = c.authKey
		if regKeySnap != nil {
			req.RegAdminMAC = vault.RegisterAdminMAC(*regKeySnap, c.authKey, c.vid)
		}
	}
	reqBytes, err := wire.SignRequest(c.authKey, &req)
	if err != nil {
		return false, fmt.Errorf("client: sign request: %w", err)
	}

	respBytes, err := c.tran.Do(ctx, reqBytes)
	if err != nil {
		return false, err
	}

	resp, err := wire.UnmarshalResponse(respBytes)
	if err != nil {
		return false, fmt.Errorf("client: decode response: %w", err)
	}

	// UNKNOWN_VAULT: server has lost our registration (or we never had
	// one). Reset our registered bit so the next syncOnce re-registers
	// and call it not-done so Sync retries.
	if resp.Status == wire.StatusUnknownVault {
		c.mu.Lock()
		c.registered = false
		c.mu.Unlock()
		if registered {
			// We thought we were registered — retry now.
			return false, nil
		}
		// We already tried to register and still got UNKNOWN_VAULT.
		// Something is wrong on the server side.
		return false, fmt.Errorf("client: server returned UNKNOWN_VAULT on registration attempt")
	}

	if resp.Status != wire.StatusOK {
		return false, fmt.Errorf("client: server status: %s", wire.StatusText(resp.Status))
	}

	// Verify response MAC BEFORE ingesting anything.
	if err := wire.VerifyResponseMAC(c.authKey, req.MAC, resp); err != nil {
		return false, fmt.Errorf("client: verify response MAC: %w", err)
	}

	// Mark registered after first successful signed response.
	if req.IsRegister() {
		c.mu.Lock()
		c.registered = true
		c.mu.Unlock()
	}

	// Clear the entries we just pushed from pending — the server has
	// them (either newly appended or dedup'd).
	c.mu.Lock()
	for _, id := range pushIDs {
		delete(c.pending, id)
	}
	c.mu.Unlock()

	if resp.Window == 0 {
		return len(pushes) == 0, nil
	}

	// Ingest server's entries. Silently discard any entry that fails
	// to open under our AEAD_Key — per PROTOCOL.txt that is never a
	// fatal condition, just a bug or rare race.
	serverIDs := make(map[vault.EntryID]struct{}, len(resp.Entries))
	for _, e := range resp.Entries {
		if _, _, err := vault.Open(c.aead, e); err != nil {
			continue
		}
		id := e.ID()
		serverIDs[id] = struct{}{}
		if _, err := c.store.Add(e); err != nil {
			return false, fmt.Errorf("client: ingest server entry: %w", err)
		}
	}

	// Diff client's post-ingest last-D against server's last-D.
	after := c.store.Sorted()
	d := min(resp.Window, uint32(len(after)))
	tail := after[uint32(len(after))-d:]
	c.mu.Lock()
	for _, e := range tail {
		id := e.ID()
		if _, have := serverIDs[id]; !have {
			c.pending[id] = struct{}{}
		}
	}
	c.mu.Unlock()

	return false, nil
}
