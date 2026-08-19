package ascon

import (
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"errors"
)

// Ascon-AEAD128 sizes (NIST SP 800-232).
const (
	AEADKeySize   = 16
	AEADNonceSize = 16
	AEADTagSize   = 16
	aeadRate      = 16
)

// Ascon-AEAD128 initialization vector (NIST SP 800-232):
//
//	bytes [version=1, 0, (b<<4)|a=0x8C, taglen_LE=0x80 0x00, rate=0x10, 0, 0]
//	LE uint64: 0x00001000808C0001
const aeadIV = 0x00001000808C0001

var (
	ErrAEADKeySize = errors.New("ascon: AEAD key must be 16 bytes")
	ErrAEADAuth    = errors.New("ascon: AEAD authentication failed")
	// ErrAEADShort is deliberately distinct from ErrAEADAuth (stdlib
	// AEADs fold the two): a too-short ciphertext is a framing error
	// the caller can report before any key is involved, whereas
	// ErrAEADAuth means a well-formed message failed verification.
	ErrAEADShort = errors.New("ascon: AEAD ciphertext shorter than tag")
)

// AEAD implements cipher.AEAD for Ascon-AEAD128.
type AEAD struct {
	k0, k1 uint64
	zeroed bool
}

var _ cipher.AEAD = (*AEAD)(nil)

// NewAEAD returns an Ascon-AEAD128 cipher with the given 16-byte key.
func NewAEAD(key []byte) (*AEAD, error) {
	if len(key) != AEADKeySize {
		return nil, ErrAEADKeySize
	}
	return &AEAD{
		k0: binary.LittleEndian.Uint64(key[0:8]),
		k1: binary.LittleEndian.Uint64(key[8:16]),
	}, nil
}

// NonceSize returns the AEAD nonce size (16 bytes).
func (a *AEAD) NonceSize() int { return AEADNonceSize }

// Overhead returns the AEAD tag size (16 bytes).
func (a *AEAD) Overhead() int { return AEADTagSize }

// Zero wipes the key material held by a. After Zero the AEAD cannot
// produce or verify any ciphertext: Seal and Open panic; callers must
// drop the reference. The wipe is best-effort: the Go runtime offers
// no guarantee that the underlying memory is not copied elsewhere,
// but Zero reduces the window in which a live key sits in a
// long-running process.
func (a *AEAD) Zero() {
	a.k0 = 0
	a.k1 = 0
	a.zeroed = true
}

// Seal encrypts and authenticates plaintext with associated data,
// returning ciphertext||tag appended to dst.
func (a *AEAD) Seal(dst, nonce, plaintext, additionalData []byte) []byte {
	if len(nonce) != AEADNonceSize {
		panic("ascon: AEAD nonce must be 16 bytes")
	}
	a.checkLive()
	ret, out := sliceForAppend(dst, len(plaintext)+AEADTagSize)

	var s [5]uint64
	a.init(&s, nonce)
	a.absorbAD(&s, additionalData)
	a.encrypt(&s, out[:len(plaintext)], plaintext)
	a.finalize(&s, out[len(plaintext):])

	return ret
}

// Open decrypts and authenticates ciphertext (which must include the
// 16-byte trailing tag). On success it returns the plaintext appended
// to dst. On authentication failure it returns ErrAEADAuth and a nil
// slice, and the output region (which in the in-place case overlaps
// the ciphertext) is zeroed; the caller should treat the message as
// untrusted.
func (a *AEAD) Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	if len(nonce) != AEADNonceSize {
		panic("ascon: AEAD nonce must be 16 bytes")
	}
	a.checkLive()
	if len(ciphertext) < AEADTagSize {
		return nil, ErrAEADShort
	}

	ct := ciphertext[:len(ciphertext)-AEADTagSize]
	tag := ciphertext[len(ciphertext)-AEADTagSize:]

	ret, out := sliceForAppend(dst, len(ct))

	var s [5]uint64
	a.init(&s, nonce)
	a.absorbAD(&s, additionalData)
	a.decrypt(&s, out, ct)

	var computed [AEADTagSize]byte
	a.finalize(&s, computed[:])

	if subtle.ConstantTimeCompare(computed[:], tag) != 1 {
		for i := range out {
			out[i] = 0
		}
		return nil, ErrAEADAuth
	}
	return ret, nil
}

