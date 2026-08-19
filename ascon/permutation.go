// Package ascon implements Ascon-XOF128 (extendable-output
// function) and Ascon-AEAD128 (authenticated encryption with
// associated data), both as specified in NIST SP 800-232.
package ascon

import "math/bits"

// p12 applies the standard 12-round Ascon-p permutation.
func p12(s *[5]uint64) { perm(s, 0) }

// p8 applies the 8-round Ascon-p permutation (rounds 4..11).
func p8(s *[5]uint64) { perm(s, 4) }

// perm applies rounds from..11 of the Ascon-p permutation. The state
// is held in registers for the whole loop and stored back once.
func perm(s *[5]uint64, from int) {
	x0, x1, x2, x3, x4 := s[0], s[1], s[2], s[3], s[4]
	for i := from; i < 12; i++ {
		// Constant addition.
		x2 ^= uint64(0xf0 - i*0x10 + i)
		// Substitution (S-box).
		x0 ^= x4
		x4 ^= x3
		x2 ^= x1
		t0 := ^x0 & x1
		t1 := ^x1 & x2
		t2 := ^x2 & x3
		t3 := ^x3 & x4
		t4 := ^x4 & x0
		x0 ^= t1
		x1 ^= t2
		x2 ^= t3
		x3 ^= t4
		x4 ^= t0
		x1 ^= x0
		x0 ^= x4
		x3 ^= x2
		x2 = ^x2
		// Linear diffusion.
		x0 ^= bits.RotateLeft64(x0, -19) ^ bits.RotateLeft64(x0, -28)
		x1 ^= bits.RotateLeft64(x1, -61) ^ bits.RotateLeft64(x1, -39)
		x2 ^= bits.RotateLeft64(x2, -1) ^ bits.RotateLeft64(x2, -6)
		x3 ^= bits.RotateLeft64(x3, -10) ^ bits.RotateLeft64(x3, -17)
		x4 ^= bits.RotateLeft64(x4, -7) ^ bits.RotateLeft64(x4, -41)
	}
	s[0], s[1], s[2], s[3], s[4] = x0, x1, x2, x3, x4
}
