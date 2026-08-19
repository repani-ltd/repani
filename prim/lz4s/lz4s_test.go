package lz4s

import (
	"bytes"
	"math/rand"
	"testing"
)

func roundTrip(t *testing.T, name string, src []byte) {
	t.Helper()
	comp := Compress(src)
	got, ok := Decompress(comp, len(src))
	if !ok || !bytes.Equal(got, src) {
		t.Fatalf("%s: round trip failed (ok=%v, %d -> %d bytes)", name, ok, len(src), len(comp))
	}
}

func TestRoundTrip(t *testing.T) {
	roundTrip(t, "empty", nil)
	roundTrip(t, "one", []byte("a"))
	roundTrip(t, "repeat", bytes.Repeat([]byte("abc"), 200))
	roundTrip(t, "prose", []byte("the quick brown fox jumps over the lazy dog; "+
		"the quick brown fox jumps over the lazy dog again and again and again."))
	// Long literal run (>= 7 + 255 extension) and long match (>= 17).
	rng := rand.New(rand.NewSource(1))
	lit := make([]byte, 600)
	rng.Read(lit)
	roundTrip(t, "literals", lit)
	roundTrip(t, "longmatch", append(append([]byte{}, lit...), lit...))
	// Far match (dist > 256) exercises the wide offset.
	far := append(bytes.Repeat([]byte("x"), 300), []byte("hello world")...)
	far = append(far, bytes.Repeat([]byte("y"), 300)...)
	far = append(far, []byte("hello world")...)
	roundTrip(t, "far", far)
}

func TestDecompressRejectsTruncation(t *testing.T) {
	src := bytes.Repeat([]byte("abcdefgh"), 50)
	comp := Compress(src)
	for i := 0; i < len(comp); i++ {
		if _, ok := Decompress(comp[:i], len(src)); ok {
			t.Fatalf("truncated input of %d bytes accepted", i)
		}
	}
	if _, ok := Decompress(comp, len(src)+1); ok {
		t.Fatal("wrong dsize accepted")
	}
}