// --- internal ---

// checkLive panics if the key has been wiped by Zero.
func (a *AEAD) checkLive() {
	if a.zeroed {
		panic("ascon: AEAD used after Zero")
	}
}

func (a *AEAD) init(s *[5]uint64, nonce []byte) {
	s[0] = aeadIV
	s[1] = a.k0
	s[2] = a.k1
	s[3] = binary.LittleEndian.Uint64(nonce[0:8])
	s[4] = binary.LittleEndian.Uint64(nonce[8:16])
	p12(s)
	s[3] ^= a.k0
	s[4] ^= a.k1
}

func (a *AEAD) absorbAD(s *[5]uint64, ad []byte) {
	if len(ad) > 0 {
		for len(ad) >= aeadRate {
			s[0] ^= binary.LittleEndian.Uint64(ad[0:8])
			s[1] ^= binary.LittleEndian.Uint64(ad[8:16])
			p8(s)
			ad = ad[aeadRate:]
		}
		var buf [aeadRate]byte
		copy(buf[:], ad)
		buf[len(ad)] = 0x01
		s[0] ^= binary.LittleEndian.Uint64(buf[0:8])
		s[1] ^= binary.LittleEndian.Uint64(buf[8:16])
		p8(s)
	}
	// Domain separation between AD and plaintext.
	s[4] ^= 0x8000000000000000
}

func (a *AEAD) encrypt(s *[5]uint64, dst, src []byte) {
	for len(src) >= aeadRate {
		s[0] ^= binary.LittleEndian.Uint64(src[0:8])
		s[1] ^= binary.LittleEndian.Uint64(src[8:16])
		binary.LittleEndian.PutUint64(dst[0:8], s[0])
		binary.LittleEndian.PutUint64(dst[8:16], s[1])
		p8(s)
		src = src[aeadRate:]
		dst = dst[aeadRate:]
	}
	var buf [aeadRate]byte
	copy(buf[:], src)
	buf[len(src)] = 0x01
	s[0] ^= binary.LittleEndian.Uint64(buf[0:8])
	s[1] ^= binary.LittleEndian.Uint64(buf[8:16])
	binary.LittleEndian.PutUint64(buf[0:8], s[0])
	binary.LittleEndian.PutUint64(buf[8:16], s[1])
	copy(dst, buf[:len(src)])
}

func (a *AEAD) decrypt(s *[5]uint64, dst, src []byte) {
	for len(src) >= aeadRate {
		c0 := binary.LittleEndian.Uint64(src[0:8])
		c1 := binary.LittleEndian.Uint64(src[8:16])
		binary.LittleEndian.PutUint64(dst[0:8], s[0]^c0)
		binary.LittleEndian.PutUint64(dst[8:16], s[1]^c1)
		s[0] = c0
		s[1] = c1
		p8(s)
		src = src[aeadRate:]
		dst = dst[aeadRate:]
	}
	t := len(src)
	var keystream [aeadRate]byte
	binary.LittleEndian.PutUint64(keystream[0:8], s[0])
	binary.LittleEndian.PutUint64(keystream[8:16], s[1])
	for i := range t {
		dst[i] = keystream[i] ^ src[i]
	}
	var pad [aeadRate]byte
	copy(pad[:t], dst[:t])
	pad[t] = 0x01
	s[0] ^= binary.LittleEndian.Uint64(pad[0:8])
	s[1] ^= binary.LittleEndian.Uint64(pad[8:16])
}

func (a *AEAD) finalize(s *[5]uint64, tag []byte) {
	s[2] ^= a.k0
	s[3] ^= a.k1
	p12(s)
	s[3] ^= a.k0
	s[4] ^= a.k1
	binary.LittleEndian.PutUint64(tag[0:8], s[3])
	binary.LittleEndian.PutUint64(tag[8:16], s[4])
}

func sliceForAppend(in []byte, n int) (head, tail []byte) {
	if total := len(in) + n; cap(in) >= total {
		head = in[:total]
	} else {
		head = make([]byte, total)
		copy(head, in)
	}
	tail = head[len(in):]
	return
}
