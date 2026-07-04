package client

import (
	"bytes"
	"context"
	"testing"

	"github.com/pavlos/kv/server"
	"github.com/pavlos/kv/store"
	"github.com/pavlos/kv/vault"
)

var testRegKey = vault.RegisterKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	srv, err := server.NewWithOptions(server.Options{RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// inProc sends frames straight to a Server, bypassing HTTP.
type inProc struct{ srv *server.Server }

func (t *inProc) Do(_ context.Context, req []byte) ([]byte, error) {
	return t.srv.HandleFrame(req)
}

// maul rewrites the response bytes before the client parses them.
type maul struct {
	srv    *server.Server
	mangle func([]byte) []byte
}

func (t *maul) Do(_ context.Context, req []byte) ([]byte, error) {
	resp, err := t.srv.HandleFrame(req)
	if err != nil {
		return nil, err
	}
	return t.mangle(resp), nil
}

func newClient(t *testing.T, srv *server.Server, rk vault.RootKey) *Client {
	t.Helper()
	c, err := New(rk, store.NewMemStore(), &inProc{srv})
	if err != nil {
		t.Fatal(err)
	}
	c.SetRegisterKey(&testRegKey)
	return c
}

// newPair returns a client and its server (used by impexp_test).
func newPair(t *testing.T) (*Client, *server.Server) {
	t.Helper()
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	return newClient(t, srv, rk), srv
}

func TestPutGetDelete_FmtTagTransparent(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, srv, rk)

	if err := c.Put([]byte("k1"), []byte("v1")); err != nil {
		t.Fatal(err)
	}
	v, ok, err := c.Get([]byte("k1"))
	if err != nil || !ok || !bytes.Equal(v, []byte("v1")) {
		t.Fatalf("Get: v=%q ok=%v err=%v", v, ok, err)
	}
	// Binary values with a leading 0x00 must round-trip (the fmt tag is
	// distinct from value bytes).
	bin := []byte{0x00, 0x01, 0xff, 0x00}
	if err := c.Put([]byte("bin"), bin); err != nil {
		t.Fatal(err)
	}
	v, ok, _ = c.Get([]byte("bin"))
	if !ok || !bytes.Equal(v, bin) {
		t.Fatalf("binary value round-trip: got %v", v)
	}

	if err := c.Delete([]byte("k1")); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Get([]byte("k1")); ok {
		t.Fatal("expected tombstoned key absent")
	}
}

func TestPut_RejectsEmptyValue(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, srv, rk)
	if err := c.Put([]byte("k"), nil); err == nil {
		t.Fatal("expected empty value to be rejected")
	}
}

func TestSync_LocalWriteReachesReader(t *testing.T) {
	ctx := context.Background()
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()

	a := newClient(t, srv, rk)
	if err := a.Put([]byte("apple"), []byte("green")); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	b := newClient(t, srv, rk) // fresh store, same RootKey
	if err := b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := b.Get([]byte("apple")); !ok || !bytes.Equal(v, []byte("green")) {
		t.Fatalf("reader saw %q (ok=%v)", v, ok)
	}
	// Second sync on a converged client is a clean no-op (matching probe).
	if err := b.Sync(ctx); err != nil {
		t.Fatalf("steady-state sync: %v", err)
	}
}

// TestSync_RejectsForgedResponse verifies the client refuses a response
// whose seal does not open (any bit flipped in the sealed region).
func TestSync_RejectsForgedResponse(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	srv := newTestServer(t)
	tran := &maul{srv: srv, mangle: func(b []byte) []byte {
		if len(b) <= 1 {
			return b // don't disturb pre-channel bytes
		}
		out := append([]byte(nil), b...)
		out[len(out)-1] ^= 1 // flip a tag byte
		return out
	}}
	c, err := New(rk, store.NewMemStore(), tran)
	if err != nil {
		t.Fatal(err)
	}
	c.SetRegisterKey(&testRegKey)
	if err := c.Sync(context.Background()); err == nil {
		t.Fatal("expected sync to fail on a forged response")
	}
}

func TestSync_GatedWrongKeyRejected(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, srv, rk)
	var wrong vault.RegisterKey
	for i := range wrong {
		wrong[i] = 0xEE
	}
	c.SetRegisterKey(&wrong)
	if err := c.Sync(context.Background()); err == nil {
		t.Fatal("expected registration to fail with wrong register key")
	}
}

func TestReplay_SkipsUnopenableEntry(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, srv, rk)
	if err := c.Put([]byte("good"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	// Inject a garbage entry that cannot open.
	garbage := vault.Entry{
		TsMs:  1,
		Nonce: [vault.NonceSize]byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9},
		Ct:    bytes.Repeat([]byte{0xCC}, 32),
	}
	if _, err := c.Store().Add(garbage); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := c.Get([]byte("good")); err != nil || !ok || !bytes.Equal(v, []byte("v")) {
		t.Fatalf("good key lost: ok=%v v=%q err=%v", ok, v, err)
	}
}
