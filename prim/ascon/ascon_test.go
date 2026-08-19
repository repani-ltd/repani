package ascon

import (
	"bytes"
	"errors"
	"testing"
)

func TestAEAD_AuthFailure(t *testing.T) {
	key := make([]byte, 16)
	nonce := make([]byte, 16)
	a, err := NewAEAD(key)
	if err != nil {
		t.Fatal(err)
	}
	ct := a.Seal(nil, nonce, []byte("hello"), []byte("ad"))
	ct[0] ^= 1
	if _, err := a.Open(nil, nonce, ct, []byte("ad")); !errors.Is(err, ErrAEADAuth) {
		t.Fatalf("expected ErrAEADAuth on tampered ciphertext, got %v", err)
	}
}

// TestAEAD_Zero checks that Zero wipes the key and makes the AEAD
// refuse further use.
func TestAEAD_Zero(t *testing.T) {
	a, err := NewAEAD([]byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	a.Zero()
	if a.k0 != 0 || a.k1 != 0 {
		t.Fatalf("key not wiped: %#x %#x", a.k0, a.k1)
	}
	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s after Zero did not panic", name)
			}
		}()
		f()
	}
	nonce := make([]byte, 16)
	mustPanic("Seal", func() { a.Seal(nil, nonce, []byte("x"), nil) })
	mustPanic("Open", func() { a.Open(nil, nonce, make([]byte, 17), nil) })
}

// TestAEAD_OpenFailureZeroesOutput checks that a failed in-place
// Open leaves no plaintext bytes in the output region.
func TestAEAD_OpenFailureZeroesOutput(t *testing.T) {
	a, err := NewAEAD(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 16)
	buf := a.Seal(nil, nonce, []byte("attack at dawn"), nil)
	buf[len(buf)-1] ^= 1
	if _, err := a.Open(buf[:0], nonce, buf, nil); !errors.Is(err, ErrAEADAuth) {
		t.Fatalf("got %v, want ErrAEADAuth", err)
	}
	for i, b := range buf[:len(buf)-AEADTagSize] {
		if b != 0 {
			t.Fatalf("output byte %d not zeroed: %#x", i, b)
		}
	}
}

func TestAEAD_BadKeySize(t *testing.T) {
	if _, err := NewAEAD(make([]byte, 15)); !errors.Is(err, ErrAEADKeySize) {
		t.Fatalf("expected ErrAEADKeySize, got %v", err)
	}
}

// --- XOF sanity ---

// TestXOF_Deterministic checks the XOF produces reproducible output
// and that length parameter is honoured precisely.
func TestXOF_Deterministic(t *testing.T) {
	data := []byte("ascon xof")
	a := make([]byte, 32)
	b := make([]byte, 32)
	XOF(data, a)
	XOF(data, b)
	if !bytes.Equal(a, b) {
		t.Fatalf("XOF not deterministic")
	}
	// Different length request of same data must share the prefix.
	short := make([]byte, 8)
	XOF(data, short)
	if !bytes.Equal(a[:8], short) {
		t.Fatalf("XOF output length not stable: prefix %x vs %x", a[:8], short)
	}
}

// TestXOF_DifferentInputs: two distinct inputs produce distinct 16-byte
// outputs (sanity, not cryptographic strength).
func TestXOF_DifferentInputs(t *testing.T) {
	x := Sum16([]byte("a"))
	y := Sum16([]byte("b"))
	if x == y {
		t.Fatalf("XOF collided on trivial inputs")
	}
}
