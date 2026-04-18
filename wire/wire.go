// Package wire encodes and decodes the ASCON-KV request and response
// frames as specified in PROTOCOL.txt. All integers are little-endian.
// No JSON, no length-delimited framing beyond the per-entry ct_len.
package wire

import (
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/pavlos/kv/vault"
)

// ProtocolVersion is the wire version byte carried at the start of
// every request and response.
const ProtocolVersion byte = 0x01

// Flags byte in a request.
const (
	FlagRegister byte = 0x01 // reg_authkey is present
	flagReserved byte = 0xFE // bits 1..7 — must be zero on the wire
)

// Status byte in a response.
const (
	StatusOK           byte = 0x00
	StatusUnknownVault byte = 0x01
	StatusBadMAC       byte = 0x02
	StatusQuota        byte = 0x03
	StatusBadRequest   byte = 0x04
)

// Errors surfaced by the codec.
var (
	ErrShort          = errors.New("wire: truncated message")
	ErrVersion        = errors.New("wire: unsupported protocol version")
	ErrFlags          = errors.New("wire: reserved flag bits set")
	ErrBadMAC         = errors.New("wire: MAC verification failed")
	ErrBadStatus      = errors.New("wire: unknown status byte")
	ErrTooManyPushes  = errors.New("wire: push_count exceeds 65535")
	ErrLadderMismatch = errors.New("wire: ladder length does not match N")
	ErrTrailing       = errors.New("wire: trailing bytes after frame")
)

// Request is the client-to-server sync message.
type Request struct {
	VaultID  vault.VaultID
	Flags    byte
	N        uint32        // number of entries the client holds
	Ladder   [][16]byte    // len(Ladder) == len(LadderSizes(N))
	Terminal [16]byte      // hash of ALL client entries
	Pushes   []vault.Entry
	RegAuth  vault.AuthKey // valid iff Flags&FlagRegister != 0
	MAC      [vault.MACSize]byte
}

// Response is the server-to-client reply.
type Response struct {
	Status  byte
	Window  uint32
	Entries []vault.Entry
	MAC     [vault.MACSize]byte
}

// IsRegister reports whether the request carries a reg_authkey.
func (r Request) IsRegister() bool { return r.Flags&FlagRegister != 0 }

// --- request ---

// MarshalBody returns the request bytes up to (but not including) the
// MAC tag. Returns an error if the request is internally inconsistent
// (too many pushes, ladder length does not match N, reserved flag
// bits set).
func (r Request) MarshalBody() ([]byte, error) {
	if len(r.Pushes) > 0xFFFF {
		return nil, ErrTooManyPushes
	}
	if r.Flags&flagReserved != 0 {
		return nil, ErrFlags
	}
	sizes := vault.LadderSizes(r.N)
	if len(sizes) != len(r.Ladder) {
		return nil, fmt.Errorf("%w: have %d, want %d for N=%d",
			ErrLadderMismatch, len(r.Ladder), len(sizes), r.N)
	}
	pushBytes := 0
	for _, e := range r.Pushes {
		pushBytes += e.Size()
	}
	regBytes := 0
	if r.IsRegister() {
		regBytes = vault.AuthKeySize
	}
	total := 1 + vault.VaultIDSize + 1 + 4 + len(sizes)*16 + 16 + 2 + pushBytes + regBytes
	buf := make([]byte, total)
	off := 0
	buf[off] = ProtocolVersion
	off += 1
	copy(buf[off:off+vault.VaultIDSize], r.VaultID[:])
	off += vault.VaultIDSize
	buf[off] = r.Flags
	off += 1
	binary.LittleEndian.PutUint32(buf[off:off+4], r.N)
	off += 4
	for _, h := range r.Ladder {
		copy(buf[off:off+16], h[:])
		off += 16
	}
	copy(buf[off:off+16], r.Terminal[:])
	off += 16
	binary.LittleEndian.PutUint16(buf[off:off+2], uint16(len(r.Pushes)))
	off += 2
	for _, e := range r.Pushes {
		off += e.MarshalTo(buf[off:])
	}
	if r.IsRegister() {
		copy(buf[off:off+vault.AuthKeySize], r.RegAuth[:])
		off += vault.AuthKeySize
	}
	if off != total {
		return nil, fmt.Errorf("wire: request marshal size mismatch: %d vs %d", off, total)
	}
	return buf, nil
}

