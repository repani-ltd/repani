package server

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"repani.com/kv/vault"
	"repani.com/kv/wire"
)

// fakeClock is a settable server clock, in ms.
type fakeClock struct{ now int64 }

func (c *fakeClock) ms() int64 { return c.now }

func newEphemeralServer(t *testing.T, dir string, clk *fakeClock, q Quotas) *Server {
	t.Helper()
	srv, err := New(Options{DataDir: dir, Quotas: q, Clock: clk.ms, RegisterKeys: []vault.RegisterKey{testRegKey}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv
}

func (h *harness) registerEphemeral() (wire.Resp, wire.PreChannel) {
	return h.send(wire.KindRegister, h.reg, wire.RegisterPayload(h.ak, true))
}

func (h *harness) drop() (wire.Resp, wire.PreChannel) {
	return h.send(wire.KindSync, h.c2s, wire.SyncReq{Op: wire.OpDrop}.Marshal())
}

// dirNames lists a vault directory's files.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

// TestEphemeral_DropEndsAndRevokes: a DROP deletes the vault and
// revokes it, on disk too, so neither a sync, a REGISTER, nor a
// restarted server brings it back.
func TestEphemeral_DropEndsAndRevokes(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{now: 1_700_000_000_000}
	srv := newEphemeralServer(t, dir, clk, Quotas{})
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	if resp, pre := h.registerEphemeral(); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("register ephemeral: pre=%v status=%v", pre, resp.Status)
	}
	if resp, pre := h.push([]vault.Entry{mkEntry(t, rk, uint64(clk.now), "keyring", "\x00secret")}); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("push: pre=%v status=%v", pre, resp.Status)
	}
	if resp, pre := h.drop(); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("drop: pre=%v status=%v", pre, resp.Status)
	}
	if _, pre := h.probe([16]byte{}); pre != wire.ErrUnknownVault {
		t.Fatalf("probe after drop: pre=%v, want UNKNOWN_VAULT", pre)
	}
	if _, pre := h.drop(); pre != wire.ErrUnknownVault {
		t.Fatalf("second drop: pre=%v, want UNKNOWN_VAULT", pre)
	}
	if _, pre := h.registerEphemeral(); pre != wire.ErrBadAuth {
		t.Fatalf("register after drop: pre=%v, want BAD_AUTH", pre)
	}
	if _, pre := h.register(); pre != wire.ErrBadAuth {
		t.Fatalf("plain register after drop: pre=%v, want BAD_AUTH", pre)
	}
	vdir := filepath.Join(dir, h.vid.String())
	if got := dirNames(t, vdir); !slices.Equal(got, []string{"vault.fact"}) {
		t.Fatalf("vault directory after drop holds %v, want only its record", got)
	}
	if r, err := readRecord(vdir); err != nil || r.Ended == nil || r.Note == nil || *r.Note != noteDropped || !r.Ephemeral {
		t.Fatalf("record after drop: %+v, %v; want ephemeral, ended, noted %q", r, err, noteDropped)
	}

	srv.Close()
	srv2 := newEphemeralServer(t, dir, clk, Quotas{})
	h2 := newHarness(t, srv2, rk, testRegKey)
	if _, pre := h2.register(); pre != wire.ErrBadAuth {
		t.Fatalf("register after restart: pre=%v, want BAD_AUTH", pre)
	}
	if n := len(srv2.Vaults()); n != 0 {
		t.Fatalf("restarted server serves %d vaults, want 0", n)
	}
}

// TestEphemeral_OnlyEphemeralDrops: a DROP of an ordinary vault is
// BAD_REQUEST and leaves it whole, and a REGISTER cannot mark an
// existing vault ephemeral.
func TestEphemeral_OnlyEphemeralDrops(t *testing.T) {
	clk := &fakeClock{now: 1_700_000_000_000}
	srv := newEphemeralServer(t, t.TempDir(), clk, Quotas{})
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	h.register()
	h.push([]vault.Entry{mkEntry(t, rk, uint64(clk.now), "k", "\x00v")})
	if resp, pre := h.registerEphemeral(); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("re-register with the flag: pre=%v status=%v, want OK", pre, resp.Status)
	}
	if resp, pre := h.drop(); pre != 0 || resp.Status != wire.StatusBadRequest {
		t.Fatalf("drop of an ordinary vault: pre=%v status=%v, want BAD_REQUEST", pre, resp.Status)
	}
	if resp, pre := h.pull(0, vault.EntryID{}); pre != 0 || len(resp.Entries) != 1 {
		t.Fatalf("after the refused drop: pre=%v, %d entries, want 1", pre, len(resp.Entries))
	}
	// An ephemeral vault stays ephemeral when re-registered plainly.
	rk2, _ := vault.NewRandomRootKey()
	h2 := newHarness(t, srv, rk2, testRegKey)
	h2.registerEphemeral()
	h2.register()
	if resp, pre := h2.drop(); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("drop after a plain re-register: pre=%v status=%v, want OK", pre, resp.Status)
	}
}

