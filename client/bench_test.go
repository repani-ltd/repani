package client

import (
	"context"
	"fmt"
	"testing"

	"repani.com/kv/server"
	"repani.com/kv/store"
	"repani.com/kv/vault"
)

// benchVault returns two devices of one vault holding n entries, both
// synced, against an in-process server with room to grow.
func benchVault(b *testing.B, n int) (a, c *Client) {
	b.Helper()
	srv, err := server.NewWithOptions(server.Options{RegisterKey: &testRegKey, Quotas: server.Quotas{MaxEntries: 1 << 30, MaxVaultBytes: 1 << 40}})
	if err != nil {
		b.Fatal(err)
	}
	rk, _ := vault.NewRandomRootKey()
	dev := func() *Client {
		d, err := New(rk, store.NewMemStore(), &inProc{srv})
		if err != nil {
			b.Fatal(err)
		}
		d.SetRegisterKey(&testRegKey)
		return d
	}
	a, c = dev(), dev()
	for i := range n {
		if err := a.Put(fmt.Appendf(nil, "key/%05d", i), []byte("a value of some ordinary length")); err != nil {
			b.Fatal(err)
		}
	}
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		b.Fatal(err)
	}
	if err := c.Sync(ctx); err != nil {
		b.Fatal(err)
	}
	return a, c
}

// BenchmarkWriteSyncRead is the common case on a 5,000-entry vault: a
// device writes one key, syncs, and reads it back.
func BenchmarkWriteSyncRead(b *testing.B) {
	a, _ := benchVault(b, 5000)
	ctx := context.Background()
	i := 0
	for b.Loop() {
		key := fmt.Appendf(nil, "new/%d", i)
		if err := a.Put(key, []byte("v")); err != nil {
			b.Fatal(err)
		}
		if err := a.Sync(ctx); err != nil {
			b.Fatal(err)
		}
		if _, ok, _ := a.Get(key); !ok {
			b.Fatal("written key not read back")
		}
		i++
	}
}

// BenchmarkReceiveOne: another device wrote one key; this one syncs
// and reads it.
func BenchmarkReceiveOne(b *testing.B) {
	a, c := benchVault(b, 5000)
	ctx := context.Background()
	i := 0
	for b.Loop() {
		key := fmt.Appendf(nil, "new/%d", i)
		b.StopTimer()
		if err := a.Put(key, []byte("v")); err != nil {
			b.Fatal(err)
		}
		if err := a.Sync(ctx); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := c.Sync(ctx); err != nil {
			b.Fatal(err)
		}
		if _, ok, _ := c.Get(key); !ok {
			b.Fatal("pulled key not read")
		}
		i++
	}
}

// BenchmarkIdleSync is a sync with nothing to do: one PROBE.
func BenchmarkIdleSync(b *testing.B) {
	a, _ := benchVault(b, 5000)
	ctx := context.Background()
	for b.Loop() {
		if err := a.Sync(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGet reads one key of an unchanged vault.
func BenchmarkGet(b *testing.B) {
	a, _ := benchVault(b, 5000)
	for b.Loop() {
		if _, ok, _ := a.Get([]byte("key/02500")); !ok {
			b.Fatal("key missing")
		}
	}
}
