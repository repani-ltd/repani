package vault

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/pavlos/kv/ascon"
)

// Entry header size: ts_ms(8) + nonce(16) + ct_len(4) = 28 bytes.
const EntryHeaderSize = 8 + NonceSize + 4

// MaxCtBytes caps the ciphertext length to a value that cannot
// overflow int on 32-bit platforms and keeps a single entry well
// below the 64 MiB server body cap. Plaintext room: 2 (key_len) +
// 65535 (max key) + 4 (value_len) + ~16 MiB value + 16 (tag) ≈ 16 MiB,
// so 32 MiB is generous.
const MaxCtBytes = 32 << 20

// Errors surfaced by entry handling.
var (
	ErrEntryShort    = errors.New("vault: entry bytes shorter than header")
	ErrEntryCorrupt  = errors.New("vault: entry ciphertext length mismatch")
	ErrEntryTooLarge = errors.New("vault: entry ciphertext length exceeds MaxCtBytes")
	ErrKeyTooLong    = errors.New("vault: plaintext key exceeds 65535 bytes")
	ErrValueTooLong  = errors.New("vault: plaintext value exceeds 2^32-1 bytes")
	ErrPlaintextBad  = errors.New("vault: decrypted plaintext is malformed")
	ErrEmptyKey      = errors.New("vault: key must be non-empty")
)

// Entry is the canonical unit of storage: a self-contained, encrypted
// record. The layout on disk and on the wire is:
//
//	| ts_ms (8) | nonce (16) | ct_len (4) | ciphertext |
//
// All multi-byte integers are little-endian. The ciphertext includes
// the trailing 16-byte Ascon-AEAD128 tag.
type Entry struct {
	TsMs  uint64
	Nonce [NonceSize]byte
	Ct    []byte // ciphertext || tag
}

// Size returns the marshaled length of e.
func (e Entry) Size() int {
	return EntryHeaderSize + len(e.Ct)
}

// Marshal serializes e into a fresh byte slice.
func (e Entry) Marshal() []byte {
	out := make([]byte, e.Size())
	e.MarshalTo(out)
	return out
}

// MarshalTo writes e's bytes into buf (which must be at least Size()
// bytes). It returns the number of bytes written.
func (e Entry) MarshalTo(buf []byte) int {
	binary.LittleEndian.PutUint64(buf[0:8], e.TsMs)
	copy(buf[8:8+NonceSize], e.Nonce[:])
	binary.LittleEndian.PutUint32(buf[8+NonceSize:EntryHeaderSize], uint32(len(e.Ct)))
	copy(buf[EntryHeaderSize:], e.Ct)
	return e.Size()
}

// UnmarshalEntry parses one entry from b and returns it along with the
// number of bytes consumed.
func UnmarshalEntry(b []byte) (Entry, int, error) {
	if len(b) < EntryHeaderSize {
		return Entry{}, 0, ErrEntryShort
	}
	var e Entry
	e.TsMs = binary.LittleEndian.Uint64(b[0:8])
	copy(e.Nonce[:], b[8:8+NonceSize])
	ctLen := binary.LittleEndian.Uint32(b[8+NonceSize : EntryHeaderSize])
	if ctLen > MaxCtBytes {
		return Entry{}, 0, fmt.Errorf("%w: ct_len=%d", ErrEntryTooLarge, ctLen)
	}
	total := EntryHeaderSize + int(ctLen)
	if len(b) < total {
		return Entry{}, 0, fmt.Errorf("%w: want %d bytes, have %d", ErrEntryCorrupt, total, len(b))
	}
	e.Ct = make([]byte, ctLen)
	copy(e.Ct, b[EntryHeaderSize:total])
	return e, total, nil
}

// ID returns the content-addressed entry id:
//
//	XOF("kv/eid1/" || marshaled_entry, 16)
//
// The computation is O(entry size) and allocates. Callers that sort
// or diff large entry sets should precompute IDs once (see
// SortCanonical) rather than call ID() inside a tight comparator.
func (e Entry) ID() EntryID {
	buf := make([]byte, 0, len(domEID)+e.Size())
	buf = append(buf, domEID...)
	buf = append(buf, e.Marshal()...)
	var out EntryID
	ascon.XOF(buf, out[:])
	return out
}

