// Package ascon implements Ascon-XOF128 (extendable-output
// function) and Ascon-AEAD128 (authenticated encryption with
// associated data), both as specified in NIST SP 800-232.
package ascon

import "math/bits"

// p12 applies the standard 12-round Ascon-p permutation.
func p12(s *[5]uint64) {
	for i := range 12 {
		round(s, i)
	}
}

// p8 applies the 8-round Ascon-p permutation (rounds 4..11).
func p8(s *[5]uint64) {
	for i := 4; i < 12; i++ {
		round(s, i)
	}
}

// round is one round of the Ascon-p permutation.
func round(s *[5]uint64, i int) {
	// Constant addition.
	s[2] ^= uint64(0xf0 - i*0x10 + i)
	// Substitution (S-box).
	s[0] ^= s[4]
	s[4] ^= s[3]
	s[2] ^= s[1]
	t0 := ^s[0] & s[1]
	t1 := ^s[1] & s[2]
	t2 := ^s[2] & s[3]
	t3 := ^s[3] & s[4]
	t4 := ^s[4] & s[0]
	s[0] ^= t1
	s[1] ^= t2
	s[2] ^= t3
	s[3] ^= t4
	s[4] ^= t0
	s[1] ^= s[0]
	s[0] ^= s[4]
	s[3] ^= s[2]
	s[2] = ^s[2]
	// Linear diffusion.
	s[0] ^= bits.RotateLeft64(s[0], -19) ^ bits.RotateLeft64(s[0], -28)
	s[1] ^= bits.RotateLeft64(s[1], -61) ^ bits.RotateLeft64(s[1], -39)
	s[2] ^= bits.RotateLeft64(s[2], -1) ^ bits.RotateLeft64(s[2], -6)
	s[3] ^= bits.RotateLeft64(s[3], -10) ^ bits.RotateLeft64(s[3], -17)
	s[4] ^= bits.RotateLeft64(s[4], -7) ^ bits.RotateLeft64(s[4], -41)
}
