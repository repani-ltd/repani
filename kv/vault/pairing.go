package vault

import (
	"encoding/base32"
	"errors"
	"strings"
)

// Key blobs (CLIENT.t, Key blobs): keys as Crockford base32 text,
// upper case, no padding. A RootKey is 32 characters, a RegisterKey
// 26 (its last 2 bits zero), a backup the two run together (58).

var crockford = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// ErrBadPairingBlob is returned when a key blob is malformed.
var ErrBadPairingBlob = errors.New("vault: malformed pairing blob")

// normalize drops what a blob may be shown with (whitespace, dashes)
// and reads it as Crockford does: either case, O as 0, I and L as 1.
func normalize(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '-':
			return -1
		case 'O', 'o':
			return '0'
		case 'I', 'i', 'L', 'l':
			return '1'
		}
		if 'a' <= r && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}, s)
}

// decode parses a normalized blob of exactly n bytes. Re-encoding must
// give the blob back, which rejects non-zero padding bits.
func decode(raw string, n int) ([]byte, error) {
	if len(raw) != crockford.EncodedLen(n) {
		return nil, ErrBadPairingBlob
	}
	b, err := crockford.DecodeString(raw)
	if err != nil || len(b) != n || crockford.EncodeToString(b) != raw {
		return nil, ErrBadPairingBlob
	}
	return b, nil
}

// EncodeRootKey returns the 32-character blob of rk.
func EncodeRootKey(rk RootKey) string { return crockford.EncodeToString(rk[:]) }

// EncodeRegisterKey returns the 26-character blob of rk.
func EncodeRegisterKey(rk RegisterKey) string { return crockford.EncodeToString(rk[:]) }

// EncodeBackup returns the blob of rk alone (32 characters), or of rk
// and regKey together (58) when regKey is not nil.
func EncodeBackup(rk RootKey, regKey *RegisterKey) string {
	if regKey == nil {
		return EncodeRootKey(rk)
	}
	return EncodeRootKey(rk) + EncodeRegisterKey(*regKey)
}

// DecodeRootKey parses a RootKey blob.
func DecodeRootKey(s string) (RootKey, error) {
	b, err := decode(normalize(s), RootKeySize)
	if err != nil {
		return RootKey{}, err
	}
	return RootKey(b), nil
}

// DecodeRegisterKey parses a RegisterKey blob.
func DecodeRegisterKey(s string) (RegisterKey, error) {
	b, err := decode(normalize(s), RegisterKeySize)
	if err != nil {
		return RegisterKey{}, err
	}
	return RegisterKey(b), nil
}

// DecodeBackup parses either backup shape. The RegisterKey is nil when
// the blob holds a RootKey alone.
func DecodeBackup(s string) (RootKey, *RegisterKey, error) {
	raw := normalize(s)
	rootLen := crockford.EncodedLen(RootKeySize)
	switch len(raw) {
	case rootLen:
		rk, err := DecodeRootKey(raw)
		return rk, nil, err
	case rootLen + crockford.EncodedLen(RegisterKeySize):
		rk, err := DecodeRootKey(raw[:rootLen])
		if err != nil {
			return RootKey{}, nil, err
		}
		reg, err := DecodeRegisterKey(raw[rootLen:])
		if err != nil {
			return RootKey{}, nil, err
		}
		return rk, &reg, nil
	default:
		return RootKey{}, nil, ErrBadPairingBlob
	}
}
