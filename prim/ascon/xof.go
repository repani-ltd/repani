package ascon

import "encoding/binary"

// XOFSize is a convenience constant: the 16-byte output length
// most callers use (e.g. identifiers and MACs).
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

	// Absorb full 8-byte blocks straight from data; only the tail
	// (0..7 bytes plus 10* padding) goes through a stack buffer, so
	// no heap copy of the (possibly sensitive) input is made.
	const rate = 8
	for len(data) >= rate {
		s[0] ^= binary.LittleEndian.Uint64(data[:rate])
		p12(&s)
		data = data[rate:]
	}
	var buf [rate]byte
	copy(buf[:], data)
	buf[len(data)] = 0x01
	s[0] ^= binary.LittleEndian.Uint64(buf[:])
	p12(&s)

	// Squeeze: 8 bytes of s[0] per p12, little-endian.
	pos := 0
	for pos < len(out) {
		binary.LittleEndian.PutUint64(buf[:], s[0])
		n := min(len(out)-pos, rate)
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
