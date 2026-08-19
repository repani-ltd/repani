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

// seq returns n bytes: 0, 1, 2, ... (mod 256). No 3-gram repeats
// within the first 256 bytes.
func seq(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// cat concatenates its arguments into a fresh slice.
func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestKnownAnswers pins the compressed byte format. Every expected
// stream was written out by hand from the grammar in the package
// doc (token [lit-ext] literals [offset [match-ext]]), not by
// running Compress; each case is also decompressed back to src.
func TestKnownAnswers(t *testing.T) {
	// filler253 is 0x00..0xFC: no 3-gram repeats, so no match forms
	// inside it.
	filler253 := seq(253)
	// tail16 is 15, 14, ..., 0: descending, so it shares no 3-gram
	// with an ascending prefix.
	tail16 := make([]byte, 16)
	for i := range tail16 {
		tail16[i] = byte(15 - i)
	}
	x272 := cat(seq(256), tail16)
	alpha := []byte("abcdefghijklmnopqrstuvwxyz")

	cases := []struct {
		name string
		src  []byte
		comp []byte
	}{
		{"empty", nil, nil},
		// L=3, M=0: one token, three literals.
		{"lits", []byte("abc"), []byte{0x30, 'a', 'b', 'c'}},
		// "abc" then match len 3 (M=1) at dist 3 (offset 2).
		{"near3", []byte("abcabc"), []byte{0x31, 'a', 'b', 'c', 0x02}},
		// Exactly 7 literals: L=7 with a zero extension byte.
		{"lit7", []byte("abcdefg"), cat([]byte{0x70, 0x00}, []byte("abcdefg"))},
		// 8 literals: L=7, ext 1.
		{"lit8", []byte("abcdefgh"), cat([]byte{0x70, 0x01}, []byte("abcdefgh"))},
		// 262 literals: L=7, ext 255 = 0xFF 0x00.
		{"lit262", cat(seq(256), []byte{5, 4, 3, 2, 1, 0}),
			cat([]byte{0x70, 0xFF, 0x00}, seq(256), []byte{5, 4, 3, 2, 1, 0})},
		// 16-byte match (M=14, no match-ext) at dist 16 after 16 literals.
		{"match16", cat(alpha[:16], alpha[:16]),
			cat([]byte{0x7E, 0x09}, alpha[:16], []byte{0x0F})},
		// 17-byte match: M=15 with a zero match-ext byte.
		{"match17", cat(alpha[:17], alpha[:17]),
			cat([]byte{0x7F, 0x0A}, alpha[:17], []byte{0x10, 0x00})},
		// 26-byte match: M=15, match-ext 9.
		{"match26", cat(alpha, alpha),
			cat([]byte{0x7F, 0x13}, alpha, []byte{0x19, 0x09})},
		// 272-byte match at dist 272: W=1, offset 271 = 0x010F LE,
		// lit-ext 265 = 0xFF 0x0A, match-ext 255 = 0xFF 0x00.
		{"match272wide", cat(x272, x272),
			cat([]byte{0xFF, 0xFF, 0x0A}, x272, []byte{0x0F, 0x01, 0xFF, 0x00})},
		// Marker FD FE FF, 253 filler bytes, marker again: match len 3
		// at dist 256, the widest near offset (0xFF), after 256
		// literals (lit-ext 249).
		{"near256", cat([]byte{0xFD, 0xFE, 0xFF}, filler253, []byte{0xFD, 0xFE, 0xFF}),
			cat([]byte{0x71, 0xF9, 0xFD, 0xFE, 0xFF}, filler253, []byte{0xFF})},
		// Marker FC FD FE FF, 253 filler bytes, marker again: match
		// len 4 (M=2) at dist 257, the narrowest wide offset
		// (0x0100 LE), after 257 literals (lit-ext 250). A wide
		// 3-byte match would gain nothing and stay literal.
		{"wide257", cat([]byte{0xFC, 0xFD, 0xFE, 0xFF}, filler253, []byte{0xFC, 0xFD, 0xFE, 0xFF}),
			cat([]byte{0xF2, 0xFA, 0xFC, 0xFD, 0xFE, 0xFF}, filler253, []byte{0x00, 0x01})},
	}
	for _, c := range cases {
		if got := Compress(c.src); !bytes.Equal(got, c.comp) {
			t.Errorf("%s: Compress\n got  % x\n want % x", c.name, got, c.comp)
		}
		got, ok := Decompress(c.comp, len(c.src))
		if !ok || !bytes.Equal(got, c.src) {
			t.Errorf("%s: Decompress ok=%v got %d bytes, want %d", c.name, ok, len(got), len(c.src))
		}
	}
}

// TestDecompressNegativeSize checks that a negative dsize is
// rejected rather than panicking in make.
func TestDecompressNegativeSize(t *testing.T) {
	if _, ok := Decompress(nil, -1); ok {
		t.Fatal("negative dsize accepted")
	}
	if _, ok := Decompress([]byte{0x10, 'a'}, -1); ok {
		t.Fatal("negative dsize accepted")
	}
}

// TestDecompressNoOpTokens pins the contract that tokens producing
// no output (L=0, M=0) are accepted anywhere, including as trailing
// bytes: the only framing check is that exactly dsize bytes result.
func TestDecompressNoOpTokens(t *testing.T) {
	comp := []byte{0x00, 0x80, 0x30, 'a', 'b', 'c', 0x00, 0x01, 0x02, 0x00, 0x00}
	got, ok := Decompress(comp, 6)
	if !ok || string(got) != "abcabc" {
		t.Fatalf("no-op tokens: ok=%v got %q", ok, got)
	}
	if got, ok := Decompress([]byte{0x00, 0x00}, 0); !ok || len(got) != 0 {
		t.Fatalf("empty stream of no-ops: ok=%v got %q", ok, got)
	}
	// A stream of no-ops still cannot stand in for missing output.
	if _, ok := Decompress([]byte{0x00}, 1); ok {
		t.Fatal("no-op token accepted as output")
	}
}
