package vault

import (
	"errors"
	"strings"
)

// Pairing blob: base32(RootKey), 32 Crockford base32 characters
// (displayed as 8 groups of 4).

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

// strip removes formatting (whitespace and dashes) from a blob, leaving
// just the base32 characters.
func strip(s string) []byte {
	raw := make([]byte, 0, len(s))
	for i := range len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '-' {
			continue
		}
		raw = append(raw, c)
	}
	return raw
}

// encodeGroup appends the 8 Crockford chars of one 5-byte group to out.
func encodeGroup(out *strings.Builder, b []byte) {
	v := uint64(b[0])<<32 | uint64(b[1])<<24 | uint64(b[2])<<16 | uint64(b[3])<<8 | uint64(b[4])
	for i := 7; i >= 0; i-- {
		out.WriteByte(crockford[(v>>(uint(i)*5))&0x1f])
	}
}

// decodeGroup decodes 8 Crockford chars into the 5-byte dst.
func decodeGroup(chars, dst []byte) error {
	var v uint64
	for i := range 8 {
		d := crockfordDec[chars[i]]
		if d < 0 {
			return ErrBadPairingBlob
		}
		v = v<<5 | uint64(d)
	}
	dst[0] = byte(v >> 32)
	dst[1] = byte(v >> 24)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 8)
	dst[4] = byte(v)
	return nil
}

// EncodeRootKey returns the Crockford base32 encoding of rk (32
// chars). 20 bytes = exactly four 5-byte groups of 8 chars each, so
// there are no padding bits.
func EncodeRootKey(rk RootKey) string {
	var out strings.Builder
	out.Grow(32)
	for g := range 4 {
		encodeGroup(&out, rk[g*5:g*5+5])
	}
	return out.String()
}

// EncodeRegisterKey returns the Crockford base32 encoding of a
// RegisterKey (16 bytes → 26 chars; 2 trailing bits are padding
// and truncated on encode / validated on decode).
func EncodeRegisterKey(rk RegisterKey) string {
	var out strings.Builder
	out.Grow(26)
	// Three full 5-byte groups → 24 chars.
	for g := range 3 {
		encodeGroup(&out, rk[g*5:g*5+5])
	}
	// Trailing 1 byte = 8 bits, encoded as 2 chars (2 bits padding).
	v := uint32(rk[15]) << 2 // pad up to 10 bits so the first 5-bit chunk is MSB-aligned
	for i := 1; i >= 0; i-- {
		out.WriteByte(crockford[(v>>(uint(i)*5))&0x1f])
	}
	return out.String()
}

// EncodeBackup packs a RootKey and optional RegisterKey into a single
// Crockford base32 blob for offline backup / restore.
//
//	regKey == nil → 32 chars (RootKey alone; identical to EncodeRootKey).
//	regKey != nil → 58 chars (EncodeRootKey || EncodeRegisterKey).
//
// DecodeBackup accepts either shape.
func EncodeBackup(rk RootKey, regKey *RegisterKey) string {
	if regKey == nil {
		return EncodeRootKey(rk)
	}
	return EncodeRootKey(rk) + EncodeRegisterKey(*regKey)
}

// DecodeBackup parses a blob produced by EncodeBackup or EncodeRootKey.
// Whitespace and dashes in the input are ignored. Returns the RootKey
// and a non-nil RegisterKey pointer only if the blob included one
// (i.e. it was 58 chars after stripping).
func DecodeBackup(s string) (RootKey, *RegisterKey, error) {
	raw := strip(s)
	switch len(raw) {
	case 32:
		rk, err := DecodeRootKey(string(raw))
		if err != nil {
			return RootKey{}, nil, err
		}
		return rk, nil, nil
	case 58:
		rk, err := DecodeRootKey(string(raw[:32]))
		if err != nil {
			return RootKey{}, nil, err
		}
		reg, err := DecodeRegisterKey(string(raw[32:]))
		if err != nil {
			return RootKey{}, nil, err
		}
		return rk, &reg, nil
	default:
		return RootKey{}, nil, ErrBadPairingBlob
	}
}

// DecodeRegisterKey parses the 26-char Crockford base32 form produced
// by EncodeRegisterKey. Whitespace and dashes are ignored.
func DecodeRegisterKey(s string) (RegisterKey, error) {
	raw := strip(s)
	if len(raw) != 26 {
		return RegisterKey{}, ErrBadPairingBlob
	}
	var rk RegisterKey
	// First 24 chars → 15 bytes.
	for g := range 3 {
		if err := decodeGroup(raw[g*8:g*8+8], rk[g*5:g*5+5]); err != nil {
			return RegisterKey{}, err
		}
	}
	// Last 2 chars → 1 byte (plus 2 bits of padding).
	var v uint32
	for i := range 2 {
		d := crockfordDec[raw[24+i]]
		if d < 0 {
			return RegisterKey{}, ErrBadPairingBlob
		}
		v = v<<5 | uint32(d)
	}
	if v&0x3 != 0 {
		return RegisterKey{}, ErrBadPairingBlob
	}
	rk[15] = byte(v >> 2)
	return rk, nil
}

// DecodeRootKey parses the 32-char Crockford base32 form produced by
// EncodeRootKey. Whitespace and dashes in the input are ignored.
func DecodeRootKey(s string) (RootKey, error) {
	raw := strip(s)
	if len(raw) != 32 {
		return RootKey{}, ErrBadPairingBlob
	}
	// 32 chars → 20 bytes (4 full 5-byte groups, no padding bits).
	var rk RootKey
	for g := range 4 {
		if err := decodeGroup(raw[g*8:g*8+8], rk[g*5:g*5+5]); err != nil {
			return RootKey{}, err
		}
	}
	return rk, nil
}
