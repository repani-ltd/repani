package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pavlos/kv/vault"
	"github.com/pavlos/kv/wire"
)

// ownerServer returns a registration-disabled server and a harness for
// rk whose vault has been created via EnsureVault.
func ownerServer(t *testing.T, dataDir string) (*Server, vault.RootKey, *harness) {
	t.Helper()
	srv, err := NewWithOptions(Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	rk, _ := vault.NewRandomRootKey()
	if err := srv.EnsureVault(vault.DeriveVaultID(rk), vault.DeriveAuthKey(rk)); err != nil {
		t.Fatal(err)
	}
	return srv, rk, newHarness(t, srv, rk, testRegKey)
}

func TestRegistrationDisabled(t *testing.T) {
	srv, _, h := ownerServer(t, "")

	// Every REGISTER frame is rejected, even a well-formed one.
	if _, pre := h.register(); pre != wire.ErrBadAuth {
		t.Fatalf("register on disabled server: pre=0x%02x, want BAD_AUTH", pre)
	}

	// The EnsureVault'd vault syncs normally over the channel keys.
	if resp, pre := h.probe(vault.SetHash(nil)); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("probe: pre=0x%02x status=%d", pre, resp.Status)
	}

	// A vault the owner did not create stays unknown.
	other, _ := vault.NewRandomRootKey()
	h2 := newHarness(t, srv, other, testRegKey)
	if _, pre := h2.probe([16]byte{}); pre != wire.ErrUnknownVault {
		t.Fatalf("unknown vault probe: pre=0x%02x, want UNKNOWN_VAULT", pre)
	}
}

func TestEnsureVault_IdempotentAndMismatch(t *testing.T) {
	srv, rk, _ := ownerServer(t, "")
	vid := vault.DeriveVaultID(rk)

	if err := srv.EnsureVault(vid, vault.DeriveAuthKey(rk)); err != nil {
		t.Fatalf("idempotent EnsureVault: %v", err)
	}
	other, _ := vault.NewRandomRootKey()
	if err := srv.EnsureVault(vid, vault.DeriveAuthKey(other)); !errors.Is(err, ErrAuthKeyMismatch) {
		t.Fatalf("mismatched EnsureVault: %v, want ErrAuthKeyMismatch", err)
	}
}

func TestOnPush(t *testing.T) {
	var fired []vault.VaultID
	srv, err := NewWithOptions(Options{OnPush: func(vid vault.VaultID) { fired = append(fired, vid) }})
	if err != nil {
		t.Fatal(err)
	}
	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)
	if err := srv.EnsureVault(vid, vault.DeriveAuthKey(rk)); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, srv, rk, testRegKey)

	e := mkEntry(t, rk, nowMs(), "k", "\x00v")
	if resp, _ := h.push([]vault.Entry{e}); resp.Status != wire.StatusOK {
		t.Fatalf("push status %d", resp.Status)
	}
	if len(fired) != 1 || fired[0] != vid {
		t.Fatalf("OnPush fired %d times, want once for the vault", len(fired))
	}

	// A dedup re-push adds nothing and does not fire.
	if resp, _ := h.push([]vault.Entry{e}); resp.Status != wire.StatusOK {
		t.Fatalf("re-push status %d", resp.Status)
	}
	if len(fired) != 1 {
		t.Fatalf("OnPush fired on dedup re-push")
	}

	// AddEntries never fires.
	e2 := mkEntry(t, rk, nowMs()+1, "k2", "\x00v2")
	if err := srv.AddEntries(vid, []vault.Entry{e2}); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 {
		t.Fatalf("OnPush fired on AddEntries")
	}
}

func TestEntriesAndAddEntries(t *testing.T) {
	const now = int64(1_700_000_000_000)
	q := DefaultQuotas()
	q.TSSkewMs = 60_000
	q.MaxEntries = 2
	srv, err := NewWithOptions(Options{Quotas: q, Clock: func() int64 { return now }})
	if err != nil {
		t.Fatal(err)
	}
	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)
	if err := srv.EnsureVault(vid, vault.DeriveAuthKey(rk)); err != nil {
		t.Fatal(err)
	}

	if _, err := srv.Entries(vault.VaultID{}); !errors.Is(err, ErrUnknownVault) {
		t.Fatalf("Entries on unknown vault: %v", err)
	}

	old := mkEntry(t, rk, uint64(now-120_000), "old", "\x00v")
	if err := srv.AddEntries(vid, []vault.Entry{old}); !errors.Is(err, ErrEntrySkew) {
		t.Fatalf("out-of-skew AddEntries: %v, want ErrEntrySkew", err)
	}

	a := mkEntry(t, rk, uint64(now+2), "a", "\x001")
	b := mkEntry(t, rk, uint64(now+1), "b", "\x002")
	if err := srv.AddEntries(vid, []vault.Entry{a, b}); err != nil {
		t.Fatal(err)
	}
	// Idempotent re-add.
	if err := srv.AddEntries(vid, []vault.Entry{a}); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	c := mkEntry(t, rk, uint64(now+3), "c", "\x003")
	if err := srv.AddEntries(vid, []vault.Entry{c}); !errors.Is(err, ErrQuota) {
		t.Fatalf("over-cap AddEntries: %v, want ErrQuota", err)
	}

	got, err := srv.Entries(vid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID() != b.ID() || got[1].ID() != a.ID() {
		t.Fatalf("Entries returned %d entries or wrong canonical order", len(got))
	}
}

func TestDropVault(t *testing.T) {
	dir := t.TempDir()
	srv, rk, h := ownerServer(t, dir)
	vid := vault.DeriveVaultID(rk)

	e := mkEntry(t, rk, nowMs(), "k", "\x00v")
	if resp, _ := h.push([]vault.Entry{e}); resp.Status != wire.StatusOK {
		t.Fatalf("push status %d", resp.Status)
	}

	if err := srv.DropVault(vid); err != nil {
		t.Fatal(err)
	}
	if err := srv.DropVault(vid); !errors.Is(err, ErrUnknownVault) {
		t.Fatalf("second drop: %v, want ErrUnknownVault", err)
	}
	if _, pre := h.probe([16]byte{}); pre != wire.ErrUnknownVault {
		t.Fatalf("probe after drop: pre=0x%02x, want UNKNOWN_VAULT", pre)
	}

	// On disk: entries.log archived, auth.key gone, so a restart does
	// not re-adopt the vault.
	logs, _ := filepath.Glob(filepath.Join(dir, "*", "entries.log"))
	if len(logs) != 1 {
		t.Fatalf("expected archived log, got %v", logs)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(logs[0]), "auth.key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("auth.key after drop: %v, want ErrNotExist", err)
	}
	srv2, err := NewWithOptions(Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv2.Entries(vid); !errors.Is(err, ErrUnknownVault) {
		t.Fatalf("dropped vault re-adopted after restart: %v", err)
	}

	// EnsureVault re-adopts the archived log.
	if err := srv2.EnsureVault(vid, vault.DeriveAuthKey(rk)); err != nil {
		t.Fatal(err)
	}
	got, err := srv2.Entries(vid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != e.ID() {
		t.Fatalf("re-adopted vault has %d entries", len(got))
	}
}
