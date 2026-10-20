package client

import (
	"context"
	"errors"
	"testing"

	"repani.com/kv/store"
	"repani.com/kv/vault"
)

// TestEphemeral_HandOver is the exchange ephemeral vaults exist for: a
// giver registers one and writes a secret, a receiver holding only the
// RootKey reads it and drops the vault, and from then on neither can
// sync it, nor bring it back.
func TestEphemeral_HandOver(t *testing.T) {
	ctx := context.Background()
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	giver, err := New(rk, store.NewMemStore(), &inProc{srv}, Options{RegisterKey: &testRegKey, Ephemeral: true})
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := New(rk, store.NewMemStore(), &inProc{srv}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if err := receiver.Sync(ctx); !errors.Is(err, ErrNoRegisterKey) {
		t.Fatalf("receiver before the vault exists: %v, want ErrNoRegisterKey", err)
	}
	if err := giver.Put([]byte("keyring"), []byte("secret")); err != nil {
		t.Fatal(err)
	}
	if err := giver.Sync(ctx); err != nil {
		t.Fatalf("giver sync: %v", err)
	}
	if err := receiver.Sync(ctx); err != nil {
		t.Fatalf("receiver sync: %v", err)
	}
	if v, ok, _ := receiver.Get([]byte("keyring")); !ok || string(v) != "secret" {
		t.Fatalf("receiver read %q %v, want the secret", v, ok)
	}
	if err := receiver.Drop(ctx); err != nil {
		t.Fatalf("drop: %v", err)
	}

	for name, c := range map[string]*Client{"giver": giver, "receiver": receiver} {
		if err := c.Sync(ctx); !errors.Is(err, ErrVaultEnded) {
			t.Fatalf("%s sync after the drop: %v, want ErrVaultEnded", name, err)
		}
	}
	if err := receiver.Drop(ctx); !errors.Is(err, ErrVaultEnded) {
		t.Fatalf("second drop: %v, want ErrVaultEnded", err)
	}
	// A fresh client with the register key cannot bring it back.
	again := newClient(t, srv, rk)
	if err := again.Sync(ctx); err == nil {
		t.Fatal("a dropped vault was registered again")
	}
}

// TestEphemeral_OrdinaryVaultDoesNotDrop: Drop on a vault registered
// without the flag is ErrNotEphemeral and leaves it whole.
func TestEphemeral_OrdinaryVaultDoesNotDrop(t *testing.T) {
	ctx := context.Background()
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, srv, rk)
	if err := c.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Drop(ctx); !errors.Is(err, ErrNotEphemeral) {
		t.Fatalf("drop of an ordinary vault: %v, want ErrNotEphemeral", err)
	}
	other := newClient(t, srv, rk)
	if err := other.Sync(ctx); err != nil {
		t.Fatalf("sync after the refused drop: %v", err)
	}
	if v, ok, _ := other.Get([]byte("k")); !ok || string(v) != "v" {
		t.Fatalf("read %q %v after the refused drop", v, ok)
	}
}
