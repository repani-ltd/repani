// Package trudge implements trudge1, a deliberately slow,
// memory-hard password-based key derivation function built on
// Ascon-XOF128: fill a 256 MiB pool from one XOF squeeze, sweep
// it once sequentially, then walk it 2^24 data-dependent steps,
// overwriting as you go.
//
// The design's primary target is simplicity of implementation --
// the whole function re-implements from SPEC.t in a few dozen
// lines against any Ascon-XOF128 -- while offering a decent
// security margin for low-value keys. It is NOT a substitute for
// Argon2id where high-value secrets are at stake; SPEC.t states
// the threat model and the accepted residues.
//
// Trudge is intended for settings where CACHING the derived key
// is acceptable: derive once per machine, store the output under
// the operating system's file permissions, and re-derive only on
// a new or wiped machine. A derivation is minutes on old
// hardware by design (that cost, paid per guess, is the entire
// security argument), so per-login re-derivation is the wrong
// deployment; a caller that cannot cache wants a different tool.
//
// Parameters are fixed by the version, not by the caller: trudge1
// is pool 2^24 entries of 16 bytes (256 MiB), one full sweep plus
// 2^24 walk steps. A different parameterization is a different,
// incompatible function by construction (the parameters are bound
// into the fill preimage).
package trudge

import (
	"encoding/binary"

	"repani.com/ascon"
)

// trudge1 parameters: pool entries (2^poolBits) and data-dependent
// walk steps (the sweep is always one full pass). Frozen; see
// SPEC.t. A change here is a new version, never an edit.
const (
	poolBits = 24
	steps    = 1 << 24
)

// Key derives len(out) bytes from salt and passphrase under the
// trudge1 parameters: 256 MiB of memory and tens of seconds to
// minutes of work, once, with the output cached by the caller.
// The salt is mandatory (it defeats multi-target grinding and
// rainbow tables); callers derive it from a public identity,
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

	// current is an explicit COPY of the last entry (an
	// implementation that aliases the pool derives different
	// keys; SPEC.t forbids it).
	var current [16]byte
	copy(current[:], pool[(entries-1)*16:])
	var buf [32]byte
	mix := func(pos int) {
		copy(buf[:16], pool[pos*16:pos*16+16])
		copy(buf[16:], current[:])
		ascon.XOF(buf[:], current[:])
		copy(pool[pos*16:pos*16+16], current[:])
	}

	// Sweep: one full sequential pass, data-independent. Its
	// addresses are public, so a cache-timing observer learns
	// nothing here, and every entry is guaranteed to be mixed
	// with walk state before the secret-dependent phase begins
	// (SPEC.t, Constraints).
	for i := range entries {
		mix(i)
	}

	// Walk: data-dependent, with write-back -- the write-back is
	// what defeats checkpoint recomputation of the fill stream
	// (SPEC.t, Why write-back).
	mask := uint32(entries - 1)
	for range steps {
		pos := int((uint32(current[0])<<16 | uint32(current[1])<<8 | uint32(current[2])) & mask)
		mix(pos)
	}

	// Output: a fresh finalizing hash, so callers may take any
	// length without exposing walk state.
	fin := make([]byte, 0, 10+16)
	fin = append(fin, "trudge1out"...)
	fin = append(fin, current[:]...)
	ascon.XOF(fin, out)
}