// Marshal serializes r with its current MAC appended. Returns an
// error on structural inconsistency; see MarshalBody.
func (r Request) Marshal() ([]byte, error) {
	body, err := r.MarshalBody()
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(body)+vault.MACSize)
	copy(out, body)
	copy(out[len(body):], r.MAC[:])
	return out, nil
}

// SignRequest fills r.MAC using the given AuthKey and returns the
// full marshaled request. r is updated in place with the computed MAC
// so the caller can bind it into a subsequent response verification.
func SignRequest(ak vault.AuthKey, r *Request) ([]byte, error) {
	body, err := r.MarshalBody()
	if err != nil {
		return nil, err
	}
	r.MAC = vault.RequestMAC(ak, body)
	out := make([]byte, len(body)+vault.MACSize)
	copy(out, body)
	copy(out[len(body):], r.MAC[:])
	return out, nil
}

// UnmarshalRequest parses a full request (body || mac). It does NOT
// verify the MAC; call VerifyRequestMAC separately once you know the
// AuthKey.
func UnmarshalRequest(b []byte) (Request, error) {
	// minimum size: ver + vid + flags + N + terminal + push_count + mac
	minSize := 1 + vault.VaultIDSize + 1 + 4 + 16 + 2 + vault.MACSize
	if len(b) < minSize {
		return Request{}, fmt.Errorf("%w: request too short", ErrShort)
	}
	off := 0
	if b[off] != ProtocolVersion {
		return Request{}, fmt.Errorf("%w: got 0x%02x, want 0x%02x", ErrVersion, b[off], ProtocolVersion)
	}
	off += 1

	var r Request
	copy(r.VaultID[:], b[off:off+vault.VaultIDSize])
	off += vault.VaultIDSize

	r.Flags = b[off]
	off += 1
	if r.Flags&flagReserved != 0 {
		return Request{}, ErrFlags
	}

	r.N = binary.LittleEndian.Uint32(b[off : off+4])
	off += 4

	sizes := vault.LadderSizes(r.N)
	needLadder := len(sizes) * 16
	regBytes := 0
	if r.IsRegister() {
		regBytes = vault.AuthKeySize
	}
	if len(b)-off < needLadder+16+2+regBytes+vault.MACSize {
		return Request{}, fmt.Errorf("%w: ladder+terminal+push_count", ErrShort)
	}
	r.Ladder = make([][16]byte, len(sizes))
	for i := range sizes {
		copy(r.Ladder[i][:], b[off:off+16])
		off += 16
	}
	copy(r.Terminal[:], b[off:off+16])
	off += 16

	pc := binary.LittleEndian.Uint16(b[off : off+2])
	off += 2

	// The trailing block holds: entries || [reg_authkey] || mac.
	// entries region = everything between off and len(b) - (regBytes + MACSize).
	entryEnd := len(b) - regBytes - vault.MACSize
	if entryEnd < off {
		return Request{}, fmt.Errorf("%w: entries region negative", ErrShort)
	}
	r.Pushes = make([]vault.Entry, 0, pc)
	for range int(pc) {
		if entryEnd-off < vault.EntryHeaderSize {
			return Request{}, fmt.Errorf("%w: entry header", ErrShort)
		}
		e, n, err := vault.UnmarshalEntry(b[off:entryEnd])
		if err != nil {
			return Request{}, fmt.Errorf("wire: entry parse: %w", err)
		}
		r.Pushes = append(r.Pushes, e)
		off += n
	}
	if off != entryEnd {
		return Request{}, fmt.Errorf("%w: %d unexpected bytes between entries and trailer",
			ErrTrailing, entryEnd-off)
	}
	if r.IsRegister() {
		copy(r.RegAuth[:], b[off:off+vault.AuthKeySize])
		off += vault.AuthKeySize
	}
	copy(r.MAC[:], b[off:off+vault.MACSize])
	return r, nil
}

