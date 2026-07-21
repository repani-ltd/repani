// Package server implements the ASCON-KV v1 server: a blind relay that
// holds a content-addressed set of entries per VaultID and serves the
// paged sync protocol defined in PROTOCOL.txt. It opens every frame
// under a channel key, never under AEAD_Key, so it never sees plaintext.
package server

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pavlos/kv/store"
	"github.com/pavlos/kv/vault"
	"github.com/pavlos/kv/wire"
)

// Quotas configure per-vault limits.
type Quotas struct {
	MaxEntries    uint64
	MaxVaultBytes uint64
	MaxVaults     uint64 // 0 = unlimited
	TSSkewMs      int64
}

// DefaultQuotas are the protocol-recommended limits. Paging decouples
// vault size from the frame size, so MaxVaultBytes is a storage cap, not
// a transport one.
func DefaultQuotas() Quotas {
	return Quotas{
		MaxEntries:    1_000_000,
		MaxVaultBytes: 1 << 30, // 1 GiB
		MaxVaults:     10_000,
		TSSkewMs:      24 * 60 * 60 * 1000,
	}
}

// Options configure a Server. At most one of RegisterKey or
// RegisterKeyFile may be set. If neither is set, in-band registration
// is disabled: every REGISTER frame is rejected with BAD_AUTH, and
// vaults can only be created by a co-resident owner via EnsureVault.
// There is never permissionless registration.
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
	regOK   bool             // false = registration disabled
	regKey  vault.ChannelKey // K_reg = XOF("kv/reg0/" || RegisterKey)
	onPush  func(vault.VaultID)

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
}

// NewWithOptions constructs a Server.
func NewWithOptions(opt Options) (*Server, error) {
	if opt.RegisterKey != nil && opt.RegisterKeyFile != "" {
		return nil, errors.New("server: RegisterKey and RegisterKeyFile are mutually exclusive")
	}
	regOK := true
	var regKey vault.RegisterKey
	switch {
	case opt.RegisterKey != nil:
		regKey = *opt.RegisterKey
	case opt.RegisterKeyFile != "":
		loaded, err := loadRegisterKey(opt.RegisterKeyFile)
		if err != nil {
			return nil, err
		}
		regKey = loaded
	default:
		regOK = false // registration disabled
	}

	if opt.DataDir != "" {
		if err := os.MkdirAll(opt.DataDir, 0o700); err != nil {
			return nil, err
		}
	}
	q := opt.Quotas
	if q == (Quotas{}) {
		q = DefaultQuotas()
	}
	clk := opt.Clock
	if clk == nil {
		clk = func() int64 { return time.Now().UnixMilli() }
	}
	s := &Server{
		dataDir: opt.DataDir,
		quotas:  q,
		clock:   clk,
		regOK:   regOK,
		onPush:  opt.OnPush,
		vaults:  make(map[vault.VaultID]*vaultState),
	}
	if regOK {
		s.regKey = vault.DeriveRegKey(regKey)
	}
	regKey = vault.RegisterKey{} // scrub
	if opt.DataDir != "" {
		if err := s.loadPersistedVaults(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func loadRegisterKey(path string) (vault.RegisterKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return vault.RegisterKey{}, fmt.Errorf("server: register-key-file %s: %w", path, err)
	}
	rk, err := vault.DecodeRegisterKey(string(b))
	if err != nil {
		return vault.RegisterKey{}, fmt.Errorf("server: register-key-file %s: %w", path, err)
	}
	for i := range b {
		b[i] = 0
	}
	return rk, nil
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
		ls, err := store.OpenLogStore(filepath.Join(dir, "entries.log"))
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
			return []byte{wire.ErrBadVersion}, nil
		}
		return []byte{wire.ErrBadFrame}, nil
	}
	switch kind {
	case wire.KindRegister:
		return s.handleRegister(vid, nonce, sealed)
	case wire.KindSync:
		return s.handleSync(vid, nonce, sealed)
	default:
		return []byte{wire.ErrBadKind}, nil
	}
}

