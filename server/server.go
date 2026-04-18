// Package server implements the ASCON-KV server: a blind relay that
// holds a content-addressed set of entries per VaultID and serves the
// sync protocol defined in PROTOCOL.txt.
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

// Quotas configure per-vault limits enforced by the server. Defaults
// come from DefaultQuotas; override via Options.
type Quotas struct {
	MaxEntries     uint64 // hard cap on entries per vault
	MaxVaultBytes  uint64 // hard cap on total entry bytes per vault
	MaxVaults      uint64 // hard cap on number of registered vaults; 0 = unlimited
	TSSkewMs       int64  // accepted |ts_ms - now| in milliseconds
}

// DefaultQuotas are the protocol-recommended limits. The MaxVaults
// default is finite so an out-of-the-box server is DoS-bounded; raise
// it explicitly for multi-tenant deployments.
func DefaultQuotas() Quotas {
	return Quotas{
		MaxEntries:    1_000_000,
		MaxVaultBytes: 1 << 30,            // 1 GiB
		MaxVaults:     10_000,
		TSSkewMs:      24 * 60 * 60 * 1000, // 24 h
	}
}

// Options configure a Server at construction time.
type Options struct {
	DataDir string       // empty for memory-only
	Quotas  Quotas       // zero value → DefaultQuotas
	Clock   func() int64 // unix ms; nil → time.Now
}

// Server is a vault-aware HTTP handler. The zero value is not ready;
// use New or NewWithOptions.
type Server struct {
	dataDir string
	quotas  Quotas
	clock   func() int64

	mu     sync.Mutex
	vaults map[vault.VaultID]*vaultState
}

type vaultState struct {
	mu      sync.Mutex
	s       store.Store
	authKey vault.AuthKey
	bytes   uint64
	dir     string // per-vault directory; empty for memory-only
}

// New returns a Server rooted at dataDir with default quotas. dataDir
// is created if missing. Pass an empty string for a purely in-memory
// server (no persistence).
func New(dataDir string) (*Server, error) {
	return NewWithOptions(Options{DataDir: dataDir})
}

// NewWithOptions is the full constructor.
func NewWithOptions(opt Options) (*Server, error) {
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
		vaults:  make(map[vault.VaultID]*vaultState),
	}
	if opt.DataDir != "" {
		if err := s.loadPersistedVaults(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// loadPersistedVaults walks dataDir and rebuilds any on-disk vaults
// into the in-memory registry. Each vault directory is expected to
// contain entries.log and auth.key.
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
		authKey, err := readAuthKey(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// No auth.key — legacy or crashed registration;
				// skip without loading. Its entries remain on
				// disk but are unreachable until a client
				// re-registers.
				continue
			}
			return err
		}
		logPath := filepath.Join(dir, "entries.log")
		ls, err := store.OpenLogStore(logPath)
		if err != nil {
			return err
		}
		st := &vaultState{
			s:       ls,
			authKey: authKey,
			dir:     dir,
		}
		for _, e := range ls.Sorted() {
			st.bytes += uint64(e.Size())
		}
		s.vaults[vid] = st
	}
	return nil
}

