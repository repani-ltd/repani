package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"repani.com/kv/vault"
	"repani.com/kv/wire"
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

	// Far outside the push-skew window: the owner API accepts it (an
	// owner legitimately writes preserved historical timestamps), and
	// it counts toward MaxEntries below.
	old := mkEntry(t, rk, uint64(now-120_000), "old", "\x00v")
	if err := srv.AddEntries(vid, []vault.Entry{old}); err != nil {
		t.Fatalf("historical-ts AddEntries: %v", err)
	}

	a := mkEntry(t, rk, uint64(now+2), "a", "\x001")
	if err := srv.AddEntries(vid, []vault.Entry{a}); err != nil {
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
	if len(got) != 2 || got[0].ID() != old.ID() || got[1].ID() != a.ID() {
		t.Fatalf("Entries returned %d entries or wrong canonical order", len(got))
	}
}

func TestVaults(t *testing.T) {
	srv := mustNew(t, "")
	if got := srv.Vaults(); len(got) != 0 {
		t.Fatalf("fresh server serves %d vaults", len(got))
	}
	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)
	if err := srv.EnsureVault(vid, vault.DeriveAuthKey(rk)); err != nil {
		t.Fatal(err)
	}
	if got := srv.Vaults(); len(got) != 1 || got[0] != vid {
		t.Fatalf("Vaults = %v, want [%x]", got, vid[:4])
	}
	if err := srv.DropVault(vid); err != nil {
		t.Fatal(err)
	}
	if got := srv.Vaults(); len(got) != 0 {
		t.Fatalf("dropped vault still served")
	}
}

func TestSetNoPush(t *testing.T) {
	srv := mustNew(t, "")
	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)
	if err := srv.EnsureVault(vid, vault.DeriveAuthKey(rk)); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetNoPush(vault.VaultID{}, true); !errors.Is(err, ErrUnknownVault) {
		t.Fatalf("SetNoPush on unknown vault: %v", err)
	}
	if err := srv.SetNoPush(vid, true); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, srv, rk, testRegKey)

	// Wire PUSH is refused with QUOTA; nothing is stored.
	e := mkEntry(t, rk, nowMs(), "k", "\x00v")
	if resp, pre := h.push([]vault.Entry{e}); pre != 0 || resp.Status != wire.StatusQuota {
		t.Fatalf("push on no-push vault: pre=0x%02x status=%d, want quota", pre, resp.Status)
	}
	if got, _ := srv.Entries(vid); len(got) != 0 {
		t.Fatalf("no-push vault stored %d entries", len(got))
	}

	// The owner still writes, and PROBE / PULL still serve.
	if err := srv.AddEntries(vid, []vault.Entry{e}); err != nil {
		t.Fatal(err)
	}
	if resp, pre := h.pull(0, vault.EntryID{}); pre != 0 || resp.Status != wire.StatusOK || len(resp.Entries) != 1 {
		t.Fatalf("pull on no-push vault: pre=0x%02x status=%d entries=%d", pre, resp.Status, len(resp.Entries))
	}

	// A dedup no-op (re-pushing what the vault already holds) acks OK,
	// so a read-only client's pull-then-push walk converges.
	if resp, pre := h.push([]vault.Entry{e}); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("no-op push on no-push vault: pre=0x%02x status=%d, want OK", pre, resp.Status)
	}
	if got, _ := srv.Entries(vid); len(got) != 1 {
		t.Fatalf("no-op push changed the vault: %d entries", len(got))
	}

	// The flag clears.
	if err := srv.SetNoPush(vid, false); err != nil {
		t.Fatal(err)
	}
	e2 := mkEntry(t, rk, nowMs(), "k2", "\x00v2")
	if resp, pre := h.push([]vault.Entry{e2}); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("push after clearing no-push: pre=0x%02x status=%d", pre, resp.Status)
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

func TestRemoveVault(t *testing.T) {
	dir := t.TempDir()
	srv, rk, h := ownerServer(t, dir)
	vid := vault.DeriveVaultID(rk)

	e := mkEntry(t, rk, nowMs(), "k", "\x00v")
	if resp, _ := h.push([]vault.Entry{e}); resp.Status != wire.StatusOK {
		t.Fatalf("push status %d", resp.Status)
	}

	// A serving server holds the log; removal under it is refused.
	if err := RemoveVault(dir, vid); !errors.Is(err, ErrVaultInUse) {
		t.Fatalf("remove while served: %v, want ErrVaultInUse", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}

	if err := RemoveVault(dir, vid); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("data dir after remove: %v", left)
	}
	if err := RemoveVault(dir, vid); !errors.Is(err, ErrUnknownVault) {
		t.Fatalf("second remove: %v, want ErrUnknownVault", err)
	}

	// Unlike a drop, nothing comes back: re-registering starts empty.
	srv2, err := NewWithOptions(Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv2.EnsureVault(vid, vault.DeriveAuthKey(rk)); err != nil {
		t.Fatal(err)
	}
	if got, _ := srv2.Entries(vid); len(got) != 0 {
		t.Fatalf("removed vault came back with %d entries", len(got))
	}
}
