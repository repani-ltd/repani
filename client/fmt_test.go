package client

import (
	"bytes"
	"testing"

	"repani.com/kv/store"
	"repani.com/kv/vault"
)

// TestPutFmtRoundTrip checks that a format tag written with PutFmt
// survives the local merge and surfaces on LiveEntry.Fmt, and that
// plain Put keeps the raw tag.
func TestPutFmtRoundTrip(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	c, err := New(rk, store.NewMemStore(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Put([]byte("raw"), []byte("v0")); err != nil {
		t.Fatal(err)
	}
	if err := c.PutFmt([]byte("tagged"), []byte("v1"), 0x51); err != nil {
		t.Fatal(err)
	}

	all, err := c.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("All returned %d entries, want 2", len(all))
	}
	byKey := map[string]LiveEntry{}
	for _, le := range all {
		byKey[string(le.Key)] = le
	}
	if le := byKey["raw"]; le.Fmt != 0x00 || !bytes.Equal(le.Value, []byte("v0")) {
		t.Fatalf("raw entry: fmt=0x%02x value=%q", le.Fmt, le.Value)
	}
	if le := byKey["tagged"]; le.Fmt != 0x51 || !bytes.Equal(le.Value, []byte("v1")) {
		t.Fatalf("tagged entry: fmt=0x%02x value=%q", le.Fmt, le.Value)
	}

	// Get is fmt-agnostic and returns the payload.
	v, ok, err := c.Get([]byte("tagged"))
	if err != nil || !ok || !bytes.Equal(v, []byte("v1")) {
		t.Fatalf("Get tagged: %q ok=%v err=%v", v, ok, err)
	}
}
