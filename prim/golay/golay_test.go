package golay

import (
	"math/bits"
	"math/rand"
	"testing"
)

func TestGolayRoundTrip(t *testing.T) {
	for d := 0; d < 4096; d++ {
		cw := Encode(uint16(d))
		if bits.OnesCount32(cw)&1 != 0 {
			t.Fatalf("codeword %#x has odd weight", cw)
		}
		got, ok := Decode(cw)
		if !ok || got != uint16(d) {
			t.Fatalf("clean decode of %#x: got %#x ok=%v", d, got, ok)
		}
	}
}

func TestGolayMinDistance(t *testing.T) {
	// Spot-check d = 8 against the all-zero codeword.
	for d := 1; d < 4096; d++ {
		if w := bits.OnesCount32(Encode(uint16(d))); w < 8 {
			t.Fatalf("codeword for %#x has weight %d < 8", d, w)
		}
	}
}

// TestGolayCorrect3 checks every error pattern of weight <= 3
// over 24 bits against a sample of codewords.
func TestGolayCorrect3(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 32; trial++ {
		d := uint16(rng.Intn(4096))
		cw := Encode(d)
		for a := 0; a < 24; a++ {
			for b := a; b < 24; b++ {
				for c := b; c < 24; c++ {
					e := uint32(1)<<a | 1<<b | 1<<c
					got, ok := Decode(cw ^ e)
					if !ok || got != d {
						t.Fatalf("data %#x error %#06x: got %#x ok=%v", d, e, got, ok)
					}
				}
			}
		}
	}
}

// TestGolayDetect4 checks that every weight-4 error pattern is
// detected, never miscorrected, on a sample of codewords.
func TestGolayDetect4(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for trial := 0; trial < 8; trial++ {
		d := uint16(rng.Intn(4096))
		cw := Encode(d)
		for a := 0; a < 24; a++ {
			for b := a + 1; b < 24; b++ {
				for c := b + 1; c < 24; c++ {
					for e4 := c + 1; e4 < 24; e4++ {
						e := uint32(1)<<a | 1<<b | 1<<c | 1<<e4
						if _, ok := Decode(cw ^ e); ok {
							t.Fatalf("data %#x error %#06x: weight-4 accepted", d, e)
						}
					}
				}
			}
		}
	}
}

// TestGolayDecodeHighBits checks that bits above 23 are ignored
// rather than indexing the syndrome table out of range.
func TestGolayDecodeHighBits(t *testing.T) {
	cw := Encode(0x5A5)
	got, ok := Decode(cw | 1<<24 | 1<<31)
	if !ok || got != 0x5A5 {
		t.Fatalf("Decode with high bits set: got %#x ok=%v", got, ok)
	}
}

// TestGolaySyndromeTable checks the table is a bijection: every
// entry round-trips through syndrome and has weight <= 3.
func TestGolaySyndromeTable(t *testing.T) {
	for s, e := range golaySyn {
		if s == 0 {
			if e != 0 {
				t.Fatalf("syndrome 0 maps to %#x", e)
			}
			continue
		}
		if e == 0 || bits.OnesCount32(e) > 3 || syndrome(e) != uint32(s) {
			t.Fatalf("syndrome %#x: bad pattern %#x", s, e)
		}
	}
}
