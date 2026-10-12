package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"repani.com/kv/vault"
	"repani.com/kv/wire"
)

func nowMs() uint64 { return uint64(time.Now().UnixMilli()) }

var testRegKey = vault.RegisterKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

// harness drives a Server in-process the way a client would: it seals
// frames and opens responses.
type harness struct {
	t   *testing.T
	srv *Server
	rk  vault.RootKey
	vid vault.VaultID
	ak  vault.AuthKey
	c2s vault.ChannelKey
	s2c vault.ChannelKey
	reg vault.ChannelKey
}

func newHarness(t *testing.T, srv *Server, rk vault.RootKey, regKey vault.RegisterKey) *harness {
	ak := vault.DeriveAuthKey(rk)
	return &harness{
		t:   t,
		srv: srv,
		rk:  rk,
		vid: vault.DeriveVaultID(rk),
		ak:  ak,
		c2s: vault.DeriveC2S(ak),
		s2c: vault.DeriveS2C(ak),
		reg: vault.DeriveRegKey(regKey),
	}
}

func (h *harness) send(kind byte, sealK vault.ChannelKey, payload []byte) (wire.Resp, byte) {
	h.t.Helper()
	nonce, err := vault.FrameNonce(h.vid)
	if err != nil {
		h.t.Fatal(err)
	}
	frame, err := wire.SealFrame(kind, h.vid, nonce, sealK, payload)
	if err != nil {
		h.t.Fatal(err)
	}
	respBytes, err := h.srv.HandleFrame(frame)
	if err != nil {
		h.t.Fatal(err)
	}
	if len(respBytes) == 1 {
		return wire.Resp{}, respBytes[0]
	}
	rkind, rvid, rnonce, sealed, err := wire.ParseHeader(respBytes)
	if err != nil {
		h.t.Fatalf("parse response: %v", err)
	}
	if rkind != wire.KindResp {
		h.t.Fatalf("response kind %d", rkind)
	}
	payloadOut, err := wire.OpenFrame(h.s2c, wire.KindResp, rvid, rnonce, sealed)
	if err != nil {
		h.t.Fatalf("open response: %v", err)
	}
	resp, err := wire.ParseResp(payloadOut)
	if err != nil {
		h.t.Fatalf("parse resp payload: %v", err)
	}
	if resp.ReqNonce != nonce {
		h.t.Fatal("response nonce mismatch")
	}
	return resp, 0
}

func (h *harness) register() (wire.Resp, byte) {
	return h.send(wire.KindRegister, h.reg, h.ak[:])
}

func (h *harness) probe(setHash [16]byte) (wire.Resp, byte) {
	return h.send(wire.KindSync, h.c2s, wire.SyncReq{Op: wire.OpProbe, SetHash: setHash}.Marshal())
}

func (h *harness) pull(ts uint64, id vault.EntryID) (wire.Resp, byte) {
	return h.send(wire.KindSync, h.c2s, wire.SyncReq{Op: wire.OpPull, CurTs: ts, CurID: id}.Marshal())
}

func (h *harness) push(entries []vault.Entry) (wire.Resp, byte) {
	return h.send(wire.KindSync, h.c2s, wire.SyncReq{Op: wire.OpPush, Entries: entries}.Marshal())
}

