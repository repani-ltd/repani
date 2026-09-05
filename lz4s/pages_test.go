package lz4s

import (
	"bytes"
	"os"
	"testing"
)

// Real raster pages (typeset/raster bytes: six of a 48 by 20 app,
// five of tessera's 34 by 28 by 4) round-trip, and their compressed
// sizes are the parser's known answers: a change here is a change of
// the canonical encoding.
func TestPages(t *testing.T) {
	want := map[string]int{
		"qam-home": 562, "qam-report": 264, "qam-report2": 265, "qam-search": 396, "qam-trend": 245, "qam-near": 334,
		"tess-aegean": 1877, "tess-harbour": 1065, "tess-harbour2": 1056, "tess-gallery": 1064, "tess-features": 1517,
	}
	for name, size := range want {
		src, err := os.ReadFile("testdata/" + name + ".bin")
		if err != nil {
			t.Fatal(err)
		}
		comp := Compress(src)
		if got, ok := Decompress(comp, len(src)); !ok || !bytes.Equal(got, src) {
			t.Fatalf("%s: round trip failed", name)
		}
		if len(comp) != size {
			t.Errorf("%s: %d bytes, want %d", name, len(comp), size)
		}
	}
}

// Where the optimal parser beats a greedy one: a three-byte match
// taken greedily would cut a longer one starting a byte later.
func TestOptimalBeatsGreedy(t *testing.T) {
	src := []byte("xabcxx" + "yabcdefghijk" + "xabc" + "yabcdefghijk")
	comp := Compress(src)
	if got, ok := Decompress(comp, len(src)); !ok || !bytes.Equal(got, src) {
		t.Fatal("round trip")
	}
	// Greedy: at the third "xabc" it takes "xabc" (4, dist 18) and then
	// "yabcdefghijk" (12, dist 16): 6 + 6 literals + two matches = 2 tokens.
	// The optimal parse is one byte shorter than the greedy 24 here.
	if len(comp) > 23 {
		t.Errorf("optimal parse = %d bytes: % x", len(comp), comp)
	}
}
