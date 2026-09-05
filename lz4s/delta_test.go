package lz4s

import (
	"bytes"
	"os"
	"testing"
)

// Delta against the previous version of a page: the known answers of
// the corpus pairs, and the contract's edges.
func TestDelta(t *testing.T) {
	for _, pair := range []struct {
		base, src string
		size      int
	}{
		{"qam-report", "qam-report2", 29},
		{"tess-harbour", "tess-harbour2", 32},
	} {
		base, _ := os.ReadFile("testdata/" + pair.base + ".bin")
		src, _ := os.ReadFile("testdata/" + pair.src + ".bin")
		d := Delta(base, src)
		if got, ok := Undelta(base, d, len(src)); !ok || !bytes.Equal(got, src) {
			t.Fatalf("%s: round trip failed", pair.src)
		}
		if len(d) != pair.size {
			t.Errorf("%s against %s: %d bytes, want %d", pair.src, pair.base, len(d), pair.size)
		}
		// The wrong base is not detected by the decoder: it decodes to
		// the right length and the wrong bytes, or fails. The caller
		// names the base.
		other, _ := os.ReadFile("testdata/qam-home.bin")
		if got, ok := Undelta(other, d, len(src)); ok && bytes.Equal(got, src) {
			t.Errorf("%s: decoded correctly against the wrong base", pair.src)
		}
		if _, ok := Undelta(base, d, len(src)-1); ok {
			t.Errorf("%s: accepted the wrong size", pair.src)
		}
	}
	// An empty base is Compress and Decompress exactly.
	src := []byte("abcabcabcdefdefdef the quick brown fox")
	if !bytes.Equal(Delta(nil, src), Compress(src)) {
		t.Error("Delta(nil) differs from Compress")
	}
	if got, ok := Undelta(nil, Compress(src), len(src)); !ok || !bytes.Equal(got, src) {
		t.Error("Undelta(nil) differs from Decompress")
	}
	// A page delta'd against itself is a single match.
	if d := Delta(src, src); len(d) > 5 {
		t.Errorf("self delta = % x", d)
	}
}