// TestEphemeral_Expiry: an ephemeral vault ends EphemeralTTLMs after
// its creation, used or not, and an ordinary one never does.
func TestEphemeral_Expiry(t *testing.T) {
	clk := &fakeClock{now: 1_700_000_000_000}
	srv := newEphemeralServer(t, t.TempDir(), clk, Quotas{EphemeralTTLMs: 60_000})
	eph, _ := vault.NewRandomRootKey()
	ord, _ := vault.NewRandomRootKey()
	he := newHarness(t, srv, eph, testRegKey)
	ho := newHarness(t, srv, ord, testRegKey)
	he.registerEphemeral()
	ho.register()

	clk.now += 59_999
	if _, pre := he.probe([16]byte{}); pre != 0 {
		t.Fatalf("probe within the lifetime: pre=%v", pre)
	}
	clk.now++
	if _, pre := he.probe([16]byte{}); pre != wire.ErrUnknownVault {
		t.Fatalf("probe at the lifetime: pre=%v, want UNKNOWN_VAULT", pre)
	}
	if _, pre := he.registerEphemeral(); pre != wire.ErrBadAuth {
		t.Fatalf("register after expiry: pre=%v, want BAD_AUTH", pre)
	}
	clk.now += 365 * 24 * 60 * 60 * 1000
	if _, pre := ho.probe([16]byte{}); pre != 0 {
		t.Fatalf("an ordinary vault a year on: pre=%v", pre)
	}
}

// TestEphemeral_ExpiredGiveBackTheCap: expired ephemeral vaults end
// before a new vault is counted against MaxVaults.
func TestEphemeral_ExpiredGiveBackTheCap(t *testing.T) {
	clk := &fakeClock{now: 1_700_000_000_000}
	srv := newEphemeralServer(t, t.TempDir(), clk, Quotas{MaxVaults: 1, EphemeralTTLMs: 60_000})
	eph, _ := vault.NewRandomRootKey()
	next, _ := vault.NewRandomRootKey()
	newHarness(t, srv, eph, testRegKey).registerEphemeral()
	hn := newHarness(t, srv, next, testRegKey)
	if resp, _ := hn.register(); resp.Status != wire.StatusQuota {
		t.Fatalf("register at the cap: status %v, want QUOTA", resp.Status)
	}
	clk.now += 60_000
	if resp, pre := hn.register(); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("register once the ephemeral vault expired: pre=%v status=%v, want OK", pre, resp.Status)
	}
}

// TestEphemeral_SurvivesRestart: the mark and the creation time are
// on disk, so a restarted server still drops the vault, and ends it at
// start once its lifetime has passed.
func TestEphemeral_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{now: 1_700_000_000_000}
	q := Quotas{EphemeralTTLMs: 60_000}
	srv := newEphemeralServer(t, dir, clk, q)
	a, _ := vault.NewRandomRootKey()
	b, _ := vault.NewRandomRootKey()
	newHarness(t, srv, a, testRegKey).registerEphemeral()
	clk.now += 30_000
	newHarness(t, srv, b, testRegKey).registerEphemeral()
	srv.Close()

	clk.now += 30_000 // a has expired, b has not
	srv2 := newEphemeralServer(t, dir, clk, q)
	if got := srv2.Vaults(); len(got) != 1 || got[0] != vault.DeriveVaultID(b) {
		t.Fatalf("after restart serving %v, want only b", got)
	}
	if _, pre := newHarness(t, srv2, a, testRegKey).register(); pre != wire.ErrBadAuth {
		t.Fatalf("register a after restart: pre=%v, want BAD_AUTH", pre)
	}
	if resp, pre := newHarness(t, srv2, b, testRegKey).drop(); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("drop b after restart: pre=%v status=%v, want OK", pre, resp.Status)
	}
}

// TestEphemeral_EndedRecordWinsAtStart: a crash after the record was
// rewritten as ended but before the entry log was deleted leaves a
// vault the next start treats as ended, and clears.
func TestEphemeral_EndedRecordWinsAtStart(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{now: 1_700_000_000_000}
	srv := newEphemeralServer(t, dir, clk, Quotas{})
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	h.registerEphemeral()
	srv.Close()
	vdir := filepath.Join(dir, h.vid.String())
	r, err := readRecord(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRecord(vdir, r.ended(clk.now, noteDropped)); err != nil {
		t.Fatal(err)
	}

	srv2 := newEphemeralServer(t, dir, clk, Quotas{})
	if n := len(srv2.Vaults()); n != 0 {
		t.Fatalf("serving %d vaults, want 0", n)
	}
	if _, pre := newHarness(t, srv2, rk, testRegKey).register(); pre != wire.ErrBadAuth {
		t.Fatalf("register: pre=%v, want BAD_AUTH", pre)
	}
	if got := dirNames(t, vdir); !slices.Equal(got, []string{"vault.fact"}) {
		t.Fatalf("vault directory holds %v, want only its record", got)
	}
}

// TestEphemeral_InMemory: a server without a data directory drops and
// revokes too.
func TestEphemeral_InMemory(t *testing.T) {
	clk := &fakeClock{now: 1_700_000_000_000}
	srv := newEphemeralServer(t, "", clk, Quotas{})
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	h.registerEphemeral()
	if resp, pre := h.drop(); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("drop: pre=%v status=%v", pre, resp.Status)
	}
	if _, pre := h.register(); pre != wire.ErrBadAuth {
		t.Fatalf("register after drop: pre=%v, want BAD_AUTH", pre)
	}
}

// TestRegister_Flags: an unknown flag or a wrong length is BAD_FRAME,
// and creates nothing.
func TestRegister_Flags(t *testing.T) {
	srv := mustNew(t, t.TempDir())
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	for _, payload := range [][]byte{
		append(h.ak[:], 0x02),
		append(h.ak[:], 0x01, 0x00),
		h.ak[:15],
	} {
		if _, pre := h.send(wire.KindRegister, h.reg, payload); pre != wire.ErrBadFrame {
			t.Fatalf("register payload of %d bytes: pre=%v, want BAD_FRAME", len(payload), pre)
		}
	}
	if n := len(srv.Vaults()); n != 0 {
		t.Fatalf("serving %d vaults, want 0", n)
	}
}
