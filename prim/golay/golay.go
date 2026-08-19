// Package golay implements the extended Golay(24,12) code: a
// systematic encoder and a syndrome-table decoder that corrects
// up to 3 bit errors per codeword and detects 4.
package golay

import "math/bits"

// gen is g(x) = x^11 + x^10 + x^6 + x^5 + x^4 + x^2 + 1.
const gen = 0xC75

// golaySyn maps an 11-bit syndrome to its unique error pattern
// of weight <= 3 over 23 bits; the (23,12) code is perfect, so
// the 2048 syndromes and patterns are in bijection.
var golaySyn [2048]uint32

// syndrome reduces a 23-bit word modulo gen.
func syndrome(r uint32) uint32 {
	for i := 22; i >= 11; i-- {
		if r&(1<<uint(i)) != 0 {
			r ^= gen << uint(i-11)
		}
	}
	return r
}

func init() {
	// set records a pattern under its syndrome and counts the
	// slot only the first time it is filled, so a generator that
	// collides two patterns leaves filled short of 2048.
	filled := 1 // syndrome 0 -> pattern 0
	set := func(e uint32) {
		s := syndrome(e)
		if golaySyn[s] == 0 {
			filled++
		}
		golaySyn[s] = e
	}
	for a := 0; a < 23; a++ {
		ea := uint32(1) << a
		set(ea)
		for b := a + 1; b < 23; b++ {
			eb := ea | 1<<b
			set(eb)
			for c := b + 1; c < 23; c++ {
				set(eb | 1<<c)
			}
		}
	}
	if filled != 2048 {
		panic("golay: syndrome table build failed")
	}
}

// Encode returns the 24-bit extended codeword for 12 data
// bits: data in bits 23-12, check bits in 11-1, overall even
// parity in bit 0. Transmitted bit i is (cw >> (23-i)) & 1.
func Encode(data uint16) uint32 {
	v := uint32(data&0xFFF) << 11
	cw23 := v | syndrome(v)
	return cw23<<1 | uint32(bits.OnesCount32(cw23))&1
}

// Decode decodes a 24-bit received word; bits above 23 are
// ignored, as Encode ignores data bits above 11. data is the
// best-effort 12 data bits regardless of ok; ok reports whether
// the word decoded within radius 3 (up to 3 bit errors
// corrected, 4 detected).
func Decode(cw uint32) (data uint16, ok bool) {
	cw &= 0xFFFFFF
	r23 := cw >> 1
	e := golaySyn[syndrome(r23)]
	c23 := r23 ^ e
	t := bits.OnesCount32(e)
	if uint32(bits.OnesCount32(c23))&1 != cw&1 {
		t++
	}
	return uint16(c23 >> 11), t <= 3
}
