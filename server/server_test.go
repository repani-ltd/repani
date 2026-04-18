package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pavlos/kv/vault"
	"github.com/pavlos/kv/wire"
)

// nowMs returns the current unix time in milliseconds. Server quotas
// reject ts_ms outside ±24h of the server clock, so tests must use
// timestamps anchored to real time.
func nowMs() uint64 { return uint64(time.Now().UnixMilli()) }

// testRegKey is the fixed register-gate secret used by the non-gated
// tests. Test helpers feed it into every server constructed via
// mustNew and every REGISTER request built via signedRequest.
var testRegKey = vault.RegisterKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

// mustNew is the common test-side server constructor. It uses
// testRegKey so any REGISTER built with signedRequest / signedRegisterWithGate
// on the same key will be accepted.
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
	aead, err := rk.AEAD()
	if err != nil {
		t.Fatal(err)
	}
	e, err := vault.Seal(aead, ts, []byte(key), []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// signedRequest builds a request body signed under rk's AuthKey,
// marking REGISTER only when the client has not yet synced with the
// server. When register=true the request also carries a valid
// reg_admin_mac computed under testRegKey, matching the servers built
// by mustNew.
func signedRequest(t *testing.T, rk vault.RootKey, req wire.Request, register bool) []byte {
	t.Helper()
	ak := vault.DeriveAuthKey(rk)
	if register {
		vid := vault.DeriveVaultID(rk)
		req.Flags |= wire.FlagRegister
		req.RegAuth = ak
		req.VaultID = vid
		req.RegAdminMAC = vault.RegisterAdminMAC(testRegKey, ak, vid)
	}
	b, err := wire.SignRequest(ak, &req)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// signedRegisterWithGate builds a REGISTER request with a valid
// reg_admin_mac under the given register key.
func signedRegisterWithGate(t *testing.T, rk vault.RootKey, req wire.Request, regKey vault.RegisterKey) []byte {
	t.Helper()
	ak := vault.DeriveAuthKey(rk)
	vid := vault.DeriveVaultID(rk)
	req.Flags |= wire.FlagRegister
	req.RegAuth = ak
	req.VaultID = vid
	req.RegAdminMAC = vault.RegisterAdminMAC(regKey, ak, vid)
	b, err := wire.SignRequest(ak, &req)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func postSync(t *testing.T, url string, body []byte) []byte {
	t.Helper()
	resp, err := http.Post(url+"/v1/sync", "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, msg)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHTTP_RoundTrip_EmptyVault(t *testing.T) {
	srv := mustNew(t, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	rk, _ := vault.NewRandomRootKey()
	req := wire.Request{VaultID: vault.DeriveVaultID(rk), N: 0}
	body := signedRequest(t, rk, req, true)

	rb := postSync(t, ts.URL, body)
	resp, err := wire.UnmarshalResponse(rb)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != wire.StatusOK {
		t.Fatalf("status %s, want OK", wire.StatusText(resp.Status))
	}
	if resp.Window != 0 || len(resp.Entries) != 0 {
		t.Fatalf("expected empty response, got window=%d entries=%d", resp.Window, len(resp.Entries))
	}
}

func TestHTTP_UnknownVaultWithoutRegister(t *testing.T) {
	srv := mustNew(t, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	rk, _ := vault.NewRandomRootKey()
	req := wire.Request{VaultID: vault.DeriveVaultID(rk), N: 0}
	body := signedRequest(t, rk, req, false)
	rb := postSync(t, ts.URL, body)
	resp, err := wire.UnmarshalResponse(rb)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != wire.StatusUnknownVault {
		t.Fatalf("status %s, want UNKNOWN_VAULT", wire.StatusText(resp.Status))
	}
}

func TestHTTP_BadMACRejected(t *testing.T) {
	srv := mustNew(t, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	rk, _ := vault.NewRandomRootKey()
	// Register first.
	_ = postSync(t, ts.URL, signedRequest(t, rk, wire.Request{VaultID: vault.DeriveVaultID(rk)}, true))

	// Second request signed under the wrong key.
	wrongRK, _ := vault.NewRandomRootKey()
	req := wire.Request{VaultID: vault.DeriveVaultID(rk), N: 0} // real VaultID
	ak := vault.DeriveAuthKey(wrongRK)
	body, _ := wire.SignRequest(ak, &req)
	rb := postSync(t, ts.URL, body)
	resp, err := wire.UnmarshalResponse(rb)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != wire.StatusBadMAC {
		t.Fatalf("status %s, want BAD_MAC", wire.StatusText(resp.Status))
	}
}

func TestHTTP_RegisterIdempotent(t *testing.T) {
	srv := mustNew(t, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)
	_ = postSync(t, ts.URL, signedRequest(t, rk, wire.Request{VaultID: vid}, true))
	// Re-register with matching reg_authkey — must succeed.
	rb := postSync(t, ts.URL, signedRequest(t, rk, wire.Request{VaultID: vid}, true))
	resp, _ := wire.UnmarshalResponse(rb)
	if resp.Status != wire.StatusOK {
		t.Fatalf("idempotent re-register status %s", wire.StatusText(resp.Status))
	}
}

// --- register-key gate ---

// gatedServer returns a server configured with a register key
// DIFFERENT from the default testRegKey, so that signedRequest (which
// uses testRegKey) fails the admin MAC check — that's how the gate-
// rejection tests assert "without MAC" / "wrong MAC".
func gatedServer(t *testing.T) (*Server, vault.RegisterKey, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "register.key")
	var rk vault.RegisterKey
	for i := range rk {
		rk[i] = byte(0xA0 | i) // distinct from testRegKey
	}
	if err := os.WriteFile(keyPath, []byte(vault.EncodeRegisterKey(rk)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, err := NewWithOptions(Options{DataDir: "", RegisterKeyFile: keyPath})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	return srv, rk, ts
}

func TestHTTP_RegisterGate_Accepts(t *testing.T) {
	_, regKey, ts := gatedServer(t)
	defer ts.Close()

	rk, _ := vault.NewRandomRootKey()
	body := signedRegisterWithGate(t, rk, wire.Request{}, regKey)
	resp, err := wire.UnmarshalResponse(postSync(t, ts.URL, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != wire.StatusOK {
		t.Fatalf("status %s, want OK", wire.StatusText(resp.Status))
	}
}

func TestHTTP_RegisterGate_RejectsWithoutMAC(t *testing.T) {
	_, _, ts := gatedServer(t)
	defer ts.Close()

	rk, _ := vault.NewRandomRootKey()
	// Build a REGISTER without a valid admin MAC (RegAdminMAC zeros).
	body := signedRequest(t, rk, wire.Request{VaultID: vault.DeriveVaultID(rk)}, true)
	resp, err := wire.UnmarshalResponse(postSync(t, ts.URL, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != wire.StatusBadMAC {
		t.Fatalf("status %s, want BAD_MAC (gate should reject missing admin MAC)", wire.StatusText(resp.Status))
	}
}

func TestHTTP_RegisterGate_RejectsWithWrongMAC(t *testing.T) {
	_, _, ts := gatedServer(t)
	defer ts.Close()

	var wrongKey vault.RegisterKey
	for i := range wrongKey {
		wrongKey[i] = 0xFF
	}
	rk, _ := vault.NewRandomRootKey()
	body := signedRegisterWithGate(t, rk, wire.Request{}, wrongKey)
	resp, err := wire.UnmarshalResponse(postSync(t, ts.URL, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != wire.StatusBadMAC {
		t.Fatalf("status %s, want BAD_MAC (wrong register key)", wire.StatusText(resp.Status))
	}
}

// Once a vault is registered through the gate, subsequent non-
// REGISTER requests don't need the admin MAC (the gate is only for
// the first sync).
func TestHTTP_RegisterGate_SubsequentSyncNoGateNeeded(t *testing.T) {
	_, regKey, ts := gatedServer(t)
	defer ts.Close()

	rk, _ := vault.NewRandomRootKey()
	_ = postSync(t, ts.URL, signedRegisterWithGate(t, rk, wire.Request{}, regKey))

	// Follow-up pull: no REGISTER flag, no admin MAC — must succeed.
	body := signedRequest(t, rk, wire.Request{VaultID: vault.DeriveVaultID(rk)}, false)
	resp, err := wire.UnmarshalResponse(postSync(t, ts.URL, body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != wire.StatusOK {
		t.Fatalf("status %s, want OK on post-register sync", wire.StatusText(resp.Status))
	}
}

func TestHTTP_RegisterMismatchRejected(t *testing.T) {
	srv := mustNew(t, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)
	_ = postSync(t, ts.URL, signedRequest(t, rk, wire.Request{VaultID: vid}, true))

	// Attempt to re-register the same VaultID under a different AuthKey.
	attacker, _ := vault.NewRandomRootKey()
	attackerAK := vault.DeriveAuthKey(attacker)
	req := wire.Request{
		VaultID: vid,
		Flags:   wire.FlagRegister,
		RegAuth: attackerAK,
	}
	body, _ := wire.SignRequest(attackerAK, &req)
	rb := postSync(t, ts.URL, body)
	resp, _ := wire.UnmarshalResponse(rb)
	if resp.Status != wire.StatusBadMAC {
		t.Fatalf("hostile re-register status %s, want BAD_MAC", wire.StatusText(resp.Status))
	}
}

func TestHTTP_PushThenPull(t *testing.T) {
	srv := mustNew(t, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)
	e := mkEntry(t, rk, nowMs(), "k", "v")

	// First sync registers and pushes.
	pushReq := wire.Request{VaultID: vid, N: 0, Pushes: []vault.Entry{e}}
	_ = postSync(t, ts.URL, signedRequest(t, rk, pushReq, true))

	// Second sync is a pull with N=0 (no register — vault is known).
	pullReq := wire.Request{VaultID: vid, N: 0}
	rb := postSync(t, ts.URL, signedRequest(t, rk, pullReq, false))
	resp, err := wire.UnmarshalResponse(rb)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != wire.StatusOK {
		t.Fatalf("status %s", wire.StatusText(resp.Status))
	}
	if resp.Window == 0 || len(resp.Entries) != 1 {
		t.Fatalf("expected window>0 with 1 entry, got window=%d entries=%d", resp.Window, len(resp.Entries))
	}
	if resp.Entries[0].ID() != e.ID() {
		t.Fatal("wrong entry returned")
	}
}

func TestHTTP_Persistence(t *testing.T) {
	dir := t.TempDir()
	srv1 := mustNew(t, dir)
	ts1 := httptest.NewServer(srv1)

	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)
	e := mkEntry(t, rk, nowMs(), "persistent", "yes")

	req := wire.Request{VaultID: vid, N: 0, Pushes: []vault.Entry{e}}
	_ = postSync(t, ts1.URL, signedRequest(t, rk, req, true))
	ts1.Close()

	// Verify a file was written under the VaultID hex directory.
	entries, err := filepath.Glob(filepath.Join(dir, "*", "entries.log"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 persistent log, got %v (err=%v)", entries, err)
	}

	// Open a second server on the same dir — should replay AuthKey
	// plus entries and accept a NON-registering sync.
	srv2 := mustNew(t, dir)
	ts2 := httptest.NewServer(srv2)
	defer ts2.Close()

	pullReq := wire.Request{VaultID: vid, N: 0}
	rb := postSync(t, ts2.URL, signedRequest(t, rk, pullReq, false))
	resp, err := wire.UnmarshalResponse(rb)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != wire.StatusOK {
		t.Fatalf("status %s", wire.StatusText(resp.Status))
	}
	if len(resp.Entries) != 1 || resp.Entries[0].ID() != e.ID() {
		t.Fatalf("persistence round-trip failed: got %d entries", len(resp.Entries))
	}
}

// --- quota / clock-skew enforcement ---

// quotaServer builds an in-memory Server with a fixed clock and the
// caller-supplied Quotas. The returned handler is the raw HandleSync
// so tests can assert response.Status without fighting net/http.
func quotaServer(t *testing.T, q Quotas, nowMS int64) *Server {
	t.Helper()
	s, err := NewWithOptions(Options{
		Quotas:      q,
		Clock:       func() int64 { return nowMS },
		RegisterKey: &testRegKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestQuota_TSSkewOutsideWindowRejected(t *testing.T) {
	const nowMS = int64(1_700_000_000_000)
	q := DefaultQuotas()
	q.TSSkewMs = 60_000 // 1 minute
	srv := quotaServer(t, q, nowMS)

	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)
	// Register with a valid push (now).
	_, _ = srv.HandleSync(signedRequest(t, rk, wire.Request{
		VaultID: vid,
		Pushes:  []vault.Entry{mkEntry(t, rk, uint64(nowMS), "k", "v")},
	}, true))

	// Now push an entry 2 minutes in the past (outside the 1-minute skew).
	badPush := mkEntry(t, rk, uint64(nowMS-120_000), "old", "v")
	rb, err := srv.HandleSync(signedRequest(t, rk, wire.Request{
		VaultID: vid,
		Pushes:  []vault.Entry{badPush},
	}, false))
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := wire.UnmarshalResponse(rb)
	if resp.Status != wire.StatusBadRequest {
		t.Fatalf("out-of-skew push: status %s, want BAD_REQUEST", wire.StatusText(resp.Status))
	}
}

func TestQuota_MaxEntriesEnforced(t *testing.T) {
	const nowMS = int64(1_700_000_000_000)
	q := DefaultQuotas()
	q.MaxEntries = 3
	srv := quotaServer(t, q, nowMS)

	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)

	// Fill vault up to the limit (3 entries).
	fill := []vault.Entry{
		mkEntry(t, rk, uint64(nowMS+1), "a", "1"),
		mkEntry(t, rk, uint64(nowMS+2), "b", "2"),
		mkEntry(t, rk, uint64(nowMS+3), "c", "3"),
	}
	rb, _ := srv.HandleSync(signedRequest(t, rk, wire.Request{VaultID: vid, Pushes: fill}, true))
	resp, _ := wire.UnmarshalResponse(rb)
	if resp.Status != wire.StatusOK {
		t.Fatalf("filling to limit: status %s", wire.StatusText(resp.Status))
	}

	// One more push must be rejected.
	over := mkEntry(t, rk, uint64(nowMS+4), "d", "4")
	rb, _ = srv.HandleSync(signedRequest(t, rk, wire.Request{VaultID: vid, Pushes: []vault.Entry{over}}, false))
	resp, _ = wire.UnmarshalResponse(rb)
	if resp.Status != wire.StatusQuota {
		t.Fatalf("over-limit push: status %s, want QUOTA", wire.StatusText(resp.Status))
	}
}

func TestQuota_MaxVaultBytesEnforced(t *testing.T) {
	const nowMS = int64(1_700_000_000_000)
	q := DefaultQuotas()
	q.MaxVaultBytes = 200 // very small
	srv := quotaServer(t, q, nowMS)

	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)
	// Craft a single entry that is larger than the quota.
	bigValue := bytes.Repeat([]byte("x"), 200)
	big := mkEntry(t, rk, uint64(nowMS), "k", string(bigValue))

	rb, _ := srv.HandleSync(signedRequest(t, rk, wire.Request{VaultID: vid, Pushes: []vault.Entry{big}}, true))
	resp, _ := wire.UnmarshalResponse(rb)
	if resp.Status != wire.StatusQuota {
		t.Fatalf("oversize push: status %s, want QUOTA", wire.StatusText(resp.Status))
	}
}

func TestQuota_MaxVaultsEnforced(t *testing.T) {
	const nowMS = int64(1_700_000_000_000)
	q := DefaultQuotas()
	q.MaxVaults = 1
	srv := quotaServer(t, q, nowMS)

	// First vault registers fine.
	rk1, _ := vault.NewRandomRootKey()
	rb, _ := srv.HandleSync(signedRequest(t, rk1, wire.Request{VaultID: vault.DeriveVaultID(rk1)}, true))
	resp, _ := wire.UnmarshalResponse(rb)
	if resp.Status != wire.StatusOK {
		t.Fatalf("first vault: status %s", wire.StatusText(resp.Status))
	}

	// Second vault registration hits the cap.
	rk2, _ := vault.NewRandomRootKey()
	rb, _ = srv.HandleSync(signedRequest(t, rk2, wire.Request{VaultID: vault.DeriveVaultID(rk2)}, true))
	resp, _ = wire.UnmarshalResponse(rb)
	if resp.Status != wire.StatusQuota {
		t.Fatalf("second vault: status %s, want QUOTA", wire.StatusText(resp.Status))
	}
}

func TestHTTP_ConcurrentPushes(t *testing.T) {
	srv := mustNew(t, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	rk, _ := vault.NewRandomRootKey()
	vid := vault.DeriveVaultID(rk)

	// Register once up-front (in isolation) so the subsequent
	// concurrent requests don't all race to be the first registrant.
	_ = postSync(t, ts.URL, signedRequest(t, rk, wire.Request{VaultID: vid}, true))

	const N = 50
	var wg sync.WaitGroup
	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := mkEntry(t, rk, nowMs()+uint64(i), "k", string(rune('a'+i%26)))
			req := wire.Request{VaultID: vid, N: 0, Pushes: []vault.Entry{e}}
			_ = postSync(t, ts.URL, signedRequest(t, rk, req, false))
		}(i)
	}
	wg.Wait()

	// Final pull — must see all N distinct entries.
	rb := postSync(t, ts.URL, signedRequest(t, rk, wire.Request{VaultID: vid, N: 0}, false))
	resp, err := wire.UnmarshalResponse(rb)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != wire.StatusOK {
		t.Fatalf("status %s", wire.StatusText(resp.Status))
	}
	if len(resp.Entries) != N {
		t.Fatalf("after %d concurrent pushes, server has %d entries", N, len(resp.Entries))
	}
}