func mustNew(t *testing.T, dataDir string) *Server {
	t.Helper()
	srv, err := NewWithOptions(Options{DataDir: dataDir, RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func mkEntry(t *testing.T, rk vault.RootKey, ts uint64, key, value string) vault.Entry {
	t.Helper()
	aead, _ := rk.AEAD()
	e, err := vault.Seal(aead, ts, []byte(key), []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestRegister_GateAcceptsAndRejects(t *testing.T) {
	srv := mustNew(t, "")
	rk, _ := vault.NewRandomRootKey()

	// Correct register key → OK.
	h := newHarness(t, srv, rk, testRegKey)
	if resp, pre := h.register(); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("register: pre=0x%02x status=%d", pre, resp.Status)
	}

	// Wrong register key → BAD_AUTH pre-channel byte (seal will not open).
	rk2, _ := vault.NewRandomRootKey()
	var wrong vault.RegisterKey
	for i := range wrong {
		wrong[i] = 0xFF
	}
	h2 := newHarness(t, srv, rk2, wrong)
	if _, pre := h2.register(); pre != wire.ErrBadAuth {
		t.Fatalf("wrong register key: pre=0x%02x, want BAD_AUTH", pre)
	}
}

func TestRegister_KeyFileHoldsSeveralKeys(t *testing.T) {
	second := vault.RegisterKey{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	path := filepath.Join(t.TempDir(), "register.key")
	file := "# operator\n" + vault.EncodeRegisterKey(testRegKey) + "\n\n  # iou\n" +
		vault.EncodeRegisterKey(second) + "\n"
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, err := NewWithOptions(Options{RegisterKeyFile: path})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []vault.RegisterKey{testRegKey, second} {
		rk, _ := vault.NewRandomRootKey()
		if resp, pre := newHarness(t, srv, rk, k).register(); pre != 0 || resp.Status != wire.StatusOK {
			t.Fatalf("register under %v: pre=0x%02x status=%d", k, pre, resp.Status)
		}
	}
	rk, _ := vault.NewRandomRootKey()
	if _, pre := newHarness(t, srv, rk, vault.RegisterKey{0xFF}).register(); pre != wire.ErrBadAuth {
		t.Fatalf("unlisted register key: pre=0x%02x, want BAD_AUTH", pre)
	}
}

func TestRegister_KeyFileErrors(t *testing.T) {
	for name, file := range map[string]string{
		"empty":        "",
		"only comment": "# iou\n\n",
		"bad line":     vault.EncodeRegisterKey(testRegKey) + "\nnot-a-key\n",
	} {
		path := filepath.Join(t.TempDir(), "register.key")
		if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewWithOptions(Options{RegisterKeyFile: path}); err == nil {
			t.Errorf("%s: NewWithOptions accepted the file", name)
		}
	}
}

func TestSync_UnknownVaultBeforeRegister(t *testing.T) {
	srv := mustNew(t, "")
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	if _, pre := h.probe([16]byte{}); pre != wire.ErrUnknownVault {
		t.Fatalf("probe before register: pre=0x%02x, want UNKNOWN_VAULT", pre)
	}
}

func TestSync_BadAuthAfterRegister(t *testing.T) {
	srv := mustNew(t, "")
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	h.register()
	// Tamper the c2s key so the SYNC seal will not open.
	h.c2s[0] ^= 1
	if _, pre := h.probe([16]byte{}); pre != wire.ErrBadAuth {
		t.Fatalf("probe with wrong c2s: pre=0x%02x, want BAD_AUTH", pre)
	}
}

func TestBadVersionAndKind(t *testing.T) {
	srv := mustNew(t, "")
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	nonce, _ := vault.FrameNonce(h.vid)
	frame, _ := wire.SealFrame(wire.KindResp, h.vid, nonce, h.s2c, []byte("x")) // RESP from a client is invalid
	resp, _ := srv.HandleFrame(frame)
	if len(resp) != 1 || resp[0] != wire.ErrBadKind {
		t.Fatalf("client-sent RESP: got %v, want BAD_KIND", resp)
	}
	frame[0] = 0x02 // bad version
	resp, _ = srv.HandleFrame(frame)
	if len(resp) != 1 || resp[0] != wire.ErrBadVersion {
		t.Fatalf("bad version: got %v, want BAD_VERSION", resp)
	}
}

func TestProbePushPull(t *testing.T) {
	srv := mustNew(t, "")
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	h.register()

	// Empty vault: our set-hash (empty) matches → in sync.
	if resp, _ := h.probe(vault.SetHash(nil)); resp.Status != wire.StatusOK {
		t.Fatalf("empty probe status %d, want OK", resp.Status)
	}

	// Push two entries.
	e1 := mkEntry(t, rk, nowMs(), "k1", "v1")
	e2 := mkEntry(t, rk, nowMs()+1, "k2", "v2")
	if resp, _ := h.push([]vault.Entry{e1, e2}); resp.Status != wire.StatusOK {
		t.Fatalf("push status %d", resp.Status)
	}

	// Probe with the empty hash now differs.
	if resp, _ := h.probe(vault.SetHash(nil)); resp.Status != wire.StatusDiffer {
		t.Fatalf("post-push probe status %d, want DIFFER", resp.Status)
	}

	// Pull from the beginning returns both, in canonical order.
	resp, _ := h.pull(0, vault.EntryID{})
	if len(resp.Entries) != 2 {
		t.Fatalf("pull returned %d entries, want 2", len(resp.Entries))
	}

	// Pull after the last cursor returns nothing.
	last := resp.Entries[len(resp.Entries)-1]
	resp2, _ := h.pull(last.TsMs, last.ID())
	if len(resp2.Entries) != 0 {
		t.Fatalf("pull past end returned %d entries", len(resp2.Entries))
	}
}

func TestQuota_SkewAndMaxEntries(t *testing.T) {
	const now = int64(1_700_000_000_000)
	q := DefaultQuotas()
	q.TSSkewMs = 60_000
	q.MaxEntries = 2
	srv, err := NewWithOptions(Options{Quotas: q, Clock: func() int64 { return now }, RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	h.register()

	// Out-of-skew push rejected.
	old := mkEntry(t, rk, uint64(now-120_000), "old", "v")
	if resp, _ := h.push([]vault.Entry{old}); resp.Status != wire.StatusBadRequest {
		t.Fatalf("out-of-skew push status %d, want BAD_REQUEST", resp.Status)
	}

	// Fill to the entry cap.
	a := mkEntry(t, rk, uint64(now+1), "a", "1")
	b := mkEntry(t, rk, uint64(now+2), "b", "2")
	if resp, _ := h.push([]vault.Entry{a, b}); resp.Status != wire.StatusOK {
		t.Fatalf("fill push status %d", resp.Status)
	}
	// One more new entry exceeds MaxEntries.
	c := mkEntry(t, rk, uint64(now+3), "c", "3")
	if resp, _ := h.push([]vault.Entry{c}); resp.Status != wire.StatusQuota {
		t.Fatalf("over-cap push status %d, want QUOTA", resp.Status)
	}
}

// TestQuota_ZeroFieldTakesDefault: a Quotas that sets one limit keeps
// the defaults of the others, so an ordinary push is accepted.
func TestQuota_ZeroFieldTakesDefault(t *testing.T) {
	srv, err := NewWithOptions(Options{Quotas: Quotas{MaxVaults: 5}, RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	h.register()
	if resp, _ := h.push([]vault.Entry{mkEntry(t, rk, nowMs(), "k", "v")}); resp.Status != wire.StatusOK {
		t.Fatalf("push status %s, want ok", wire.StatusText(resp.Status))
	}
	want := DefaultQuotas()
	want.MaxVaults = 5
	if srv.quotas != want {
		t.Errorf("quotas %+v, want %+v", srv.quotas, want)
	}
}

func TestPersistence_ReloadsVaultAndEntries(t *testing.T) {
	dir := t.TempDir()
	rk, _ := vault.NewRandomRootKey()
	e := mkEntry(t, rk, nowMs(), "persistent", "\x00yes")

	srv1 := mustNew(t, dir)
	h1 := newHarness(t, srv1, rk, testRegKey)
	h1.register()
	h1.push([]vault.Entry{e})

	// A file exists under the VaultID hex directory.
	logs, _ := filepath.Glob(filepath.Join(dir, "*", "entries.log"))
	if len(logs) != 1 {
		t.Fatalf("expected 1 persistent log, got %v", logs)
	}

	// Release srv1's file handles/locks, as a real restart would.
	if err := srv1.Close(); err != nil {
		t.Fatalf("close srv1: %v", err)
	}

	// A second server on the same dir replays AuthKey + entries and
	// serves a non-registering pull.
	srv2 := mustNew(t, dir)
	h2 := newHarness(t, srv2, rk, testRegKey)
	resp, pre := h2.pull(0, vault.EntryID{})
	if pre != 0 {
		t.Fatalf("pull after reload pre=0x%02x", pre)
	}
	if len(resp.Entries) != 1 || resp.Entries[0].ID() != e.ID() {
		t.Fatalf("reload returned %d entries", len(resp.Entries))
	}
}

func TestRegister_IdempotentAndRebindRejected(t *testing.T) {
	srv := mustNew(t, "")
	rk, _ := vault.NewRandomRootKey()
	h := newHarness(t, srv, rk, testRegKey)
	h.register()
	// Same key again — idempotent OK.
	if resp, pre := h.register(); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("idempotent re-register: pre=0x%02x status=%d", pre, resp.Status)
	}
	// A different device (different AuthKey) but same VaultID and valid
	// register key: rebind attempt must be rejected.
	other, _ := vault.NewRandomRootKey()
	h.ak = vault.DeriveAuthKey(other) // present a mismatching authkey
	if _, pre := h.register(); pre != wire.ErrBadAuth {
		t.Fatalf("rebind: pre=0x%02x, want BAD_AUTH", pre)
	}
}
