package client

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/pavlos/kv/server"
	"github.com/pavlos/kv/store"
	"github.com/pavlos/kv/vault"
	"github.com/pavlos/kv/wire"
)

// writeFileBytes is a small helper used by gated tests.
func writeFileBytes(path string, b []byte) error {
	return os.WriteFile(path, b, 0o600)
}

// testRegKey is the fixed register-gate secret used by the default
// test helpers. Every test that calls newPair / newTestServer shares
// it so the client can complete REGISTER.
var testRegKey = vault.RegisterKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

// newTestServer returns an in-memory server wired with testRegKey.
func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	srv, err := server.NewWithOptions(server.Options{RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// inProcTransport sends the request bytes directly to a server.Server
// in the same process, bypassing HTTP. Used by tests.
type inProcTransport struct {
	srv *server.Server
}

func (t *inProcTransport) Do(_ context.Context, req []byte) ([]byte, error) {
	return t.srv.HandleSync(req)
}

func newPair(t *testing.T) (*Client, *server.Server) {
	t.Helper()
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	c, err := New(rk, store.NewMemStore(), &inProcTransport{srv})
	if err != nil {
		t.Fatal(err)
	}
	c.SetRegisterKey(&testRegKey)
	return c, srv
}

func TestSteadyState_NoOp(t *testing.T) {
	c, _ := newPair(t)
	if err := c.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Second sync must also be a no-op.
	if err := c.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPutGetDelete_Local(t *testing.T) {
	c, _ := newPair(t)
	if err := c.Put([]byte("k1"), []byte("v1")); err != nil {
		t.Fatal(err)
	}
	v, ok, err := c.Get([]byte("k1"))
	if err != nil || !ok {
		t.Fatalf("Get: v=%q ok=%v err=%v", v, ok, err)
	}
	if !bytes.Equal(v, []byte("v1")) {
		t.Fatalf("got %q want v1", v)
	}
	if err := c.Delete([]byte("k1")); err != nil {
		t.Fatal(err)
	}
	_, ok, err = c.Get([]byte("k1"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected tombstoned key to be absent")
	}
}

func TestSync_LocalWriteReachesServer(t *testing.T) {
	ctx := context.Background()
	c, srv := newPair(t)
	if err := c.Put([]byte("apple"), []byte("green")); err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	// Second client with the same RootKey, fresh store, syncs from
	// the same server — should see the entry.
	c2, err := New(c.rk, store.NewMemStore(), &inProcTransport{srv})
	if err != nil {
		t.Fatal(err)
	}
	if err := c2.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	v, ok, err := c2.Get([]byte("apple"))
	if err != nil || !ok {
		t.Fatalf("c2 Get apple: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(v, []byte("green")) {
		t.Fatalf("c2 got %q want green", v)
	}
}

func TestSync_ClientWipedRecoversFromServer(t *testing.T) {
	ctx := context.Background()
	c, srv := newPair(t)
	for i := range 5 {
		if err := c.Put([]byte{byte('a' + i)}, []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	// "Wipe" client: new memstore, same RootKey.
	c2, _ := New(c.rk, store.NewMemStore(), &inProcTransport{srv})
	if err := c2.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if l := c2.store.Len(); l != 5 {
		t.Fatalf("c2 after wipe+sync has %d entries, want 5", l)
	}
}

func TestSync_ServerWipedRecoversFromClient(t *testing.T) {
	ctx := context.Background()
	c, _ := newPair(t)
	for i := range 7 {
		_ = c.Put([]byte{byte('a' + i)}, []byte{byte(i)})
	}
	if err := c.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	// Rebuild a fresh server with the same VaultID (empty store).
	// Plug the client into it and sync.
	srv2 := newTestServer(t)
	c.tran = &inProcTransport{srv2}
	// The client was registered on the old server; on the fresh
	// server it must re-REGISTER, so flip the bit.
	c.SetRegistered(false)
	if err := c.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	// Server should now have all 7 entries: assert by pulling from a
	// fresh client against srv2.
	c2, _ := New(c.rk, store.NewMemStore(), &inProcTransport{srv2})
	c2.SetRegisterKey(&testRegKey)
	if err := c2.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if c2.store.Len() != 7 {
		t.Fatalf("after server-wipe recovery, c2 Len=%d want 7", c2.store.Len())
	}
}

func TestSync_ConvergesInLogRounds(t *testing.T) {
	ctx := context.Background()
	c, _ := newPair(t)
	const n = 100
	for i := range n {
		_ = c.Put([]byte{byte(i & 0xff), byte(i >> 8)}, []byte("v"))
	}
	// Fresh server to simulate wipe; sync from scratch.
	srv2 := newTestServer(t)
	c.tran = &inProcTransport{srv2}
	c.SetRegistered(false)

	rounds := 0
	for {
		done, err := c.syncOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rounds++
		if done {
			break
		}
		if rounds > 20 {
			t.Fatalf("too many rounds for N=%d", n)
		}
	}
	// Upper bound from protocol: log2(N) + a small constant.
	if rounds > 10 {
		t.Fatalf("converged in %d rounds for N=%d; expected O(log N)", rounds, n)
	}
}

// TestReplay_SilentlyDiscardsUnopenableEntries verifies that a stored
// entry which fails AEAD open does not poison the client. A fresh
// client replays the store for every Get/List; a failing entry was
// previously fatal. The protocol now requires silent discard.
func TestReplay_SilentlyDiscardsUnopenableEntries(t *testing.T) {
	c, _ := newPair(t)
	if err := c.Put([]byte("good"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	// Craft a garbage entry that will fail to open: random bytes in
	// the ciphertext under this client's AEAD context. Use a valid
	// header shape so the store will accept it.
	garbage := vault.Entry{
		TsMs:  1,
		Nonce: [vault.NonceSize]byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9},
		Ct:    bytes.Repeat([]byte{0xCC}, 32),
	}
	if _, err := c.Store().Add(garbage); err != nil {
		t.Fatal(err)
	}
	// Get must still work and not return an error.
	v, ok, err := c.Get([]byte("good"))
	if err != nil {
		t.Fatalf("replay errored on unopenable entry: %v", err)
	}
	if !ok || !bytes.Equal(v, []byte("v")) {
		t.Fatalf("legit key lost; ok=%v v=%q", ok, v)
	}
}

// maulTransport lets a test rewrite the response bytes just before
// the client parses them.
type maulTransport struct {
	srv   *server.Server
	mangle func([]byte) []byte
}

func (t *maulTransport) Do(_ context.Context, req []byte) ([]byte, error) {
	resp, err := t.srv.HandleSync(req)
	if err != nil {
		return nil, err
	}
	return t.mangle(resp), nil
}

// TestSync_RejectsForgedResponse verifies the client refuses to
// ingest a response whose MAC does not verify under its AuthKey.
// A previous revision of the client would blindly Add() every entry
// from the response, poisoning the local store.
func TestSync_RejectsForgedResponse(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	tran := &maulTransport{srv: srv, mangle: func(b []byte) []byte {
		// Flip the last byte of the response MAC.
		if len(b) == 0 {
			return b
		}
		out := append([]byte(nil), b...)
		out[len(out)-1] ^= 1
		return out
	}}
	c, err := New(rk, store.NewMemStore(), tran)
	if err != nil {
		t.Fatal(err)
	}
	c.SetRegisterKey(&testRegKey)
	err = c.Sync(context.Background())
	if err == nil {
		t.Fatal("expected MAC-verify failure, got nil")
	}
	if !errors.Is(err, wire.ErrBadMAC) {
		t.Fatalf("expected ErrBadMAC, got %v", err)
	}
}

// --- register-key gate (end-to-end) ---

// gatedPair returns a client+server pair where the server requires
// reg_admin_mac under the given register key; the client is pre-
// configured to present the matching key.
func gatedPair(t *testing.T, regKey vault.RegisterKey) (*Client, *server.Server) {
	t.Helper()
	dir := t.TempDir()
	keyPath := dir + "/register.key"
	if err := writeFileBytes(keyPath, regKey[:]); err != nil {
		t.Fatal(err)
	}
	srv, err := server.NewWithOptions(server.Options{RegisterKeyFile: keyPath})
	if err != nil {
		t.Fatal(err)
	}
	rk, _ := vault.NewRandomRootKey()
	c, err := New(rk, store.NewMemStore(), &inProcTransport{srv})
	if err != nil {
		t.Fatal(err)
	}
	c.SetRegisterKey(&regKey)
	return c, srv
}

func TestSync_Gated_Accepts(t *testing.T) {
	var regKey vault.RegisterKey
	for i := range regKey {
		regKey[i] = byte(0x55 ^ i)
	}
	c, _ := gatedPair(t, regKey)
	if err := c.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(context.Background()); err != nil {
		t.Fatalf("gated sync: %v", err)
	}
	if v, ok, _ := c.Get([]byte("k")); !ok || string(v) != "v" {
		t.Fatalf("round-trip through gated server failed")
	}
}

func TestSync_Gated_WrongKeyRejected(t *testing.T) {
	var serverKey, clientKey vault.RegisterKey
	for i := range serverKey {
		serverKey[i] = byte(0x11 + i)
		clientKey[i] = byte(0xEE - i)
	}
	c, _ := gatedPair(t, serverKey)
	c.SetRegisterKey(&clientKey) // override with wrong key
	err := c.Sync(context.Background())
	if err == nil {
		t.Fatal("sync should have failed with wrong register key")
	}
}

func TestSync_Gated_NoKeyRejected(t *testing.T) {
	var serverKey vault.RegisterKey
	for i := range serverKey {
		serverKey[i] = byte(i)
	}
	c, _ := gatedPair(t, serverKey)
	c.SetRegisterKey(nil) // unset
	err := c.Sync(context.Background())
	if err == nil {
		t.Fatal("sync should have failed without register key on gated server")
	}
}

func TestSync_RemoteWritePulledByReader(t *testing.T) {
	ctx := context.Background()
	c, srv := newPair(t)
	// c acts as writer, c2 as reader.
	c2, _ := New(c.rk, store.NewMemStore(), &inProcTransport{srv})

	// c writes something and syncs.
	_ = c.Put([]byte("k"), []byte("v"))
	_ = c.Sync(ctx)

	// c2 polls — should pick it up in one round.
	done, err := c2.syncOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if done {
		t.Fatal("expected divergence on first poll after remote write")
	}
	// second round should be clean
	done, err = c2.syncOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("expected convergence by second round")
	}
	v, ok, err := c2.Get([]byte("k"))
	if err != nil || !ok || !bytes.Equal(v, []byte("v")) {
		t.Fatalf("c2 Get k: v=%q ok=%v err=%v", v, ok, err)
	}
}
