// Package server implements the ASCON-KV v1 server: a blind relay that
// holds a content-addressed set of entries per VaultID and serves the
// paged sync protocol defined in PROTOCOL.t. It opens every frame
// under a channel key, never under AEAD_Key, so it never sees plaintext.
package server

import (
	"cmp"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"repani.com/kv/store"
	"repani.com/kv/vault"
	"repani.com/kv/wire"
)

// Quotas configure a server's limits. A zero field takes its
// DefaultQuotas value.
type Quotas struct {
	MaxEntries    uint64
	MaxVaultBytes uint64
	MaxVaults     uint64
	TSSkewMs      int64
}

// withDefaults returns q with each zero field set to its default.
func (q Quotas) withDefaults() Quotas {
	d := DefaultQuotas()
	return Quotas{
		MaxEntries:    cmp.Or(q.MaxEntries, d.MaxEntries),
		MaxVaultBytes: cmp.Or(q.MaxVaultBytes, d.MaxVaultBytes),
		MaxVaults:     cmp.Or(q.MaxVaults, d.MaxVaults),
		TSSkewMs:      cmp.Or(q.TSSkewMs, d.TSSkewMs),
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
		MaxEntries:    10_000,
		MaxVaultBytes: 8 << 20, // 8 MiB
		MaxVaults:     1_000,
		TSSkewMs:      24 * 60 * 60 * 1000,
	}
}

// Options configure a Server. At most one of RegisterKey or
// RegisterKeyFile may be set. If neither is set, in-band registration
// is disabled: every REGISTER frame is rejected with BAD_AUTH, and
// vaults can only be created by a co-resident owner via EnsureVault.
// There is never permissionless registration.
//
// RegisterKeyFile holds one or more RegisterKeys, one per line; blank
// lines and lines starting with "#" are ignored, so each key can carry
// a label comment naming who holds it. A REGISTER sealed under any of
// them is accepted, and revoking one (deleting its line, then
// restarting) leaves the others and every existing vault untouched.
type Options struct {
	DataDir         string
	Quotas          Quotas
	Clock           func() int64
	RegisterKey     *vault.RegisterKey
	RegisterKeyFile string

	// OnPush, if non-nil, is called after a PUSH frame's entries have
	// been validated and persisted, once per PUSH that added at least
	// one new entry. It runs outside the vault locks, on the goroutine
	// serving the frame, so it must not block for long. Entries added
	// via AddEntries do NOT fire it.
	OnPush func(vid vault.VaultID)
}

// Server is a vault-aware HTTP handler. Use NewWithOptions.
type Server struct {
	dataDir string
	quotas  Quotas
	clock   func() int64
	regKeys []vault.ChannelKey // K_reg per RegisterKey; none = registration disabled
	onPush  func(vault.VaultID)
	mux     *http.ServeMux // the protocol routes, built once

	mu     sync.Mutex
	vaults map[vault.VaultID]*vaultState
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
}

// NewWithOptions constructs a Server.
func NewWithOptions(opt Options) (*Server, error) {
	if opt.RegisterKey != nil && opt.RegisterKeyFile != "" {
		return nil, errors.New("server: RegisterKey and RegisterKeyFile are mutually exclusive")
	}
	var regKeys []vault.ChannelKey // none = registration disabled
	switch {
	case opt.RegisterKey != nil:
		regKeys = []vault.ChannelKey{vault.DeriveRegKey(*opt.RegisterKey)}
	case opt.RegisterKeyFile != "":
		loaded, err := loadRegisterKeys(opt.RegisterKeyFile)
		if err != nil {
			return nil, err
		}
		regKeys = loaded
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
		onPush:  opt.OnPush,
		vaults:  make(map[vault.VaultID]*vaultState),
	}
	s.mux = s.routes()
	if opt.DataDir != "" {
		if err := s.loadPersistedVaults(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// loadRegisterKeys reads a register-key file (see Options) and returns
// the K_reg of each key in it, in file order. A file with no key is an
// error: registration is disabled by not configuring a file, never by
// an empty one.
func loadRegisterKeys(path string) ([]vault.ChannelKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("server: register-key-file %s: %w", path, err)
	}
	var keys []vault.ChannelKey
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rk, err := vault.DecodeRegisterKey(line)
		if err != nil {
			return nil, fmt.Errorf("server: register-key-file %s:%d: %w", path, i+1, err)
		}
		keys = append(keys, vault.DeriveRegKey(rk))
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("server: register-key-file %s: no register key", path)
	}
	return keys, nil
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
		vidBytes, err := hex.DecodeString(d.Name())
		if err != nil || len(vidBytes) != vault.VaultIDSize {
			continue
		}
		var vid vault.VaultID
		copy(vid[:], vidBytes)
		dir := filepath.Join(s.dataDir, d.Name())
		ak, err := readAuthKey(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // crashed registration; unreachable until re-register
			}
			// A single damaged vault must not take the whole server
			// offline; skip it loudly and serve the rest. It stays
			// reachable via re-registration under the same RootKey.
			log.Printf("server: skipping vault %s: bad auth.key: %v", d.Name(), err)
			continue
		}
		ls, err := store.OpenLogStore(filepath.Join(dir, "entries.log"), logLockWait)
		if err != nil {
			log.Printf("server: skipping vault %s: %v", d.Name(), err)
			continue
		}
		st := newVaultState(ls, ak, dir)
		for _, e := range ls.Sorted() {
			st.bytes += uint64(e.Size())
		}
		s.vaults[vid] = st
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
	}
}

