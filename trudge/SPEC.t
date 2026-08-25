TRUDGE -- A SIMPLE MEMORY-HARD KDF
.date 2026-08-25
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem Normative specification of trudge1. The Go reference is
.rem repani.com/trudge; its tiny-parameter vectors below are the
.rem cross-implementation contract. Descends from the Gimli pool
.rem KDF in _attic/quietcasting-orig/experimental/simple, with
.rem the defects found in its 2026-08-25 adversarial review fixed
.rem and recorded here as constraints.

Trudge derives keys from low-entropy human input by making each
guess cost real memory and real time: fill a 256 MiB pool from
one Ascon-XOF128 squeeze, then trudge through it -- 2^24 slow,
data-dependent steps, overwriting the path as you walk it.

# Goals, in order

.item Simplicity of implementation. The whole function
re-implements from this page in a few dozen lines against any
Ascon-XOF128. Every independent client (Go, JS, Python, someone's
homebrew logger) must derive identical keys forever; a spec a
person can hold in their head is the best defence against that
ever failing. There are no caller-facing parameters to get wrong.
.item One primitive family. Ascon end to end, like everything
else in the house. No second hash function enters the tree.
.item A decent margin, honestly stated. Trudge defends
low-value keys (a QSL registry credential, not a bank vault)
against offline guessing by costing the attacker 256 MiB per
parallel guess. It has had one adversarial review and no
published cryptanalysis; where high-value secrets are at stake
use Argon2id and pay its complexity.

# The function

trudge1(salt, passphrase, outlen) -> outlen bytes.

Fixed parameters: pool of n = 2^24 entries of 16 bytes each
(256 MiB); t = 2^24 walk steps. XOF is Ascon-XOF128 (NIST SP
800-232). u32le is a 4-byte little-endian length; be24 reads 3
bytes big-endian.

.pre
  fill:
    pre  = "trudge1" || byte(24) || u32le(t)
           || u32le(len salt) || salt
           || u32le(len passphrase) || passphrase
    pool = XOF(pre, n*16 bytes)          one sequential squeeze

  walk:
    current = COPY of pool[n-1]          never an alias
    pos     = be24(current[0:3]) mod n
    repeat t times:
      current   = XOF(pool[pos] || current, 16)
      pool[pos] = current                the write-back
      pos       = be24(current[0:3]) mod n

  output:
    key = XOF("trudge1out" || current, outlen)
.end

n is a power of two, so mod n is a mask. Outputs of different
lengths are prefix-consistent (one finalizing squeeze read
longer), an XOF property callers may rely on.

# Constraints, from the adversarial review

Each of these is normative because its absence was a finding
against the ancestor design (2026-08-25 review); an
implementation that drops one derives weaker or different keys.

.item Write-back is load-bearing. Without it the pool is a pure
function of the fill stream, and an attacker checkpoints the
squeeze (an Ascon state is 40 bytes) instead of holding 256 MiB:
about 640 KB and modest recompute stood in for the whole pool, a
factor of about 400,000 in memory. Overwriting the walked path
makes any entry depend on the whole walk history; recomputation
becomes full replay, and the 256 MiB must actually be held.
.item Steps equal pool entries. The ancestor walked 2^20 steps
over 2^24 entries; the short walk is what made the checkpoint
trade cheap. t = n, scrypt's discipline.
.item The salt is mandatory and the encoding length-prefixed.
An unsalted derivation invites rainbow tables and one grinding
pass across every user; bare concatenation lets different
(salt, passphrase) pairs collide across the boundary. Callers
derive the salt from a public identity (qsl: the callsign).
.item Current is a copy. The ancestor's current ALIASED the
pool's last entry, silently rewriting it every step; two
implementations, one aliasing and one copying, derive different
keys from the same input -- fatal for a deterministic scheme.
Copy is the normative behavior.
.item Parameters live inside the preimage. poolBits and t are
absorbed before the inputs, so any re-parameterization -- the
test profile included -- is a different function by
construction, never a silent variant. New parameters mean a new
version tag ("trudge2"), never an edit to this one.
.item The finalizing hash is fresh. Output is squeezed from
XOF over a distinct tag and current, so no walk state or pool
byte is ever exposed directly, at any output length.

# Accepted residues

Stated so they are deliberate. The walk's addressing is
data-dependent, so a co-resident attacker timing the cache
learns access patterns (Argon2id runs a data-independent first
half for this reason); trudge accepts this because its setting
is a client deriving its own key on its own machine. The fill
is one sequential squeeze and therefore inherently
checkpointable in isolation; the write-back, not the fill, is
what carries the memory bound. And the security argument is
structural (scrypt-shaped, reviewed once), not competition-vetted.

# Cost

Honest figures from the Go reference, one derivation: 256 MiB
resident, about 12.6 s on a 2019-class Intel MacBook Pro
(measured 2026-08-25); expect a few seconds on newer hardware.
An attacker who respects the memory bound pays the same per
guess; renting big-RAM machines, that prices a guess at roughly
$0.2 per million. Against the qsl input profile (a realistic 31
bits: one PIN, one date, one four-letter word) a targeted crack
runs to hundreds or thousands of dollars per victim -- garden
shed security, the deliberate choice of a registry whose safety
net is public detection, not prevention.

# Test vectors

Tiny profile, poolBits 10 and t 1024, for implementation
bring-up only (the parameter binding keeps its outputs disjoint
from trudge1's); 32-byte outputs, hex:

.pre
  salt "W1AW"    pass "1234 19910713 CQCQ"
    f896b74a303a7c86f2bcf1cb66d6a9f3ea6135f80686ab38b46d16f3
    84ec29d4
  salt "EA5XYZ"  pass "1234 19910713 CQCQ"
    b91e774d0f31a6d9f7c585131480ab6a7d9321481135935c2a5692bb
    18cf1627
  salt ""        pass ""
    4cf9055bbb1c4beadb3ed2577e708e13963cf9063465b83f3b9f5177
    5ae1d88f
.end

trudge1, full parameters:

.pre
  salt "W1AW"    pass "1234 19910713 CQCQ"
    12b1053b2cdbfd8ded9e52635e727ac2fca7c79e12aacb58d854bcb1
    63931b71
.end

The Go tests carry the same vectors (trudge_test.go); the full
vector runs unless -short.

.width 72
.cols 1
.font sans
