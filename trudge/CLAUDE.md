# trudge

trudge1: a deliberately slow, memory-hard KDF on Ascon-XOF128 —
fill a 256 MiB pool from one squeeze, walk it 2^24 data-dependent
steps with write-back. Primary design target is simplicity of
implementation (re-implementable from SPEC.t in a few dozen
lines) with an honestly stated, decent-not-heroic security
margin; Argon2id is the answer for high-value secrets.

- SPEC.t is normative; the constraints in it came out of the
  2026-08-25 adversarial review of the ancestor Gimli pool KDF
  (`_attic/quietcasting-orig/experimental/simple`) and are
  load-bearing — do not "simplify" write-back, t = n, the salt,
  the copy semantics, or the in-preimage parameters away.
- Parameters are frozen inside the version tag; a change is
  trudge2, never an edit. Test vectors (tiny profile + one full
  trudge1 vector) are the cross-implementation contract.
- Not a primitive package (it imports repani.com/ascon), so it
  lives under the product rules of the root CLAUDE.md.
- First consumer: the qsl registry's key derivation (callsign as
  salt); its input canonicalization lives in qsl's own docs, not
  here.