// Close releases every vault's backing store, including the advisory
// file locks its log holds. The server must not be used afterwards; it
// is the clean-shutdown counterpart to NewWithOptions.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for _, st := range s.vaults {
		if c, ok := st.s.(io.Closer); ok {
			if err := c.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// HandleFrame processes one request frame and returns a response: either
// a sealed RESP frame, or a single pre-channel error byte. A non-nil
// error is an I/O problem that prevents any response.
func (s *Server) HandleFrame(req []byte) ([]byte, error) {
	kind, vid, nonce, sealed, err := wire.ParseHeader(req)
	if err != nil {
		if errors.Is(err, wire.ErrVersion) {
			return wire.ErrBadVersion.Reply(), nil
		}
		return wire.ErrBadFrame.Reply(), nil
	}
	switch kind {
	case wire.KindRegister:
		return s.handleRegister(vid, nonce, sealed)
	case wire.KindSync:
		return s.handleSync(vid, nonce, sealed)
	default:
		return wire.ErrBadKind.Reply(), nil
	}
}

func (s *Server) handleRegister(vid vault.VaultID, nonce [vault.NonceSize]byte, sealed []byte) ([]byte, error) {
	var payload []byte
	opened := false // no keys = registration disabled
	for _, k := range s.regKeys {
		if p, err := wire.OpenFrame(k, wire.KindRegister, vid, nonce, sealed); err == nil {
			payload, opened = p, true
			break
		}
	}
	if !opened {
		return wire.ErrBadAuth.Reply(), nil // gate: no RegisterKey opens it
	}
	if len(payload) != vault.AuthKeySize {
		return wire.ErrBadFrame.Reply(), nil
	}
	var ak vault.AuthKey
	copy(ak[:], payload)

	s.mu.Lock()
	st, exists := s.vaults[vid]
	if exists {
		s.mu.Unlock()
		if subtle.ConstantTimeCompare(st.authKey[:], ak[:]) != 1 {
			return wire.ErrBadAuth.Reply(), nil // rebind attempt
		}
		return s.sealResp(vid, st.s2c, nonce, wire.StatusOK, nil)
	}
	if uint64(len(s.vaults)) >= s.quotas.MaxVaults {
		s.mu.Unlock()
		return s.sealResp(vid, vault.DeriveS2C(ak), nonce, wire.StatusQuota, nil)
	}
	st, err := s.createVaultLocked(vid, ak)
	s.mu.Unlock()
	if err != nil {
		log.Printf("server: create vault %x: %v", vid[:], err)
		return wire.ErrTryLater.Reply(), nil
	}
	return s.sealResp(vid, st.s2c, nonce, wire.StatusOK, nil)
}

func (s *Server) handleSync(vid vault.VaultID, nonce [vault.NonceSize]byte, sealed []byte) ([]byte, error) {
	s.mu.Lock()
	st, ok := s.vaults[vid]
	s.mu.Unlock()
	if !ok {
		return wire.ErrUnknownVault.Reply(), nil
	}
	payload, err := wire.OpenFrame(st.c2s, wire.KindSync, vid, nonce, sealed)
	if err != nil {
		return wire.ErrBadAuth.Reply(), nil
	}
	req, err := wire.ParseSync(payload)
	if err != nil {
		return s.sealResp(vid, st.s2c, nonce, wire.StatusBadRequest, nil)
	}

	// The op runs under the vault lock; OnPush fires after it is
	// released so the hook can safely call back into the server.
	resp, pushedNew, err := func() ([]byte, bool, error) {
		st.mu.Lock()
		defer st.mu.Unlock()

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
	return added, nil
}

// validateNew enforces quota -- and, when checkSkew is set, timestamp
// skew -- on the entries the server does not already hold; re-pushing
// an accepted entry is a no-op and not re-judged. The wire PUSH path
// checks skew; the owner API does not (an owner legitimately writes
// entries carrying preserved historical timestamps). Returns the new
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
		if checkSkew {
			d := int64(e.TsMs) - now
			if d < -skew || d > skew {
				return nil, errSkew
			}
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

// errSkew rejects a pushed entry whose ts_ms is too far from the
// server's clock (TSSkewMs).
var errSkew = errors.New("server: entry timestamp outside the allowed skew")

// sealResp builds a RESP frame under s2c, echoing reqNonce.
func (s *Server) sealResp(vid vault.VaultID, s2c vault.ChannelKey, reqNonce [vault.NonceSize]byte, status wire.Status, entries []vault.Entry) ([]byte, error) {
	respNonce, err := vault.FrameNonce(vid)
	if err != nil {
		return nil, err
	}
	payload := wire.Resp{Status: status, ReqNonce: reqNonce, Entries: entries}.Marshal()
	return wire.SealFrame(wire.KindResp, vid, respNonce, s2c, payload)
}

// createVaultLocked constructs and persists a new vaultState. s.mu held.
func (s *Server) createVaultLocked(vid vault.VaultID, ak vault.AuthKey) (*vaultState, error) {
	if s.dataDir == "" {
		st := newVaultState(store.NewMemStore(), ak, "")
		s.vaults[vid] = st
		return st, nil
	}
	dir := filepath.Join(s.dataDir, hex.EncodeToString(vid[:]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := writeAuthKey(dir, ak); err != nil {
		return nil, err
	}
	ls, err := store.OpenLogStore(filepath.Join(dir, "entries.log"), logLockWait)
	if err != nil {
		return nil, err
	}
	st := newVaultState(ls, ak, dir)
	s.vaults[vid] = st
	return st, nil
}

// ErrVaultInUse reports a vault whose entry log a running server holds.
var ErrVaultInUse = errors.New("server: vault is held by a running server")

// RemoveVault deletes vid's directory under dataDir: its auth.key and
// its entry log, for good. It is the offline counterpart to DropVault,
// for an operator with no Server in hand. It takes the entry log's lock
// first, so it refuses with ErrVaultInUse while a server is serving the
// vault: a server keeps every vault's log open from start to stop, and
// deleting the files under it changes nothing it serves until it
// restarts. Returns ErrUnknownVault if dataDir has no such vault.
//
// Removal is not revocation: any device still holding the RootKey and
// a RegisterKey re-registers the vault and pushes its copy back.
func RemoveVault(dataDir string, vid vault.VaultID) error {
	dir := filepath.Join(dataDir, hex.EncodeToString(vid[:]))
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrUnknownVault
		}
		return err
	}
	logPath := filepath.Join(dir, "entries.log")
	var held io.Closer
	if _, err := os.Stat(logPath); err == nil {
		ls, err := store.OpenLogStore(logPath, 0)
		if errors.Is(err, store.ErrLocked) {
			return ErrVaultInUse
		}
		if err != nil {
			return err
		}
		held = ls
	}
	// auth.key goes first: a crash part-way leaves a directory that
	// the next start skips rather than adopts.
	err := os.Remove(authKeyPath(dir))
	if err == nil || errors.Is(err, os.ErrNotExist) {
		err = os.RemoveAll(dir)
	}
	if held != nil {
		_ = held.Close()
	}
	if err != nil {
		return err
	}
	if d, err := os.Open(dataDir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// --- per-vault auth.key persistence ---

func authKeyPath(dir string) string { return filepath.Join(dir, "auth.key") }

func writeAuthKey(dir string, ak vault.AuthKey) error {
	path := authKeyPath(dir)
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(ak[:]); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	// Flush the rename (and the new file's directory entry) so a crash
	// cannot leave auth.key partially written or absent.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func readAuthKey(dir string) (vault.AuthKey, error) {
	b, err := os.ReadFile(authKeyPath(dir))
	if err != nil {
		return vault.AuthKey{}, err
	}
	if len(b) != vault.AuthKeySize {
		return vault.AuthKey{}, fmt.Errorf("server: auth.key is %d bytes, want %d", len(b), vault.AuthKeySize)
	}
	var ak vault.AuthKey
	copy(ak[:], b)
	return ak, nil
}

// --- HTTP ---

// SyncPath is the one route the protocol defines, relative to
// whatever prefix a deployment mounts the server under: a station may
// serve it at /kv/v1/sync, a bare kvserve at /v1/sync. The client
// builds the same path in client.HTTPTransport.
const SyncPath = "/v1/sync"

// routes builds the server's route table. The path and method live in
// the pattern, so http.ServeMux answers a wrong method with 405 and a
// correct Allow header.
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+SyncPath, s.serveSync)
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
	resp, err := s.HandleFrame(body)
	if err != nil {
		log.Printf("server: handle frame: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(resp)
}
