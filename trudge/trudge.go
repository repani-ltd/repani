// Package trudge implements trudge1, a deliberately slow,
// memory-hard password-based key derivation function built on
// Ascon-XOF128: fill a 256 MiB pool from one XOF squeeze, then
// walk it 2^24 data-dependent steps, overwriting as you go.
//
// The design's primary target is simplicity of implementation --
// the whole function re-implements from SPEC.t in a few dozen
// lines against any Ascon-XOF128 -- while offering a decent
// security margin for low-value keys. It is NOT a substitute for
// Argon2id where high-value secrets are at stake; SPEC.t states
// the threat model and the accepted residues.
//
// Parameters are fixed by the version, not by the caller: trudge1
// is pool 2^24 entries of 16 bytes (256 MiB) and 2^24 walk steps,
// several seconds and 256 MiB on commodity hardware. A different
// parameterization is a different, incompatible function by
// construction (the parameters are bound into the fill preimage).
package trudge

import (
	"encoding/binary"

	"repani.com/ascon"
)

// trudge1 parameters: pool entries (2^poolBits) and walk steps.
// Frozen; see SPEC.t. A change here is a new version, never an
// edit.
const (
	poolBits = 24
	steps    = 1 << 24
)

// Key derives len(out) bytes from salt and passphrase under the
// trudge1 parameters: 256 MiB of memory and a few seconds of
// work. The salt is mandatory (it defeats multi-target grinding
// and rainbow tables); callers derive it from a public identity,
// never leave it empty. Both inputs are length-prefixed
// internally, so no (salt, passphrase) pair collides with a
// different split of the same bytes.
func Key(salt, passphrase, out []byte) {
	derive(poolBits, steps, salt, passphrase, out)
}

// derive is Key with open parameters, kept unexported: tiny
// parameterizations exist only for tests and cross-implementation
// vectors (SPEC.t, Test vectors). poolBits must be at most 24
// (the walk draws 3 bytes of position) and at least 1.
func derive(poolBits, steps int, salt, passphrase, out []byte) {
	entries := 1 << poolBits
	pool := make([]byte, entries*16)

	// Fill: one sequential squeeze over the version tag, the
	// parameters, and the length-prefixed inputs.
	pre := make([]byte, 0, 16+len(salt)+len(passphrase))
	pre = append(pre, "trudge1"...)
	pre = append(pre, byte(poolBits))
	pre = binary.LittleEndian.AppendUint32(pre, uint32(steps))
	pre = binary.LittleEndian.AppendUint32(pre, uint32(len(salt)))
	pre = append(pre, salt...)
	pre = binary.LittleEndian.AppendUint32(pre, uint32(len(passphrase)))
	pre = append(pre, passphrase...)
	ascon.XOF(pre, pool)

	// Walk: current is an explicit COPY of the last entry (an
	// implementation that aliases the pool derives different
	// keys; SPEC.t forbids it). Each step mixes the entry at pos
	// into current and writes current back over that entry --
	// the write-back is what defeats checkpoint recomputation of
	// the fill stream (SPEC.t, Why write-back).
	var current [16]byte
	copy(current[:], pool[(entries-1)*16:])
	mask := uint32(entries - 1)
	pos := int((uint32(current[0])<<16 | uint32(current[1])<<8 | uint32(current[2])) & mask)
	var buf [32]byte
	for range steps {
		copy(buf[:16], pool[pos*16:pos*16+16])
		copy(buf[16:], current[:])
		ascon.XOF(buf[:], current[:])
		copy(pool[pos*16:pos*16+16], current[:])
		pos = int((uint32(current[0])<<16 | uint32(current[1])<<8 | uint32(current[2])) & mask)
	}

	// Output: a fresh finalizing hash, so callers may take any
	// length without exposing walk state.
	fin := make([]byte, 0, 10+16)
	fin = append(fin, "trudge1out"...)
	fin = append(fin, current[:]...)
	ascon.XOF(fin, out)
}
