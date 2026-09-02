package ascon

// CXOFMaxCustom is the customization-string limit set by NIST SP
// 800-232: 2048 bits, so 256 bytes.
const CXOFMaxCustom = 256

// CXOF computes Ascon-CXOF128 over data under the customization
// string z and writes exactly len(out) bytes of hash output into
// out. Any output length is valid. z distinguishes domains: equal
// data hashed under different z is unrelated. len(z) must be at
// most CXOFMaxCustom; CXOF panics otherwise, since callers pass
// fixed protocol strings, not data.
func CXOF(z, data, out []byte) {
	if len(z) > CXOFMaxCustom {
		panic("ascon: customization string longer than 256 bytes")
	}
	// Ascon-CXOF128 IV (NIST SP 800-232):
	//   [version=4, 0, (b<<4)|a=0xCC, taglen=0, rate=8, 0, 0]
	// Read LE: 0x0000080000CC0004.
	var s [5]uint64
	s[0] = 0x0000080000CC0004
	p12(&s)

	// The customization is framed by its own bit length, then
	// absorbed exactly like a message; after that CXOF is XOF.
	s[0] ^= uint64(len(z)) * 8
	p12(&s)
	absorb(&s, z)
	absorb(&s, data)
	squeeze(&s, out)
}
