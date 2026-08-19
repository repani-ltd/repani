// Package integration exercises the full ASCON-KV v1 stack end to end
// against a real HTTP server and real HTTP transports, with an emphasis
// on the sync/merge logic — the most intricate part of the system.
package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"repani.com/kv/client"
	"repani.com/kv/server"
	"repani.com/kv/store"
	"repani.com/kv/vault"
)

var testRegKey = vault.RegisterKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func newServer(t *testing.T) (*server.Server, *httptest.Server) {
	t.Helper()
	srv, err := server.NewWithOptions(server.Options{RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}
	return srv, httptest.NewServer(srv)
}

// device returns a fresh client (own empty store) on the given RootKey
// and server, wired with the register key.
func device(t *testing.T, rk vault.RootKey, url string) *client.Client {
	t.Helper()
	c, err := client.New(rk, store.NewMemStore(), &client.HTTPTransport{Base: url})
	if err != nil {
		t.Fatal(err)
	}
	c.SetRegisterKey(&testRegKey)
	return c
}

func mustSync(t *testing.T, c *client.Client) {
	t.Helper()
	if err := c.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

// liveState returns a client's merged live view as a plain map.
func liveState(t *testing.T, c *client.Client) map[string]string {
	t.Helper()
	all, err := c.All()
	if err != nil {
		t.Fatal(err)
	}
	m := make(map[string]string, len(all))
	for _, e := range all {
		m[string(e.Key)] = string(e.Value)
	}
	return m
}

func assertState(t *testing.T, c *client.Client, want map[string]string) {
	t.Helper()
	got := liveState(t, c)
	if len(got) != len(want) {
		t.Fatalf("state size %d, want %d\n got=%v\nwant=%v", len(got), len(want), got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("key %q = %q, want %q", k, got[k], v)
		}
	}
}

// --- 1. the core bidirectional merge: server AND client both have new
// entries to exchange in a single sync ---

func TestE2E_BidirectionalMerge(t *testing.T) {
	_, ts := newServer(t)
	defer ts.Close()
	rk, _ := vault.NewRandomRootKey()

	a := device(t, rk, ts.URL) // the device under test
	b := device(t, rk, ts.URL) // a second device on the same vault

	// B writes two keys and pushes them to the server.
	if err := b.Put([]byte("from_b_1"), []byte("b-one")); err != nil {
		t.Fatal(err)
	}
	if err := b.Put([]byte("from_b_2"), []byte("b-two")); err != nil {
		t.Fatal(err)
	}
	mustSync(t, b)

	// A, meanwhile, has its own un-synced local writes.
	if err := a.Put([]byte("from_a_1"), []byte("a-one")); err != nil {
		t.Fatal(err)
	}
	if err := a.Put([]byte("from_a_2"), []byte("a-two")); err != nil {
		t.Fatal(err)
	}

	// A single sync from A must both PULL B's entries and PUSH A's.
	mustSync(t, a)

	want := map[string]string{
		"from_a_1": "a-one", "from_a_2": "a-two",
		"from_b_1": "b-one", "from_b_2": "b-two",
	}
	assertState(t, a, want) // A now holds the union

	// And B, after its own sync, converges to the same union — proving
	// A's writes reached the server too.
	mustSync(t, b)
	assertState(t, b, want)

	// A third, freshly-wiped reader also sees the full union.
	c := device(t, rk, ts.URL)
	mustSync(t, c)
	assertState(t, c, want)

	// Steady state: a follow-up sync on a converged device is a no-op
	// (a matching PROBE), leaving state unchanged.
	mustSync(t, a)
	assertState(t, a, want)
}

// --- 2. last-writer-wins across devices ---

func TestE2E_LWW(t *testing.T) {
	_, ts := newServer(t)
	defer ts.Close()
	rk, _ := vault.NewRandomRootKey()
	a := device(t, rk, ts.URL)
	b := device(t, rk, ts.URL)

	_ = a.Put([]byte("k"), []byte("from-a"))
	mustSync(t, a)
	mustSync(t, b) // b learns k=from-a

	_ = b.Put([]byte("k"), []byte("from-b")) // later write wins
	mustSync(t, b)
	mustSync(t, a) // a pulls the newer write

	for name, c := range map[string]*client.Client{"a": a, "b": b} {
		v, ok, err := c.Get([]byte("k"))
		if err != nil || !ok || !bytes.Equal(v, []byte("from-b")) {
			t.Fatalf("%s: k=%q ok=%v err=%v, want from-b", name, v, ok, err)
		}
	}
}

// --- 3. delete propagates as a tombstone ---

func TestE2E_DeletePropagates(t *testing.T) {
	_, ts := newServer(t)
	defer ts.Close()
	rk, _ := vault.NewRandomRootKey()
	a := device(t, rk, ts.URL)
	b := device(t, rk, ts.URL)

	_ = a.Put([]byte("gone"), []byte("here"))
	_ = a.Put([]byte("stays"), []byte("yes"))
	mustSync(t, a)
	mustSync(t, b)
	assertState(t, b, map[string]string{"gone": "here", "stays": "yes"})

	_ = a.Delete([]byte("gone"))
	mustSync(t, a)
	mustSync(t, b)
	assertState(t, b, map[string]string{"stays": "yes"})
}

// --- 4. backdated insert below the newest entry still propagates ---

func TestE2E_BackdatedInsertPropagates(t *testing.T) {
	_, ts := newServer(t)
	defer ts.Close()
	rk, _ := vault.NewRandomRootKey()
	a := device(t, rk, ts.URL)
	b := device(t, rk, ts.URL)

	for i := range 5 {
		_ = a.Put([]byte{byte('a' + i)}, []byte{byte(i)})
	}
	mustSync(t, a)
	mustSync(t, b)

	// Inject an entry into B's store directly with an older timestamp
	// (within skew) — it did not go through Put, so it sits below B's
	// frontier.
	aead, _ := rk.AEAD()
	oldTs := uint64(time.Now().UnixMilli() - 60_000)
	old, err := vault.Seal(aead, oldTs, []byte("historical"), []byte("\x00once"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Store().Add(old); err != nil {
		t.Fatal(err)
	}

	mustSync(t, b) // B pushes the backdated entry
	mustSync(t, a) // A pulls it
	if v, ok, _ := a.Get([]byte("historical")); !ok || !bytes.Equal(v, []byte("once")) {
		t.Fatalf("A did not receive backdated entry: ok=%v v=%q", ok, v)
	}
}

// --- 5. recovery: wiped client rebuilds from server; wiped server
// rebuilds from client ---

func TestE2E_Recovery(t *testing.T) {
	srv, ts := newServer(t)
	defer ts.Close()
	rk, _ := vault.NewRandomRootKey()

	a := device(t, rk, ts.URL)
	for i := range 20 {
		_ = a.Put([]byte(fmt.Sprintf("k%02d", i)), []byte(fmt.Sprintf("v%d", i)))
	}
	mustSync(t, a)
	want := liveState(t, a)

	// Wiped client: fresh store, same RootKey — pulls the whole vault.
	a2 := device(t, rk, ts.URL)
	mustSync(t, a2)
	assertState(t, a2, want)

	// Wiped server: brand new server on the same URL is not possible with
	// httptest, so build a second server and point a client at it. It
	// starts empty; the client re-registers and rebuilds it.
	_ = srv
	_, ts2 := newServer(t)
	defer ts2.Close()
	a3, err := client.New(rk, cloneStore(t, a.Store()), &client.HTTPTransport{Base: ts2.URL})
	if err != nil {
		t.Fatal(err)
	}
	a3.SetRegisterKey(&testRegKey)
	mustSync(t, a3) // registers on srv2 and pushes everything

	reader := device(t, rk, ts2.URL)
	mustSync(t, reader)
	assertState(t, reader, want)
}

// --- 6. many concurrent writers converge ---

func TestE2E_ConcurrentWritersConverge(t *testing.T) {
	_, ts := newServer(t)
	defer ts.Close()
	rk, _ := vault.NewRandomRootKey()

	const devices = 6
	want := map[string]string{}
	clients := make([]*client.Client, devices)
	for d := range devices {
		clients[d] = device(t, rk, ts.URL)
		for i := range 5 {
			k := fmt.Sprintf("d%d_k%d", d, i)
			v := fmt.Sprintf("val-%d-%d", d, i)
			if err := clients[d].Put([]byte(k), []byte(v)); err != nil {
				t.Fatal(err)
			}
			want[k] = v
		}
		mustSync(t, clients[d])
	}
	// A second sync round lets everyone pull everyone else's writes.
	for _, c := range clients {
		mustSync(t, c)
	}
	for i, c := range clients {
		got := liveState(t, c)
		if len(got) != len(want) {
			t.Fatalf("device %d has %d keys, want %d", i, len(got), len(want))
		}
	}
	assertState(t, clients[0], want)
}

// --- 7. tampered ciphertext fails to open and is skipped at merge ---

func TestE2E_TamperedEntrySkipped(t *testing.T) {
	_, ts := newServer(t)
	defer ts.Close()
	rk, _ := vault.NewRandomRootKey()
	a := device(t, rk, ts.URL)

	_ = a.Put([]byte("good"), []byte("value"))
	mustSync(t, a)

	// Corrupt a ciphertext byte directly in the store and confirm the
	// good key still reads (bad entry silently skipped at replay).
	aead, _ := rk.AEAD()
	bad, _ := vault.Seal(aead, uint64(time.Now().UnixMilli()), []byte("bad"), []byte("\x00x"))
	bad.Ct[len(bad.Ct)-1] ^= 1
	_, _ = a.Store().Add(bad)

	if v, ok, err := a.Get([]byte("good")); err != nil || !ok || !bytes.Equal(v, []byte("value")) {
		t.Fatalf("good key lost after tampered neighbour: ok=%v v=%q err=%v", ok, v, err)
	}
	if _, ok, _ := a.Get([]byte("bad")); ok {
		t.Fatal("tampered entry should not surface a value")
	}
}

// cloneStore copies a store's entries into a fresh MemStore.
func cloneStore(t *testing.T, s store.Store) store.Store {
	t.Helper()
	out := store.NewMemStore()
	for _, e := range s.Sorted() {
		if _, err := out.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	return out
}