// HandleSync processes one request body and returns a response body.
// All application-level errors are signaled in the response status
// byte; a non-nil err return indicates an I/O or framing problem that
// prevents producing any response at all.
func (s *Server) HandleSync(requestBytes []byte) ([]byte, error) {
	req, err := wire.UnmarshalRequest(requestBytes)
	if err != nil {
		// We cannot trust VaultID or MAC — respond with zero key and
		// zero request MAC. The client's response-MAC check will fail
		// (it won't have a matching key) which is fine: a broken
		// request yields a response the client can't accept.
		return s.errorResponse(vault.AuthKey{}, [vault.MACSize]byte{}, wire.StatusBadRequest)
	}

	st, ak, status := s.resolveVault(req)
	if status != wire.StatusOK {
		return s.errorResponse(ak, req.MAC, status)
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	// Quota + clock-skew validation (quotas are pre-ingest worst-case).
	if qstatus := s.validatePushes(st, req); qstatus != wire.StatusOK {
		return s.errorResponse(ak, req.MAC, qstatus)
	}

	// Ingest pushes.
	for _, e := range req.Pushes {
		added, err := st.s.Add(e)
		if err != nil {
			return nil, err
		}
		if added {
			st.bytes += uint64(e.Size())
		}
	}

	// Terminal/ladder comparison.
	var serverTerminal [16]byte
	if st.s.Len() == 0 {
		serverTerminal = vault.TerminalHash(nil)
	} else {
		serverTerminal = st.s.TerminalHash()
	}

	var window uint32
	if serverTerminal != req.Terminal {
		sizes := vault.LadderSizes(req.N)
		for i, k := range sizes {
			if i >= len(req.Ladder) {
				break // defensive; codec already validates this
			}
			var serverH [16]byte
			if st.s.Len() == 0 {
				serverH = vault.LadderHashAt(nil, k)
			} else {
				serverH = st.s.LadderHashAt(k)
			}
			if serverH != req.Ladder[i] {
				window = k
				break
			}
		}
		if window == 0 {
			window = max(st.s.Len(), req.N)
		}
	}

	resp := wire.Response{Status: wire.StatusOK}
	if window > 0 {
		sorted := st.s.Sorted()
		d := min(window, uint32(len(sorted)))
		if d > 0 {
			resp.Entries = append(resp.Entries, sorted[uint32(len(sorted))-d:]...)
		}
		resp.Window = window
	}
	out, err := wire.SignResponse(ak, req.MAC, resp)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// resolveVault looks up (or optionally TOFU-registers) the vault for
// req.VaultID and returns its state along with the AuthKey to use
// and a status code. A StatusOK means the caller may proceed; any
// other status is a terminal reason to return an error response.
func (s *Server) resolveVault(req wire.Request) (*vaultState, vault.AuthKey, byte) {
	s.mu.Lock()
	st, exists := s.vaults[req.VaultID]
	s.mu.Unlock()

	if exists {
		if req.IsRegister() {
			// Idempotent re-registration: reg_authkey must match.
			if subtle.ConstantTimeCompare(st.authKey[:], req.RegAuth[:]) != 1 {
				return nil, vault.AuthKey{}, wire.StatusBadMAC
			}
		}
		if err := wire.VerifyRequestMAC(st.authKey, req); err != nil {
			return nil, st.authKey, wire.StatusBadMAC
		}
		return st, st.authKey, wire.StatusOK
	}

	// Vault doesn't exist.
	if !req.IsRegister() {
		return nil, vault.AuthKey{}, wire.StatusUnknownVault
	}

	// First-time registration: verify MAC under the claimed AuthKey.
	if err := wire.VerifyRequestMAC(req.RegAuth, req); err != nil {
		return nil, vault.AuthKey{}, wire.StatusBadMAC
	}

	s.mu.Lock()
	// Double-check under the write lock.
	if existing, ok := s.vaults[req.VaultID]; ok {
		s.mu.Unlock()
		if subtle.ConstantTimeCompare(existing.authKey[:], req.RegAuth[:]) != 1 {
			return nil, vault.AuthKey{}, wire.StatusBadMAC
		}
		return existing, existing.authKey, wire.StatusOK
	}
	if s.quotas.MaxVaults != 0 && uint64(len(s.vaults)) >= s.quotas.MaxVaults {
		s.mu.Unlock()
		return nil, vault.AuthKey{}, wire.StatusQuota
	}
	st, err := s.createVaultLocked(req.VaultID, req.RegAuth)
	s.mu.Unlock()
	if err != nil {
		log.Printf("server: create vault %x: %v", req.VaultID[:], err)
		return nil, vault.AuthKey{}, wire.StatusBadRequest
	}
	return st, st.authKey, wire.StatusOK
}

// createVaultLocked constructs a new vaultState and persists it. s.mu
// must be held.
func (s *Server) createVaultLocked(vid vault.VaultID, ak vault.AuthKey) (*vaultState, error) {
	if s.dataDir == "" {
		st := &vaultState{
			s:       store.NewMemStore(),
			authKey: ak,
		}
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
	st := &vaultState{
		s:       ls,
		authKey: ak,
		dir:     dir,
	}
	s.vaults[vid] = st
	return st, nil
}

// validatePushes enforces clock-skew and quota bounds without ingesting.
// Returns StatusOK if pushes may proceed.
func (s *Server) validatePushes(st *vaultState, req wire.Request) byte {
	now := s.clock()
	skew := s.quotas.TSSkewMs
	for _, e := range req.Pushes {
		d := int64(e.TsMs) - now
		if d < -skew || d > skew {
			return wire.StatusBadRequest
		}
	}
	// Worst-case count/byte (ignores dedup — conservative).
	if uint64(st.s.Len())+uint64(len(req.Pushes)) > s.quotas.MaxEntries {
		return wire.StatusQuota
	}
	var pushBytes uint64
	for _, e := range req.Pushes {
		pushBytes += uint64(e.Size())
	}
	if st.bytes+pushBytes > s.quotas.MaxVaultBytes {
		return wire.StatusQuota
	}
	return wire.StatusOK
}

// errorResponse builds a signed error response with the given status.
// It never returns the application error to the client; the status
// byte carries all the information the client is entitled to.
func (s *Server) errorResponse(ak vault.AuthKey, reqMAC [vault.MACSize]byte, status byte) ([]byte, error) {
	resp := wire.Response{Status: status}
	return wire.SignResponse(ak, reqMAC, resp)
}

// --- per-vault auth.key persistence ---

func authKeyPath(dir string) string { return filepath.Join(dir, "auth.key") }

func writeAuthKey(dir string, ak vault.AuthKey) error {
	path := authKeyPath(dir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, ak[:], 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

// ServeHTTP exposes HandleSync as POST /v1/sync.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/v1/sync" {
		http.NotFound(w, r)
		return
	}
	const maxBody = 64 << 20 // 64 MB
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		// Keep the client-visible message generic; log the detail.
		log.Printf("server: read body: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	resp, err := s.HandleSync(body)
	if err != nil {
		log.Printf("server: handle sync: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(resp)
}
