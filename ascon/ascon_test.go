package ascon

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func hexDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex decode %q: %v", s, err)
	}
	return b
}

// --- AEAD-128 known-answer tests ---
//
// Vectors verified against brainfoolong/js-ascon v1.3.0 (Ascon-AEAD128,
// NIST SP 800-232 little-endian byte ordering). Inherited from
// quietcasting-go/protocol/ascon_aead_test.go.
func TestAEAD_KAT(t *testing.T) {
	key := hexDecode(t, "000102030405060708090a0b0c0d0e0f")
	nonce := hexDecode(t, "000102030405060708090a0b0c0d0e0f")
	a, err := NewAEAD(key)
	if err != nil {
		t.Fatalf("NewAEAD: %v", err)
	}

	cases := []struct {
		name    string
		pt, ad  string
		wantHex string
	}{
		{
			name: "empty", pt: "", ad: "",
			wantHex: "4427d64b8e1e1451fc445960f0839bb0",
		},
		{
			name: "pt10_ad8",
			pt:   "00010203040506070809",
			ad:   "0001020304050607",
			wantHex: "108640bd71345c6edc4ae686279ea0c1" +
				"723af33eadb5d77ef381",
		},
		{
			name:    "pt16_exact_block",
			pt:      "000102030405060708090a0b0c0d0e0f",
			ad:      "",
			wantHex: "e770d289d2a44aee7cd0a48ece5274e3ea721f9a8fc4e556f2745972f5a78411",
		},
		{
			name: "pt32_ad16",
			pt:   "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
			ad:   "000102030405060708090a0b0c0d0e0f",
			wantHex: "6a28215e4a6023fae42095318b187f99" +
				"c56d2306be1573f6e3ecfe0df819db87" +
				"363a60d9cda76645d7cbed45b7c56470",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pt, ad []byte
			if tc.pt != "" {
				pt = hexDecode(t, tc.pt)
			}
			if tc.ad != "" {
				ad = hexDecode(t, tc.ad)
			}
			ct := a.Seal(nil, nonce, pt, ad)
			if got := hex.EncodeToString(ct); got != tc.wantHex {
				t.Errorf("Seal mismatch:\n got  %s\n want %s", got, tc.wantHex)
			}
			opened, err := a.Open(nil, nonce, ct, ad)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if !bytes.Equal(opened, pt) {
				t.Fatalf("plaintext round-trip mismatch")
			}
		})
	}
}

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

func TestAEAD_BadKeySize(t *testing.T) {
	if _, err := NewAEAD(make([]byte, 15)); !errors.Is(err, ErrAEADKeySize) {
		t.Fatalf("expected ErrAEADKeySize, got %v", err)
	}
}

// --- XOF sanity ---

// TestXOF_Deterministic checks the XOF produces reproducible output
// and that length parameter is honoured precisely.
func TestXOF_Deterministic(t *testing.T) {
	data := []byte("ascon-kv")
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
