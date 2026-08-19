package vault

import (
	"encoding/binary"
	"errors"
	"fmt"

	"repani.com/prim/ascon"
)

// Entry header: ts_ms(8) + nonce(16) + ct_len(2) = 26 bytes.
const EntryHeaderSize = 8 + NonceSize + 2

// MaxEntryBytes caps a whole marshaled entry (header + ciphertext) so
// it always fits a single frame. ct_len is 2 bytes, so a ciphertext
// cannot exceed 65535 regardless; this cap is the tighter, enforced
// bound. See PROTOCOL.txt §Entries.
const MaxEntryBytes = 64 * 1024

// maxCt is the largest ciphertext a 2-byte ct_len can frame.
const maxCt = 0xFFFF

// Errors surfaced by entry handling.
var (
	ErrEntryShort    = errors.New("vault: entry bytes shorter than header")
	ErrEntryCorrupt  = errors.New("vault: entry ciphertext length mismatch")
	ErrEntryTooLarge = errors.New("vault: entry exceeds MaxEntryBytes")
	ErrKeyTooLong    = errors.New("vault: key exceeds 255 bytes")
	ErrValueTooLong  = errors.New("vault: value exceeds 65535 bytes")
	ErrPlaintextBad  = errors.New("vault: decrypted plaintext is malformed")
	ErrEmptyKey      = errors.New("vault: key must be non-empty")
)

// Entry is the canonical unit of storage, on disk and on the wire:
//
//	| ts_ms(8) | nonce(16) | ct_len(2) | ciphertext |
//
// All integers little-endian. ciphertext includes the trailing 16-byte
// Ascon-AEAD128 tag.
type Entry struct {
	TsMs  uint64
	Nonce [NonceSize]byte
	Ct    []byte
}

// Size returns the marshaled length of e.
func (e Entry) Size() int { return EntryHeaderSize + len(e.Ct) }

// Marshal serializes e into a fresh slice.
func (e Entry) Marshal() []byte {
	out := make([]byte, e.Size())
	e.MarshalTo(out)
	return out
}

// MarshalTo writes e into buf (>= Size()) and returns bytes written.
func (e Entry) MarshalTo(buf []byte) int {
	e.putHeader(buf)
	copy(buf[EntryHeaderSize:], e.Ct)
	return e.Size()
}

// putHeader writes the 26-byte entry header (ts_ms || nonce || ct_len)
// into buf[:EntryHeaderSize].
func (e Entry) putHeader(buf []byte) {
	binary.LittleEndian.PutUint64(buf[0:8], e.TsMs)
	copy(buf[8:8+NonceSize], e.Nonce[:])
	binary.LittleEndian.PutUint16(buf[8+NonceSize:EntryHeaderSize], uint16(len(e.Ct)))
}

// UnmarshalEntry parses one entry from b, returning it and the number
// of bytes consumed.
func UnmarshalEntry(b []byte) (Entry, int, error) {
	if len(b) < EntryHeaderSize {
		return Entry{}, 0, ErrEntryShort
	}
	var e Entry
	e.TsMs = binary.LittleEndian.Uint64(b[0:8])
	copy(e.Nonce[:], b[8:8+NonceSize])
	ctLen := int(binary.LittleEndian.Uint16(b[8+NonceSize : EntryHeaderSize]))
	// A ciphertext must contain at least the AEAD tag; anything shorter is
	// provably junk. The whole entry must fit MaxEntryBytes — enforced here
	// so the server rejects a spec-violating PUSH at parse time and the
	// client rejects an oversized entry from a hostile relay on ingest.
	if ctLen < ascon.AEADTagSize {
		return Entry{}, 0, ErrEntryCorrupt
	}
	total := EntryHeaderSize + ctLen
	if total > MaxEntryBytes {
		return Entry{}, 0, ErrEntryTooLarge
	}
	if len(b) < total {
		return Entry{}, 0, fmt.Errorf("%w: want %d bytes, have %d", ErrEntryCorrupt, total, len(b))
	}
	e.Ct = make([]byte, ctLen)
	copy(e.Ct, b[EntryHeaderSize:total])
	return e, total, nil
}

// Header returns the 26-byte entry header used as AEAD associated data.
func (e Entry) Header() []byte {
	h := make([]byte, EntryHeaderSize)
	e.putHeader(h)
	return h
}

// ID returns the content-addressed entry id:
// XOF("kv/eid1/" || marshaled_entry, 16).
func (e Entry) ID() EntryID {
	buf := make([]byte, 0, len(domEID)+e.Size())
	buf = append(buf, domEID...)
	buf = append(buf, e.Marshal()...)
	var out EntryID
	ascon.XOF(buf, out[:])
	return out
}

// --- seal / open ---

// Seal encrypts (key, value) into a fresh Entry with timestamp tsMs and
// a hedged nonce. An empty value denotes a tombstone. The plaintext is:
//
//	| key_len(1) | key | value_len(2) | value |
func Seal(aead *ascon.AEAD, tsMs uint64, key, value []byte) (Entry, error) {
	if len(key) == 0 {
		return Entry{}, ErrEmptyKey
	}
	if len(key) > 0xFF {
		return Entry{}, ErrKeyTooLong
	}
	if len(value) > 0xFFFF {
		return Entry{}, ErrValueTooLong
	}

	pt := make([]byte, 1+len(key)+2+len(value))
	pt[0] = byte(len(key))
	copy(pt[1:1+len(key)], key)
	binary.LittleEndian.PutUint16(pt[1+len(key):1+len(key)+2], uint16(len(value)))
	copy(pt[1+len(key)+2:], value)

	ctLen := len(pt) + ascon.AEADTagSize
	if ctLen > maxCt || EntryHeaderSize+ctLen > MaxEntryBytes {
		zero(pt)
		return Entry{}, ErrEntryTooLarge
	}

	nonce, err := EntryNonce()
	if err != nil {
		zero(pt)
		return Entry{}, err
	}

	ad := make([]byte, EntryHeaderSize)
	binary.LittleEndian.PutUint64(ad[0:8], tsMs)
	copy(ad[8:8+NonceSize], nonce[:])
	binary.LittleEndian.PutUint16(ad[8+NonceSize:EntryHeaderSize], uint16(ctLen))

	ct := aead.Seal(nil, nonce[:], pt, ad)
	zero(pt) // scrub the plaintext key/value copy

	return Entry{TsMs: tsMs, Nonce: nonce, Ct: ct}, nil
}

// Open decrypts e and returns its (key, value). An empty value denotes
// a tombstone. Bounds checks promote to int before arithmetic so a
// hostile plaintext cannot wrap a length.
func Open(aead *ascon.AEAD, e Entry) (key, value []byte, err error) {
	pt, err := aead.Open(nil, e.Nonce[:], e.Ct, e.Header())
	if err != nil {
		return nil, nil, err
	}
	if len(pt) < 3 { // key_len(1) + value_len(2)
		return nil, nil, ErrPlaintextBad
	}
	klen := int(pt[0])
	if 1+klen+2 > len(pt) {
		return nil, nil, ErrPlaintextBad
	}
	vlen := int(binary.LittleEndian.Uint16(pt[1+klen : 1+klen+2]))
	if 1+klen+2+vlen != len(pt) {
		return nil, nil, ErrPlaintextBad
	}
	key = make([]byte, klen)
	copy(key, pt[1:1+klen])
	value = make([]byte, vlen)
	copy(value, pt[1+klen+2:])
	return key, value, nil
}
