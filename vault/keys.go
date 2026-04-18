// Package vault defines the crypto-aware types and derivations used
// by ASCON-KV: key material, vault identifier, entry encoding, and
// the suffix-ladder hash scheme from PROTOCOL.txt.
package vault

import (
	"crypto/rand"
	"errors"

	"github.com/pavlos/kv/ascon"
)

// Sizes.
const (
	RootKeySize     = 32
	AEADKeySize     = 16
	AuthKeySize     = 16
	VaultIDSize     = 16
	EntryIDSize     = 16
	NonceSize       = 16
	MACSize         = 16
	RegisterKeySize = 16
)

// RootKey is the one durable secret a client must protect. All other
// key material is derived from it.
type RootKey [RootKeySize]byte

// AEADKey is the 16-byte key passed to Ascon-AEAD128 when sealing or
// opening an entry.
type AEADKey [AEADKeySize]byte

// AuthKey is the 16-byte MAC key the server holds for a vault after
// pair-time registration. It authenticates requests and responses on
// the wire; it does NOT grant decryption of any entry.
type AuthKey [AuthKeySize]byte

// RegisterKey is the 16-byte server-side secret that gates vault
// registration. The server holds it at rest (typically a file loaded
// at startup); the client must present a MAC under it in the first
// (REGISTER) request for a new VaultID or the server rejects with
// StatusBadMAC. It is cryptographically independent of every other
// key: operators may safely rotate it without re-pairing clients
// (rotation only affects the ability to register NEW vaults).
type RegisterKey [RegisterKeySize]byte

// VaultID is the public handle the server uses to address a vault.
type VaultID [VaultIDSize]byte

// EntryID is the content address of an entry (Ascon-XOF128 over its
// marshaled bytes).
type EntryID [EntryIDSize]byte

// Domain separators are 8 bytes each, per PROTOCOL.txt.
var (
	domAEAD  = []byte("kv/aead/")
	domVID   = []byte("kv/vid1/")
	domAuth  = []byte("kv/auth/")
	domRMAC  = []byte("kv/rmac/")
	domRsMAC = []byte("kv/rsmc/")
	domEID   = []byte("kv/eid1/")
	domLAD   = []byte("kv/lad1/")
	domRAdm  = []byte("kv/radm/")
)

// NewRandomRootKey returns a RootKey filled from crypto/rand.
func NewRandomRootKey() (RootKey, error) {
	var rk RootKey
	if _, err := rand.Read(rk[:]); err != nil {
		return RootKey{}, err
	}
	return rk, nil
}

// NewRegisterKey returns a RegisterKey filled from crypto/rand. The
// operator generates one per server at deploy time and distributes
// it (via EncodeRegisterKey) to every user authorized to create a
// new vault.
func NewRegisterKey() (RegisterKey, error) {
	var rk RegisterKey
	if _, err := rand.Read(rk[:]); err != nil {
		return RegisterKey{}, err
	}
	return rk, nil
}

// DeriveAEADKey computes AEAD_Key = XOF("kv/aead/" || RootKey, 16).
func DeriveAEADKey(rk RootKey) AEADKey {
	var out AEADKey
	buf := make([]byte, 0, len(domAEAD)+RootKeySize)
	buf = append(buf, domAEAD...)
	buf = append(buf, rk[:]...)
	ascon.XOF(buf, out[:])
	zero(buf)
	return out
}

// DeriveVaultID computes VaultID = XOF("kv/vid1/" || RootKey, 16).
func DeriveVaultID(rk RootKey) VaultID {
	var out VaultID
	buf := make([]byte, 0, len(domVID)+RootKeySize)
	buf = append(buf, domVID...)
	buf = append(buf, rk[:]...)
	ascon.XOF(buf, out[:])
	zero(buf)
	return out
}

// DeriveAuthKey computes AuthKey = XOF("kv/auth/" || RootKey, 16).
// The server learns this on the first (registration) request and uses
// it to verify every subsequent request and to authenticate every
// response. It is cryptographically independent of AEAD_Key by domain
// separation.
func DeriveAuthKey(rk RootKey) AuthKey {
	var out AuthKey
	buf := make([]byte, 0, len(domAuth)+RootKeySize)
	buf = append(buf, domAuth...)
	buf = append(buf, rk[:]...)
	ascon.XOF(buf, out[:])
	zero(buf)
	return out
}

// RequestMAC computes the 16-byte request authentication tag:
//
//	XOF("kv/rmac/" || AuthKey || body, 16)
//
// over a body that includes every wire field preceding the MAC.
func RequestMAC(ak AuthKey, body []byte) [MACSize]byte {
	var out [MACSize]byte
	buf := make([]byte, 0, len(domRMAC)+AuthKeySize+len(body))
	buf = append(buf, domRMAC...)
	buf = append(buf, ak[:]...)
	buf = append(buf, body...)
	ascon.XOF(buf, out[:])
	zero(buf)
	return out
}

// ResponseMAC computes the 16-byte response authentication tag:
//
//	XOF("kv/rsmc/" || AuthKey || request_mac || response_body, 16)
//
// Binding request_mac into the response tag prevents an attacker from
// stitching a cached response to a new request.
func ResponseMAC(ak AuthKey, requestMAC [MACSize]byte, body []byte) [MACSize]byte {
	var out [MACSize]byte
	buf := make([]byte, 0, len(domRsMAC)+AuthKeySize+MACSize+len(body))
	buf = append(buf, domRsMAC...)
	buf = append(buf, ak[:]...)
	buf = append(buf, requestMAC[:]...)
	buf = append(buf, body...)
	ascon.XOF(buf, out[:])
	zero(buf)
	return out
}

// RegisterAdminMAC computes the 16-byte proof-of-knowledge the
// client includes in a REGISTER request to satisfy a server that has
// loaded a RegisterKey:
//
//	XOF("kv/radm/" || RegisterKey || AuthKey || VaultID, 16)
//
// The MAC binds the specific (VaultID, AuthKey) pair being
// registered so an attacker cannot replay a captured MAC to register
// a DIFFERENT vault or a different AuthKey under the same vault.
func RegisterAdminMAC(rk RegisterKey, ak AuthKey, vid VaultID) [MACSize]byte {
	var out [MACSize]byte
	buf := make([]byte, 0, len(domRAdm)+RegisterKeySize+AuthKeySize+VaultIDSize)
	buf = append(buf, domRAdm...)
	buf = append(buf, rk[:]...)
	buf = append(buf, ak[:]...)
	buf = append(buf, vid[:]...)
	ascon.XOF(buf, out[:])
	zero(buf)
	return out
}

// AEAD returns a ready-to-use Ascon-AEAD128 cipher for the given
// RootKey's derived AEAD_Key.
func (rk RootKey) AEAD() (*ascon.AEAD, error) {
	k := DeriveAEADKey(rk)
	a, err := ascon.NewAEAD(k[:])
	k = AEADKey{} // best-effort scrub of the intermediate
	return a, err
}

// ErrInvalidKey is returned when a supplied key material is malformed.
var ErrInvalidKey = errors.New("vault: invalid key")

// zero wipes a byte slice. Best-effort: the compiler may elide this
// under optimization, but it materially reduces accidental leakage of
// RootKey bytes via heap re-use in long-lived client processes.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
