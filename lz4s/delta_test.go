package lz4s

import (
	"bytes"
	"testing"
)

// Delta against the previous version of an input: the known answer of
// an edited text, and the contract's edges.
func TestDelta(t *testing.T) {
	in := benchInputs()
	base, src := in["text"], edited(in["text"])
	d := Delta(base, src)
	if got, ok := Undelta(base, d, len(src)); !ok || !bytes.Equal(got, src) {
		t.Fatal("round trip failed")
	}
	if want := 40; len(d) != want {
		t.Errorf("edited text against text: %d bytes, want %d", len(d), want)
	}
	// The wrong base is not detected by the decoder: it decodes to
	// the right length and the wrong bytes, or fails. The caller
	// names the base.
	if got, ok := Undelta(in["random"], d, len(src)); ok && bytes.Equal(got, src) {
		t.Error("decoded correctly against the wrong base")
	}
	if _, ok := Undelta(base, d, len(src)-1); ok {
		t.Error("accepted the wrong size")
	}
	// An empty base is Compress and Decompress exactly.
	src = []byte("abcabcabcdefdefdef the quick brown fox")
	if !bytes.Equal(Delta(nil, src), Compress(src)) {
		t.Error("Delta(nil) differs from Compress")
	}
	if got, ok := Undelta(nil, Compress(src), len(src)); !ok || !bytes.Equal(got, src) {
		t.Error("Undelta(nil) differs from Decompress")
	}
	// An input delta'd against itself is a single match.
	if d := Delta(src, src); len(d) > 5 {
		t.Errorf("self delta = % x", d)
	}
}
