// Package ascon implements Ascon-XOF128 (extendable-output
// function) and Ascon-AEAD128 (authenticated encryption with
// associated data), both as specified in NIST SP 800-232.
package ascon

import (
	"encoding/binary"
	"math/bits"
)

// XOFSize is a convenience constant: the 16-byte output length
// most callers use (e.g. ASCON-KV vault ids, MACs, entry ids).
const XOFSize = 16

// XOF computes Ascon-XOF128 over data and writes exactly len(out)
// bytes of hash output into out. Any output length is valid.
func XOF(data, out []byte) {
	// Ascon-XOF128 IV (NIST SP 800-232):
	//   [version=3, 0, (b<<4)|a=0xCC, taglen=0, rate=8, 0, 0]
	// Read LE: 0x0000080000CC0003.
	var s [5]uint64
	s[0] = 0x0000080000CC0003

	p12(&s)

	// Absorb: pad message (10* padding) into 8-byte blocks.
	const rate = 8
	padded := make([]byte, 0, len(data)+rate)
	padded = append(padded, data...)
	padded = append(padded, 0x01)
	for len(padded)%rate != 0 {
		padded = append(padded, 0x00)
	}
	for off := 0; off < len(padded); off += rate {
		s[0] ^= binary.LittleEndian.Uint64(padded[off : off+rate])
		p12(&s)
	}
	// Scrub the heap copy of the (possibly sensitive) input. Best-
	// effort: the compiler may elide this under optimization, but it
	// materially reduces lingering RootKey/AuthKey bytes in MAC and
	// key-derivation paths.
	for i := range padded {
		padded[i] = 0
	}

	// Squeeze: 8 bytes of s[0] per p12, little-endian.
	pos := 0
	for pos < len(out) {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], s[0])
		n := min(len(out)-pos, 8)
		copy(out[pos:pos+n], buf[:n])
		pos += n
		if pos < len(out) {
			p12(&s)
		}
	}
}

// Sum16 is a convenience wrapper that returns a 16-byte Ascon-XOF128
// hash of data. Equivalent to XOF(data, out[:16]).
func Sum16(data []byte) [16]byte {
	var out [16]byte
	XOF(data, out[:])
	return out
}

// Sum32 returns a 32-byte Ascon-XOF128 hash of data.
func Sum32(data []byte) [32]byte {
	var out [32]byte
	XOF(data, out[:])
	return out
}

// --- permutation ---

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
