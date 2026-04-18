package vault

import (
	"errors"
	"strings"
)

// Pairing blob: base32(RootKey), 52 Crockford base32 characters.

// Crockford base32 alphabet (uppercase canonical form).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// crockfordDec maps input character (upper or lower) to its 5-bit
// value, or -1 if unknown. O/o → 0, I/i/L/l → 1 by Crockford
// convention.
var crockfordDec [256]int8

func init() {
	for i := range crockfordDec {
		crockfordDec[i] = -1
	}
	for i, c := range crockford {
		crockfordDec[byte(c)] = int8(i)
		crockfordDec[byte(strings.ToLower(string(c))[0])] = int8(i)
	}
	// Crockford aliases.
	crockfordDec['O'] = 0
	crockfordDec['o'] = 0
	crockfordDec['I'] = 1
	crockfordDec['i'] = 1
	crockfordDec['L'] = 1
	crockfordDec['l'] = 1
}

// ErrBadPairingBlob is returned when DecodeRootKey is given malformed input.
var ErrBadPairingBlob = errors.New("vault: malformed pairing blob")

// EncodeRootKey returns the Crockford base32 encoding of rk (52 chars).
// 32 bytes → 256 bits → 52 base32 chars with 4 trailing padding bits
// that are truncated.
func EncodeRootKey(rk RootKey) string {
	// Each 5 bytes → 8 chars. 32 bytes = 6 full 5-byte groups + 2
	// remainder bytes. Total output: 6*8 + ceil(2*8/5) = 48 + 4 = 52.
	var out strings.Builder
	out.Grow(52)
	// Full 5-byte groups.
	for g := range 6 {
		b := rk[g*5 : g*5+5]
		v := uint64(b[0])<<32 | uint64(b[1])<<24 | uint64(b[2])<<16 | uint64(b[3])<<8 | uint64(b[4])
		for i := 7; i >= 0; i-- {
			out.WriteByte(crockford[(v>>(uint(i)*5))&0x1f])
		}
	}
	// Trailing 2 bytes = 16 bits, encoded as 4 chars (4 bits of
	// padding at the low end, dropped by the 5-bit slicing).
	tail := rk[30:32]
	v := uint32(tail[0])<<8 | uint32(tail[1])
	// Shift up so the top nibble becomes the first 5-bit chunk.
	v <<= 4 // pad to 20 bits
	for i := 3; i >= 0; i-- {
		out.WriteByte(crockford[(v>>(uint(i)*5))&0x1f])
	}
	return out.String()
}

// DecodeRootKey parses the 52-char Crockford base32 form produced by
// EncodeRootKey. Whitespace and dashes in the input are ignored.
func DecodeRootKey(s string) (RootKey, error) {
	// Strip formatting.
	raw := make([]byte, 0, len(s))
	for i := range len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '-' {
			continue
		}
		raw = append(raw, c)
	}
	if len(raw) != 52 {
		return RootKey{}, ErrBadPairingBlob
	}
	// First 48 chars → 30 bytes (6 full 5-byte groups).
	var rk RootKey
	for g := range 6 {
		var v uint64
		for i := range 8 {
			d := crockfordDec[raw[g*8+i]]
			if d < 0 {
				return RootKey{}, ErrBadPairingBlob
			}
			v = v<<5 | uint64(d)
		}
		rk[g*5+0] = byte(v >> 32)
		rk[g*5+1] = byte(v >> 24)
		rk[g*5+2] = byte(v >> 16)
		rk[g*5+3] = byte(v >> 8)
		rk[g*5+4] = byte(v)
	}
	// Last 4 chars → 2 bytes (plus 4 bits of padding).
	var v uint32
	for i := range 4 {
		d := crockfordDec[raw[48+i]]
		if d < 0 {
			return RootKey{}, ErrBadPairingBlob
		}
		v = v<<5 | uint32(d)
	}
	// Low 4 bits are padding; validate they are zero.
	if v&0xF != 0 {
		return RootKey{}, ErrBadPairingBlob
	}
	v >>= 4
	rk[30] = byte(v >> 8)
	rk[31] = byte(v)
	return rk, nil
}