// Header returns the 28-byte entry header used as associated data for
// the AEAD seal/open.
func (e Entry) Header() []byte {
	h := make([]byte, EntryHeaderSize)
	binary.LittleEndian.PutUint64(h[0:8], e.TsMs)
	copy(h[8:8+NonceSize], e.Nonce[:])
	binary.LittleEndian.PutUint32(h[8+NonceSize:EntryHeaderSize], uint32(len(e.Ct)))
	return h
}

// --- seal / open ---

// Seal encrypts (key, value) into a fresh Entry with timestamp tsMs,
// using a random 16-byte nonce. An empty value denotes a tombstone
// (deletion). The key must be non-empty.
//
// The plaintext layout is:
//
//	| key_len(2) | key | value_len(4) | value |
func Seal(aead *ascon.AEAD, tsMs uint64, key, value []byte) (Entry, error) {
	if len(key) == 0 {
		return Entry{}, ErrEmptyKey
	}
	if len(key) > math.MaxUint16 {
		return Entry{}, ErrKeyTooLong
	}
	if uint64(len(value)) > math.MaxUint32 {
		return Entry{}, ErrValueTooLong
	}

	pt := make([]byte, 2+len(key)+4+len(value))
	binary.LittleEndian.PutUint16(pt[0:2], uint16(len(key)))
	copy(pt[2:2+len(key)], key)
	binary.LittleEndian.PutUint32(pt[2+len(key):2+len(key)+4], uint32(len(value)))
	copy(pt[2+len(key)+4:], value)

	// Nonce: 16 random bytes. Birthday collision probability
	// negligible at any realistic entry volume.
	var nonce [NonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Entry{}, err
	}

	// ct_len is known before encryption: plaintext + tag. Header
	// bytes participate in the AEAD AD; compute the entry header
	// using the pre-known ciphertext length.
	ctLen := uint32(len(pt) + ascon.AEADTagSize)
	if ctLen > MaxCtBytes {
		return Entry{}, ErrEntryTooLarge
	}

	ad := make([]byte, EntryHeaderSize)
	binary.LittleEndian.PutUint64(ad[0:8], tsMs)
	copy(ad[8:8+NonceSize], nonce[:])
	binary.LittleEndian.PutUint32(ad[8+NonceSize:EntryHeaderSize], ctLen)

	ct := aead.Seal(nil, nonce[:], pt, ad)
	// Scrub the heap-resident plaintext copy. Sensitive: pt holds
	// the user's key and value bytes until this point.
	for i := range pt {
		pt[i] = 0
	}

	return Entry{
		TsMs:  tsMs,
		Nonce: nonce,
		Ct:    ct,
	}, nil
}

// Open decrypts e and returns its (key, value). An empty value in the
// returned plaintext indicates a tombstone. Open never panics on
// malformed plaintext: every bounds check promotes to int before
// arithmetic, so a hostile or corrupt plaintext with klen near 2^16
// cannot wrap the length check.
func Open(aead *ascon.AEAD, e Entry) (key, value []byte, err error) {
	pt, err := aead.Open(nil, e.Nonce[:], e.Ct, e.Header())
	if err != nil {
		return nil, nil, err
	}
	if len(pt) < 2 {
		return nil, nil, ErrPlaintextBad
	}
	klen := int(binary.LittleEndian.Uint16(pt[0:2]))
	if 2+klen+4 > len(pt) {
		return nil, nil, ErrPlaintextBad
	}
	vlen := int(binary.LittleEndian.Uint32(pt[2+klen : 2+klen+4]))
	if vlen < 0 || 2+klen+4+vlen != len(pt) {
		// vlen < 0 cannot actually happen (uint32 -> int on 64-bit
		// platforms is always non-negative within MaxCtBytes), but the
		// guard documents the invariant.
		return nil, nil, ErrPlaintextBad
	}
	key = make([]byte, klen)
	copy(key, pt[2:2+klen])
	value = make([]byte, vlen)
	copy(value, pt[2+klen+4:])
	return key, value, nil
}
