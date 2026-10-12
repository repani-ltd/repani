// Owner API: direct vault access for a co-resident owner (a process
// that embeds the Server and holds vault keys), bypassing the wire.
package server

import (
	"crypto/subtle"
	"errors"
	"io"

	"repani.com/kv/vault"
)

// Sentinel errors for the owner API.
var (
	ErrUnknownVault    = errors.New("server: unknown vault")
	ErrAuthKeyMismatch = errors.New("server: vault exists with a different auth key")
	ErrQuota           = errors.New("server: quota exceeded")
)

// EnsureVault creates the vault for vid with the given channel root, or
// verifies that an existing one carries the same AuthKey. Idempotent.
// It is the owner-side equivalent of a REGISTER and works whether or
// not in-band registration is enabled; MaxVaults does not apply, since
// the owner is the operator.
func (s *Server) EnsureVault(vid vault.VaultID, ak vault.AuthKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.vaults[vid]; ok {
		if subtle.ConstantTimeCompare(st.authKey[:], ak[:]) != 1 {
			return ErrAuthKeyMismatch
		}
		return nil
	}
	_, err := s.createVaultLocked(vid, ak)
	return err
}

// DropVault deletes the vault for good: it stops being served, so
// frames for vid get UNKNOWN_VAULT, its store is closed, and its
// directory (auth.key and entry log) is removed. DropVaultDir does the
// same for a server that is not running. Returns ErrUnknownVault if
// vid is not being served.
//
// Removal is not revocation: a device still holding the RootKey and a
// RegisterKey re-registers the vault and pushes its copy back.
func (s *Server) DropVault(vid vault.VaultID) error {
	s.mu.Lock()
	st, ok := s.vaults[vid]
	delete(s.vaults, vid)
	s.mu.Unlock()
	if !ok {
		return ErrUnknownVault
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	var closeErr error
	if c, ok := st.s.(io.Closer); ok {
		closeErr = c.Close()
	}
	if st.dir == "" {
		return closeErr // in memory: nothing on disk
	}
	return errors.Join(closeErr, removeVaultDir(s.dataDir, st.dir))
}

// Entries returns a canonical-order snapshot of the vault's entry set.
// The slice is the caller's to read; entries must not be mutated.
func (s *Server) Entries(vid vault.VaultID) ([]vault.Entry, error) {
	s.mu.Lock()
	st, ok := s.vaults[vid]
	s.mu.Unlock()
	if !ok {
		return nil, ErrUnknownVault
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Sorted(), nil
}

// AddEntries adds entries to the vault, applying the same quota
// validation as a PUSH frame but not its timestamp-skew check: the
// owner is trusted and legitimately writes entries carrying preserved
// historical timestamps (e.g. copies that keep their source ts_ms).
// Idempotent for entries the vault already holds. Unlike a PUSH it
// never fires Options.OnPush, so an owner writing back into a vault
// from inside an OnPush-triggered handler cannot re-trigger itself.
func (s *Server) AddEntries(vid vault.VaultID, entries []vault.Entry) error {
	s.mu.Lock()
	st, ok := s.vaults[vid]
	s.mu.Unlock()
	if !ok {
		return ErrUnknownVault
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	newEntries, err := s.validateNew(st, entries, false)
	if err != nil {
		return err
	}
	_, err = addValidatedLocked(st, newEntries)
	return err
}

// SetNoPush marks the vault as accepting nothing new over the wire: a
// PUSH carrying any entry the vault does not already hold gets
// StatusQuota (a zero-entry push budget), while a dedup no-op still
// acks OK so a read-only client's pull-then-push walk converges.
// PROBE and PULL are unaffected, as is the owner's AddEntries.
// Intended for vaults whose RootKey is published to grant read access
// -- but note the flag is SERVER POLICY, not cryptography: a published
// RootKey hands every reader AEAD_Key and AuthKey, i.e. full write and
// forge capability at the crypto layer, and this flag on this server
// is the only thing refusing those writes (DESIGN.t, Publishing a
// RootKey). It
// is not persisted: an embedder re-asserts it right after EnsureVault
// on every start, and before then the vault does not exist, so no
// unguarded window opens.
func (s *Server) SetNoPush(vid vault.VaultID, v bool) error {
	s.mu.Lock()
	st, ok := s.vaults[vid]
	s.mu.Unlock()
	if !ok {
		return ErrUnknownVault
	}
	st.mu.Lock()
	st.noPush = v
	st.mu.Unlock()
	return nil
}

// Vaults returns the IDs of every vault currently being served. An
// embedder uses it to sweep vaults its own records no longer name
// (e.g. residue of a crash between creating and announcing one).
func (s *Server) Vaults() []vault.VaultID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]vault.VaultID, 0, len(s.vaults))
	for vid := range s.vaults {
		out = append(out, vid)
	}
	return out
}
