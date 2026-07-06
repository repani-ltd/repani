// Owner API: direct vault access for a co-resident owner (a process
// that embeds the Server and holds vault keys), bypassing the wire.
package server

import (
	"crypto/subtle"
	"errors"
	"io"
	"os"

	"github.com/pavlos/kv/vault"
	"github.com/pavlos/kv/wire"
)

// Sentinel errors for the owner API.
var (
	ErrUnknownVault    = errors.New("server: unknown vault")
	ErrAuthKeyMismatch = errors.New("server: vault exists with a different auth key")
	ErrQuota           = errors.New("server: quota exceeded")
	ErrEntrySkew       = errors.New("server: entry timestamp outside skew window")
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

// DropVault forgets the vault: it is removed from serving and its store
// is closed, so subsequent frames for vid get UNKNOWN_VAULT. On disk the
// entry log is left in place as an archive, but auth.key is removed so
// the vault is not re-adopted at the next start. A later EnsureVault for
// the same vid re-adopts the old log; use a fresh RootKey (hence a fresh
// vid) to start clean. Returns ErrUnknownVault if vid is not being
// served.
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
	if st.dir != "" {
		if err := os.Remove(authKeyPath(st.dir)); err != nil {
			return err
		}
	}
	if c, ok := st.s.(io.Closer); ok {
		return c.Close()
	}
	return nil
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

// AddEntries adds entries to the vault, applying the same skew and
// quota validation as a PUSH frame. Idempotent for entries the vault
// already holds. Unlike a PUSH it never fires Options.OnPush, so an
// owner writing back into a vault from inside an OnPush-triggered
// handler cannot re-trigger itself.
func (s *Server) AddEntries(vid vault.VaultID, entries []vault.Entry) error {
	s.mu.Lock()
	st, ok := s.vaults[vid]
	s.mu.Unlock()
	if !ok {
		return ErrUnknownVault
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	newEntries, status := s.validateNew(st, entries)
	switch status {
	case wire.StatusOK:
	case wire.StatusQuota:
		return ErrQuota
	case wire.StatusBadRequest:
		return ErrEntrySkew
	default:
		return errors.New("server: unexpected validation status")
	}
	for _, e := range newEntries {
		added, err := st.s.Add(e)
		if err != nil {
			return err
		}
		if added {
			st.bytes += uint64(e.Size())
		}
	}
	return nil
}
