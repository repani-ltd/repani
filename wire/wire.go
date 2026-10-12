// Package wire encodes the ASCON-KV v1 frame: a single Ascon-AEAD128
// envelope carrying every message in both directions. See PROTOCOL.t
// §Frame and §Payloads. All integers are little-endian.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"

	"repani.com/ascon"
	"repani.com/kv/vault"
)

// Version is the frame version byte.
const Version byte = 0x01

// FrameHeaderSize = ver(1) + kind(1) + vault_id(16) + nonce(16).
const FrameHeaderSize = 1 + 1 + vault.VaultIDSize + vault.NonceSize

// Kind selects the opening key and binds direction (authenticated as AD).
const (
	KindRegister byte = 0x01 // opened with K_reg   (client -> server)
	KindSync     byte = 0x02 // opened with K_c2s   (client -> server)
	KindResp     byte = 0x03 // opened with K_s2c   (server -> client)
)

// Op is the SYNC sub-operation (first byte of a SYNC payload).
const (
	OpProbe byte = 0x00
	OpPull  byte = 0x01
	OpPush  byte = 0x02
)

// Status is the status inside a sealed RESP (PROTOCOL.t, Status).
type Status byte

const (
	StatusOK         Status = 0x00
	StatusDiffer     Status = 0x01
	StatusQuota      Status = 0x02
	StatusBadRequest Status = 0x03
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusDiffer:
		return "differ"
	case StatusQuota:
		return "quota"
	case StatusBadRequest:
		return "bad-request"
	default:
		return fmt.Sprintf("status(0x%02x)", byte(s))
	}
}

// PreChannel is the single unauthenticated byte a server answers with
// when it has no key to seal a RESP (PROTOCOL.t, Errors before the
// channel). It is an error: a client returns the one it got, and a
// caller tests for one with errors.Is.
type PreChannel byte

const (
	ErrBadFrame     PreChannel = 0xE0
	ErrBadVersion   PreChannel = 0xE1
	ErrBadKind      PreChannel = 0xE2
	ErrUnknownVault PreChannel = 0xE3
	ErrBadAuth      PreChannel = 0xE4
	ErrTryLater     PreChannel = 0xE5
)

func (p PreChannel) Error() string { return "server returned " + p.String() }

func (p PreChannel) String() string {
	switch p {
	case ErrBadFrame:
		return "bad-frame"
	case ErrBadVersion:
		return "bad-version"
	case ErrBadKind:
		return "bad-kind"
	case ErrUnknownVault:
		return "unknown-vault"
	case ErrBadAuth:
		return "bad-auth"
	case ErrTryLater:
		return "try-later"
	default:
		return fmt.Sprintf("pre-channel(0x%02x)", byte(p))
	}
}

// Reply is p as the one-byte response a server sends.
func (p PreChannel) Reply() []byte { return []byte{byte(p)} }

// CursorSize is a canonical position: ts_ms(8) || entry_id(16).
const CursorSize = 8 + vault.EntryIDSize

// SyncPath is the one HTTP route (PROTOCOL.t, HTTP), relative to the
// server's base URL, which may carry a prefix: a bare kvserve serves
// /v1/sync, an almanac station /kv/v1/sync.
const SyncPath = "/v1/sync"

// PageBytes bounds a PULL page and a PUSH batch, so frames stay near
// this size. MaxFrameBytes is the transport body cap, comfortably above
// one page plus framing and one max-size entry.
const (
	PageBytes     = 256 * 1024
	MaxFrameBytes = 1 << 20
)

// Codec errors.
var (
	ErrShort     = errors.New("wire: truncated frame")
	ErrVersion   = errors.New("wire: unsupported version")
	ErrOpen      = errors.New("wire: seal did not open")
	ErrBadOp     = errors.New("wire: unknown sync op")
	ErrMalformed = errors.New("wire: malformed payload")
)

// --- envelope ---

// Header is a frame's cleartext header after its version byte: what
// routes the frame and, as associated data, binds it.
type Header struct {
	Kind    byte
	VaultID vault.VaultID
	Nonce   [vault.NonceSize]byte
}

// bytes is the header as sent: ver || kind || vault_id || nonce.
func (h Header) bytes() []byte {
	b := make([]byte, FrameHeaderSize)
	b[0] = Version
	b[1] = h.Kind
	copy(b[2:2+vault.VaultIDSize], h.VaultID[:])
	copy(b[2+vault.VaultIDSize:], h.Nonce[:])
	return b
}

// SealFrame builds a full frame: the 34-byte cleartext header followed
// by AEAD(K, nonce, ad=header, pt=payload).
func SealFrame(h Header, k vault.ChannelKey, payload []byte) ([]byte, error) {
	a, err := ascon.NewAEAD(k[:])
	if err != nil {
		return nil, err
	}
	ad := h.bytes()
	out := make([]byte, FrameHeaderSize, FrameHeaderSize+len(payload)+ascon.AEADTagSize)
	copy(out, ad)
	return a.Seal(out, h.Nonce[:], payload, ad), nil
}

