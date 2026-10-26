package vault

import (
	"crypto/rand"
	"encoding/binary"
	"time"

	"repani.com/ascon"
)

// hedgedNonce derives a 16-byte nonce from a domain, optional context,
// a high-resolution timestamp, and 16 fresh random bytes:
//
//	CXOF(dom, context || u64(unixNano) || random16, 16)
//
// The random bytes provide uniqueness; the timestamp is a hedge against
// an RNG that repeats (a fork or early boot) — see PROTOCOL.t §Nonces.
func hedgedNonce(dom, context []byte) ([NonceSize]byte, error) {
	var r [16]byte
	if _, err := rand.Read(r[:]); err != nil {
		return [NonceSize]byte{}, err
	}
	buf := make([]byte, 0, len(context)+8+16)
	buf = append(buf, context...)
	buf = binary.LittleEndian.AppendUint64(buf, uint64(time.Now().UnixNano()))
	buf = append(buf, r[:]...)
	var out [NonceSize]byte
	ascon.CXOF(dom, buf, out[:])
	return out, nil
}

// FrameNonce returns a hedged frame nonce bound to the vault:
// CXOF("kv-fnon", VaultID || ts_highres || random16, 16).
func FrameNonce(vid VaultID) ([NonceSize]byte, error) {
	return hedgedNonce(domFN, vid[:])
}

// EntryNonce returns a hedged entry nonce:
// CXOF("kv-enon", ts_highres || random16, 16).
func EntryNonce() ([NonceSize]byte, error) {
	return hedgedNonce(domEN, nil)
}
