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
	{"W1AW", "1234 19910713 CQCQ", "6e425ab059d38dd3a65a616a3786cac60ace49953234132f9bacf10ee7874e41"},
	{"EA5XYZ", "1234 19910713 CQCQ", "7116d62f3fb7249b6d745c0fab77595679c6a149afe1d4391246c1bb2f804b17"},
	{"", "", "6047214a94c9c53d3d00ad0082ca33bae7bb8e7e32b282a89ebc70cd9862da0c"},
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
		t.Skip("trudge1 takes ~20 s and 256 MiB")
	}
	out := make([]byte, 32)
	Key([]byte("W1AW"), []byte("1234 19910713 CQCQ"), out)
	const want = "e2696e33e5e5eefc0e1679ed0273564d7266fd1e5eed40509072e9ab85a430f9"
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