func (s *Server) handleRegister(vid vault.VaultID, nonce [vault.NonceSize]byte, sealed []byte) ([]byte, error) {
	if !s.regOK {
		return []byte{wire.ErrBadAuth}, nil // registration disabled
	}
	payload, err := wire.OpenFrame(s.regKey, wire.KindRegister, vid, nonce, sealed)
	if err != nil {
		return []byte{wire.ErrBadAuth}, nil // gate: wrong RegisterKey
	}
	if len(payload) != vault.AuthKeySize {
		return []byte{wire.ErrBadFrame}, nil
	}
	var ak vault.AuthKey
	copy(ak[:], payload)

	s.mu.Lock()
	st, exists := s.vaults[vid]
	if exists {
		s.mu.Unlock()
		if subtle.ConstantTimeCompare(st.authKey[:], ak[:]) != 1 {
			return []byte{wire.ErrBadAuth}, nil // rebind attempt
		}
		return s.sealResp(vid, st.s2c, nonce, wire.StatusOK, nil)
	}
	if s.quotas.MaxVaults != 0 && uint64(len(s.vaults)) >= s.quotas.MaxVaults {
		s.mu.Unlock()
		return s.sealResp(vid, vault.DeriveS2C(ak), nonce, wire.StatusQuota, nil)
	}
	st, err = s.createVaultLocked(vid, ak)
	s.mu.Unlock()
	if err != nil {
		log.Printf("server: create vault %x: %v", vid[:], err)
		return []byte{wire.ErrTryLater}, nil
	}
	return s.sealResp(vid, st.s2c, nonce, wire.StatusOK, nil)
}

func (s *Server) handleSync(vid vault.VaultID, nonce [vault.NonceSize]byte, sealed []byte) ([]byte, error) {
	s.mu.Lock()
	st, ok := s.vaults[vid]
	s.mu.Unlock()
	if !ok {
		return []byte{wire.ErrUnknownVault}, nil
	}
	payload, err := wire.OpenFrame(st.c2s, wire.KindSync, vid, nonce, sealed)
	if err != nil {
		return []byte{wire.ErrBadAuth}, nil
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
			var newEntries []vault.Entry
			newEntries, status = s.validateNew(st, req.Entries)
			if status == wire.StatusOK {
				var err error
				pushed, err = addValidatedLocked(st, newEntries)
				if err != nil {
					return nil, false, err
				}
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
func addValidatedLocked(st *vaultState, entries []vault.Entry) (int, error) {
	added := 0
	for _, e := range entries {
		ok, err := st.s.Add(e)
		if err != nil {
			return added, err
		}
		if ok {
			st.bytes += uint64(e.Size())
			added++
		}
	}
	return added, nil
}

// validateNew enforces skew and quota on the entries the server does not
// already hold; re-pushing an accepted entry is a no-op and not
// re-judged. Returns the new entries and StatusOK, or nil and a
// rejecting status.
func (s *Server) validateNew(st *vaultState, pushes []vault.Entry) ([]vault.Entry, byte) {
	now := s.clock()
	skew := s.quotas.TSSkewMs
	newEntries := make([]vault.Entry, 0, len(pushes))
	var newBytes uint64
	for _, e := range pushes {
		if st.s.Has(e.ID()) {
			continue
		}
		d := int64(e.TsMs) - now
		if d < -skew || d > skew {
			return nil, wire.StatusBadRequest
		}
		newEntries = append(newEntries, e)
		newBytes += uint64(e.Size())
	}
	if uint64(st.s.Len())+uint64(len(newEntries)) > s.quotas.MaxEntries {
		return nil, wire.StatusQuota
	}
	if st.bytes+newBytes > s.quotas.MaxVaultBytes {
		return nil, wire.StatusQuota
	}
	return newEntries, wire.StatusOK
}

// sealResp builds a RESP frame under s2c, echoing reqNonce.
func (s *Server) sealResp(vid vault.VaultID, s2c vault.ChannelKey, reqNonce [vault.NonceSize]byte, status byte, entries []vault.Entry) ([]byte, error) {
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
	ls, err := store.OpenLogStore(filepath.Join(dir, "entries.log"))
	if err != nil {
		return nil, err
	}
	st := newVaultState(ls, ak, dir)
	s.vaults[vid] = st
	return st, nil
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

// ServeHTTP exposes HandleFrame as POST /v1/sync.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/v1/sync" {
		http.NotFound(w, r)
		return
	}
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
