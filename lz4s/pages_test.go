package lz4s

import (
	"bytes"
	"os"
	"testing"
)

// Real raster pages (typeset/raster bytes: six of a 48 by 20 app,
// five of tessera's 34 by 28 by 4) round-trip, and their compressed
// sizes are the parser's known answers: a change here is a change of
// the canonical encoding. An optimal parser measured 2026-09-05 would
// take two percent off these (~/repos/research/lz4s-lab/FINDINGS.t);
// it was not admitted.
func TestPages(t *testing.T) {
	want := map[string]int{
		"qam-home": 574, "qam-report": 266, "qam-report2": 267, "qam-search": 401, "qam-trend": 246, "qam-near": 343,
		"tess-aegean": 1923, "tess-harbour": 1086, "tess-harbour2": 1077, "tess-gallery": 1115, "tess-features": 1554,
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