// ParseHeader splits a frame into its header and the trailing sealed
// bytes, without opening. It validates only length and version.
func ParseHeader(b []byte) (Header, []byte, error) {
	if len(b) < FrameHeaderSize+ascon.AEADTagSize {
		return Header{}, nil, ErrShort
	}
	if b[0] != Version {
		return Header{}, nil, ErrVersion
	}
	h := Header{Kind: b[1]}
	copy(h.VaultID[:], b[2:2+vault.VaultIDSize])
	copy(h.Nonce[:], b[2+vault.VaultIDSize:FrameHeaderSize])
	return h, b[FrameHeaderSize:], nil
}

// OpenFrame opens sealed under k with h as its associated data, and
// returns the payload.
func OpenFrame(k vault.ChannelKey, h Header, sealed []byte) ([]byte, error) {
	a, err := ascon.NewAEAD(k[:])
	if err != nil {
		return nil, err
	}
	pt, err := a.Open(nil, h.Nonce[:], sealed, h.bytes())
	if err != nil {
		return nil, ErrOpen
	}
	return pt, nil
}

// --- payloads ---

// SyncReq is a decoded SYNC payload.
type SyncReq struct {
	Op      byte
	SetHash [16]byte      // OpProbe
	CurTs   uint64        // OpPull cursor ts_ms
	CurID   vault.EntryID // OpPull cursor entry_id
	Entries []vault.Entry // OpPush
}

// Marshal encodes r as a SYNC payload.
func (r SyncReq) Marshal() []byte {
	switch r.Op {
	case OpProbe:
		out := make([]byte, 1+16)
		out[0] = OpProbe
		copy(out[1:], r.SetHash[:])
		return out
	case OpPull:
		out := make([]byte, 1+CursorSize)
		out[0] = OpPull
		binary.LittleEndian.PutUint64(out[1:9], r.CurTs)
		copy(out[9:], r.CurID[:])
		return out
	default: // OpPush
		out := []byte{OpPush}
		return appendEntries(out, r.Entries)
	}
}

// ParseSync decodes a SYNC payload.
func ParseSync(payload []byte) (SyncReq, error) {
	if len(payload) < 1 {
		return SyncReq{}, ErrMalformed
	}
	var r SyncReq
	r.Op = payload[0]
	body := payload[1:]
	switch r.Op {
	case OpProbe:
		if len(body) != 16 {
			return SyncReq{}, ErrMalformed
		}
		copy(r.SetHash[:], body)
	case OpPull:
		if len(body) != CursorSize {
			return SyncReq{}, ErrMalformed
		}
		r.CurTs = binary.LittleEndian.Uint64(body[0:8])
		copy(r.CurID[:], body[8:])
	case OpPush:
		entries, err := parseEntries(body)
		if err != nil {
			return SyncReq{}, err
		}
		r.Entries = entries
	default:
		return SyncReq{}, ErrBadOp
	}
	return r, nil
}

// Resp is a decoded RESP payload.
type Resp struct {
	Status   Status
	ReqNonce [vault.NonceSize]byte
	Entries  []vault.Entry
}

// Marshal encodes resp as a RESP payload.
func (resp Resp) Marshal() []byte {
	out := make([]byte, 1+vault.NonceSize)
	out[0] = byte(resp.Status)
	copy(out[1:], resp.ReqNonce[:])
	return appendEntries(out, resp.Entries)
}

// ParseResp decodes a RESP payload.
func ParseResp(payload []byte) (Resp, error) {
	if len(payload) < 1+vault.NonceSize {
		return Resp{}, ErrMalformed
	}
	var resp Resp
	resp.Status = Status(payload[0])
	copy(resp.ReqNonce[:], payload[1:1+vault.NonceSize])
	entries, err := parseEntries(payload[1+vault.NonceSize:])
	if err != nil {
		return Resp{}, err
	}
	resp.Entries = entries
	return resp, nil
}

// --- entry lists (push_count(4) || entries) ---

func appendEntries(dst []byte, entries []vault.Entry) []byte {
	var cnt [4]byte
	binary.LittleEndian.PutUint32(cnt[:], uint32(len(entries)))
	dst = append(dst, cnt[:]...)
	for _, e := range entries {
		dst = append(dst, e.Marshal()...)
	}
	return dst
}

func parseEntries(b []byte) ([]vault.Entry, error) {
	if len(b) < 4 {
		return nil, ErrMalformed
	}
	n := binary.LittleEndian.Uint32(b[0:4])
	off := 4
	out := make([]vault.Entry, 0, min(n, 4096))
	for range int(n) {
		e, consumed, err := vault.UnmarshalEntry(b[off:])
		if err != nil {
			return nil, fmt.Errorf("wire: entry %d: %w", len(out), err)
		}
		out = append(out, e)
		off += consumed
	}
	if off != len(b) {
		return nil, fmt.Errorf("%w: %d trailing bytes after entries", ErrMalformed, len(b)-off)
	}
	return out, nil
}