// VerifyRequestMAC checks r's MAC against a body reconstructed from
// the rest of r under the given AuthKey. Constant-time compare.
func VerifyRequestMAC(ak vault.AuthKey, r Request) error {
	body, err := r.MarshalBody()
	if err != nil {
		return err
	}
	want := vault.RequestMAC(ak, body)
	if subtle.ConstantTimeCompare(want[:], r.MAC[:]) != 1 {
		return ErrBadMAC
	}
	return nil
}

// --- response ---

// MarshalBody returns the response bytes up to (but not including)
// the resp_mac tag.
func (resp Response) MarshalBody() ([]byte, error) {
	if len(resp.Entries) > 0xFFFF {
		return nil, ErrTooManyPushes
	}
	entriesBytes := 0
	for _, e := range resp.Entries {
		entriesBytes += e.Size()
	}
	out := make([]byte, 1+1+4+2+entriesBytes)
	off := 0
	out[off] = ProtocolVersion
	off += 1
	out[off] = resp.Status
	off += 1
	binary.LittleEndian.PutUint32(out[off:off+4], resp.Window)
	off += 4
	binary.LittleEndian.PutUint16(out[off:off+2], uint16(len(resp.Entries)))
	off += 2
	for _, e := range resp.Entries {
		off += e.MarshalTo(out[off:])
	}
	return out, nil
}

// SignResponse returns the full marshaled response with the resp_mac
// computed under AuthKey and bound to the request's MAC.
func SignResponse(ak vault.AuthKey, reqMAC [vault.MACSize]byte, resp Response) ([]byte, error) {
	body, err := resp.MarshalBody()
	if err != nil {
		return nil, err
	}
	mac := vault.ResponseMAC(ak, reqMAC, body)
	out := make([]byte, len(body)+vault.MACSize)
	copy(out, body)
	copy(out[len(body):], mac[:])
	return out, nil
}

// UnmarshalResponse parses a response (body || mac). It does NOT
// verify the MAC; call VerifyResponseMAC separately.
func UnmarshalResponse(b []byte) (Response, error) {
	// min: ver + status + window + push_count + mac
	minSize := 1 + 1 + 4 + 2 + vault.MACSize
	if len(b) < minSize {
		return Response{}, fmt.Errorf("%w: response header", ErrShort)
	}
	off := 0
	if b[off] != ProtocolVersion {
		return Response{}, fmt.Errorf("%w: got 0x%02x", ErrVersion, b[off])
	}
	off += 1
	var resp Response
	resp.Status = b[off]
	off += 1
	resp.Window = binary.LittleEndian.Uint32(b[off : off+4])
	off += 4
	pc := binary.LittleEndian.Uint16(b[off : off+2])
	off += 2

	entryEnd := len(b) - vault.MACSize
	resp.Entries = make([]vault.Entry, 0, pc)
	for range int(pc) {
		if entryEnd-off < vault.EntryHeaderSize {
			return Response{}, fmt.Errorf("%w: entry header", ErrShort)
		}
		e, n, err := vault.UnmarshalEntry(b[off:entryEnd])
		if err != nil {
			return Response{}, fmt.Errorf("wire: entry parse: %w", err)
		}
		resp.Entries = append(resp.Entries, e)
		off += n
	}
	if off != entryEnd {
		return Response{}, fmt.Errorf("%w: %d bytes between entries and MAC",
			ErrTrailing, entryEnd-off)
	}
	copy(resp.MAC[:], b[off:off+vault.MACSize])
	return resp, nil
}

// VerifyResponseMAC checks resp.MAC under AuthKey bound to reqMAC.
// Constant-time compare.
func VerifyResponseMAC(ak vault.AuthKey, reqMAC [vault.MACSize]byte, resp Response) error {
	body, err := resp.MarshalBody()
	if err != nil {
		return err
	}
	want := vault.ResponseMAC(ak, reqMAC, body)
	if subtle.ConstantTimeCompare(want[:], resp.MAC[:]) != 1 {
		return ErrBadMAC
	}
	return nil
}

// StatusText returns a short human-readable label for s. Useful for
// logging and error messages; not exchanged on the wire.
func StatusText(s byte) string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusUnknownVault:
		return "unknown-vault"
	case StatusBadMAC:
		return "bad-mac"
	case StatusQuota:
		return "quota"
	case StatusBadRequest:
		return "bad-request"
	default:
		return fmt.Sprintf("unknown(0x%02x)", s)
	}
}
