// Package vault defines the crypto-aware types and derivations used by
// ASCON-KV v1: key material, vault identifier, entry encoding, the
// whole-set hash, and hedged nonces. See PROTOCOL.t.
package vault

import (
	"crypto/rand"
	"errors"

	"repani.com/ascon"
)

// Sizes. RootKey is 20 bytes (160 bits): comfortably above the
// 128-bit security level of every derived key and primitive, and
// exactly four 5-byte Crockford base32 groups, so the pairing blob
// is 32 chars with zero padding bits.
const (
	RootKeySize     = 20
	AEADKeySize     = 16
	AuthKeySize     = 16
	ChannelKeySize  = 16
	VaultIDSize     = 16
	EntryIDSize     = 16
	NonceSize       = 16
	RegisterKeySize = 16
)

// RootKey is the one durable secret a device must protect. All other
// key material is derived from it.
type RootKey [RootKeySize]byte

// AEADKey encrypts entries. It never leaves the devices.
type AEADKey [AEADKeySize]byte

// AuthKey is the shared channel root the server learns at registration.
// The directional channel keys are derived from it.
type AuthKey [AuthKeySize]byte

// ChannelKey seals one direction of frames (K_c2s, K_s2c) or the
// registration gate (K_reg).
type ChannelKey [ChannelKeySize]byte

// RegisterKey is the operator secret that gates vault creation.
type RegisterKey [RegisterKeySize]byte

// VaultID is the public routing handle.
type VaultID [VaultIDSize]byte

// EntryID is the content address of an entry.
type EntryID [EntryIDSize]byte

// Domain separators, 8 bytes each (PROTOCOL.t §Keys, §Nonces).
var (
	domAEAD = []byte("kv/aead/")
	domVID  = []byte("kv/vid1/")
	domAuth = []byte("kv/auth/")
	domC2S  = []byte("kv/c2s0/")
	domS2C  = []byte("kv/s2c0/")
	domReg  = []byte("kv/reg0/")
	domEID  = []byte("kv/eid1/")
	domSet  = []byte("kv/sset/")
	domFN   = []byte("kv/rnon/")
	domEN   = []byte("kv/enon/")
)

// ErrInvalidKey is returned when supplied key material is malformed.
var ErrInvalidKey = errors.New("vault: invalid key")

// NewRandomRootKey returns a RootKey filled from crypto/rand.
func NewRandomRootKey() (RootKey, error) {
	var rk RootKey
	if _, err := rand.Read(rk[:]); err != nil {
		return RootKey{}, err
	}
	return rk, nil
}

// NewRegisterKey returns a RegisterKey filled from crypto/rand.
func NewRegisterKey() (RegisterKey, error) {
	var rk RegisterKey
	if _, err := rand.Read(rk[:]); err != nil {
		return RegisterKey{}, err
	}
	return rk, nil
}

// derive16 computes XOF(dom || material, 16) and scrubs its scratch.
func derive16(dom, material []byte) [16]byte {
	buf := make([]byte, 0, len(dom)+len(material))
	buf = append(buf, dom...)
	buf = append(buf, material...)
	var out [16]byte
	ascon.XOF(buf, out[:])
	zero(buf)
	return out
}

// DeriveAEADKey computes AEAD_Key = XOF("kv/aead/" || RootKey, 16).
func DeriveAEADKey(rk RootKey) AEADKey { return derive16(domAEAD, rk[:]) }

// DeriveVaultID computes VaultID = XOF("kv/vid1/" || RootKey, 16).
func DeriveVaultID(rk RootKey) VaultID { return derive16(domVID, rk[:]) }

// DeriveAuthKey computes AuthKey = XOF("kv/auth/" || RootKey, 16).
func DeriveAuthKey(rk RootKey) AuthKey { return derive16(domAuth, rk[:]) }

// DeriveC2S computes K_c2s = XOF("kv/c2s0/" || AuthKey, 16), the key
// that seals client-to-server frames.
func DeriveC2S(ak AuthKey) ChannelKey { return derive16(domC2S, ak[:]) }

// DeriveS2C computes K_s2c = XOF("kv/s2c0/" || AuthKey, 16), the key
// that seals server-to-client frames.
func DeriveS2C(ak AuthKey) ChannelKey { return derive16(domS2C, ak[:]) }

// DeriveRegKey computes K_reg = XOF("kv/reg0/" || RegisterKey, 16), the
// key that seals (and gates) a REGISTER frame.
func DeriveRegKey(rk RegisterKey) ChannelKey { return derive16(domReg, rk[:]) }

// AEAD returns a ready-to-use Ascon-AEAD128 cipher for rk's AEAD_Key.
func (rk RootKey) AEAD() (*ascon.AEAD, error) {
	k := DeriveAEADKey(rk)
	a, err := ascon.NewAEAD(k[:])
	k = AEADKey{} // best-effort scrub of the intermediate
	return a, err
}

// zero wipes a byte slice. Best-effort.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
