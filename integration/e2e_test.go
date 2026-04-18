// Package integration exercises the full ASCON-KV stack end to end
// against a real HTTP server and real HTTP client transports.
package integration_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pavlos/kv/client"
	"github.com/pavlos/kv/server"
	"github.com/pavlos/kv/store"
	"github.com/pavlos/kv/vault"
	"github.com/pavlos/kv/wire"
)

type httpTran struct{ url string }

func (t *httpTran) Do(ctx context.Context, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url+"/v1/sync", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// testRegKey is a fixed register-gate secret shared by every server
// and every client in these e2e tests.
var testRegKey = vault.RegisterKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	srv, err := server.NewWithOptions(server.Options{RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// newStack spins up a real HTTP server and returns the shared RootKey
// plus two clients built on top of it.
func newStack(t *testing.T) (rk vault.RootKey, a, b *client.Client, ts *httptest.Server) {
	t.Helper()
	srv := newTestServer(t)
	ts = httptest.NewServer(srv)
	rk, _ = vault.NewRandomRootKey()
	tran := &httpTran{url: ts.URL}
	var err error
	a, err = client.New(rk, store.NewMemStore(), tran)
	if err != nil {
		t.Fatal(err)
	}
	a.SetRegisterKey(&testRegKey)
	b, err = client.New(rk, store.NewMemStore(), tran)
	if err != nil {
		t.Fatal(err)
	}
	b.SetRegisterKey(&testRegKey)
	return rk, a, b, ts
}

// --- 1. steady-state: polling is cheap ---

func TestE2E_SteadyStatePayloads(t *testing.T) {
	srv := newTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	rk, _ := vault.NewRandomRootKey()

	// Build a vault with N entries.
	const N = 1000
	c, _ := client.New(rk, store.NewMemStore(), &httpTran{url: ts.URL})
	c.SetRegisterKey(&testRegKey)
	for i := range N {
		_ = c.Put([]byte{byte(i), byte(i >> 8)}, []byte{0xab})
	}
	if err := c.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Measure steady-state payloads.
	sorted := c.Store().Sorted()
	req := wire.Request{
		VaultID:  vault.DeriveVaultID(rk),
		N:        uint32(len(sorted)),
		Ladder:   vault.ComputeLadder(sorted),
		Terminal: vault.TerminalHash(sorted),
	}
	ak := vault.DeriveAuthKey(rk)
	body, err := wire.SignRequest(ak, &req)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("steady-state request body: %d bytes (N=%d)", len(body), N)
	// Budget: request = ver(1)+vid(16)+flags(1)+N(4)+ladder(10*16=160)+terminal(16)+pc(2)+mac(16) = 216.
	if len(body) > 400 {
		t.Fatalf("steady-state request larger than expected: %d bytes for N=%d", len(body), N)
	}

	resp, err := srv.HandleSync(body)
	if err != nil {
		t.Fatal(err)
	}
	// Steady-state response: ver(1)+status(1)+window(4)+pc(2)+mac(16) = 24.
	if len(resp) != 24 {
		t.Fatalf("steady-state response %d bytes, want 24", len(resp))
	}
}

// --- 2. concurrent writes from two clients, LWW resolution ---

func TestE2E_ConcurrentWrites_LWW(t *testing.T) {
	ctx := context.Background()
	_, a, b, ts := newStack(t)
	defer ts.Close()

	if err := a.Put([]byte("k"), []byte("from-a")); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	if err := b.Put([]byte("k"), []byte("from-b")); err != nil {
		t.Fatal(err)
	}
	if err := b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]*client.Client{"a": a, "b": b} {
		v, ok, err := c.Get([]byte("k"))
		if err != nil || !ok {
			t.Fatalf("%s Get: ok=%v err=%v", name, ok, err)
		}
		if !bytes.Equal(v, []byte("from-b")) {
			t.Fatalf("%s: expected latest write 'from-b', got %q", name, v)
		}
	}
}

// --- 3. middle-of-history insert (entry with small ts_ms) ---

func TestE2E_BackdatedInsert_Propagates(t *testing.T) {
	ctx := context.Background()
	rk, a, b, ts := newStack(t)
	defer ts.Close()

	for i := range 5 {
		_ = a.Put([]byte{byte('a' + i)}, []byte{byte(i)})
	}
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	// Craft a backdated entry and force it into b's local store.
	// Keep ts within the server's 24h skew window — "backdated" here
	// means earlier than the ladder floor, not pre-1970.
	aead, _ := rk.AEAD()
	backdatedTs := uint64(time.Now().UnixMilli() - 60_000)
	old, err := vault.Seal(aead, backdatedTs, []byte("historical"), []byte("once"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Store().Add(old); err != nil {
		t.Fatal(err)
	}

	// b doesn't automatically know it needs to push this (it wasn't
	// routed through Put). Do a couple of syncs to let the ladder
	// surface the divergence.
	for range 10 {
		if err := b.Sync(ctx); err != nil {
			t.Fatal(err)
		}
		if err := a.Sync(ctx); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := a.Get([]byte("historical")); ok {
			return // success
		}
	}
	t.Fatal("A never received the backdated entry after multiple sync rounds")
}

// --- 4. tamper resistance: flipped ciphertext fails to open ---

func TestE2E_TamperedCiphertext_FailsToOpen(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	aead, _ := rk.AEAD()
	e, _ := vault.Seal(aead, 100, []byte("k"), []byte("v"))
	e.Ct[len(e.Ct)-1] ^= 1
	if _, _, err := vault.Open(aead, e); err == nil {
		t.Fatal("expected Open to fail on tampered ciphertext")
	}
}
