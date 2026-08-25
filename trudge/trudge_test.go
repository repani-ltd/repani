package trudge

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// The tiny parameterization (poolBits 10, steps 1024) exists only
// for tests and cross-implementation checking; SPEC.t freezes
// these vectors as the contract. The parameters are bound into
// the fill preimage, so tiny and trudge1 outputs can never be
// confused.
var tinyVectors = []struct {
	salt, passphrase, hexOut string
}{
	{"W1AW", "1234 19910713 CQCQ", "f896b74a303a7c86f2bcf1cb66d6a9f3ea6135f80686ab38b46d16f384ec29d4"},
	{"EA5XYZ", "1234 19910713 CQCQ", "b91e774d0f31a6d9f7c585131480ab6a7d9321481135935c2a5692bb18cf1627"},
	{"", "", "4cf9055bbb1c4beadb3ed2577e708e13963cf9063465b83f3b9f51775ae1d88f"},
}

func TestTinyVectors(t *testing.T) {
	for _, v := range tinyVectors {
		out := make([]byte, 32)
		derive(10, 1024, []byte(v.salt), []byte(v.passphrase), out)
		want, err := hex.DecodeString(v.hexOut)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, want) {
			t.Errorf("derive(tiny, %q, %q) = %x, want %s", v.salt, v.passphrase, out, v.hexOut)
		}
	}
}

func TestOutputLengths(t *testing.T) {
	// Outputs of different lengths are prefix-consistent (one
	// finalizing XOF squeezed longer), per SPEC.t.
	long := make([]byte, 64)
	short := make([]byte, 8)
	derive(10, 1024, []byte("W1AW"), []byte("x"), long)
	derive(10, 1024, []byte("W1AW"), []byte("x"), short)
	if !bytes.Equal(long[:8], short) {
		t.Errorf("8-byte output %x is not a prefix of 64-byte output %x", short, long[:8])
	}
}

func TestInputSeparation(t *testing.T) {
	// Length prefixes: moving a byte across the salt/passphrase
	// boundary must change the key.
	a, b := make([]byte, 16), make([]byte, 16)
	derive(10, 1024, []byte("AB"), []byte("CD"), a)
	derive(10, 1024, []byte("ABC"), []byte("D"), b)
	if bytes.Equal(a, b) {
		t.Error("salt/passphrase boundary shift did not change the key")
	}
}

// TestTrudge1Vector runs the real parameters: 256 MiB and roughly
// ten seconds. Skipped under -short.
func TestTrudge1Vector(t *testing.T) {
	if testing.Short() {
		t.Skip("trudge1 takes ~10 s and 256 MiB")
	}
	out := make([]byte, 32)
	Key([]byte("W1AW"), []byte("1234 19910713 CQCQ"), out)
	const want = "12b1053b2cdbfd8ded9e52635e727ac2fca7c79e12aacb58d854bcb163931b71"
	if hex.EncodeToString(out) != want {
		t.Errorf("Key = %x, want %s", out, want)
	}
}

func BenchmarkKey(b *testing.B) {
	out := make([]byte, 32)
	for b.Loop() {
		Key([]byte("W1AW"), []byte("bench"), out)
	}
}
