# prim

Self-contained primitives shared by the pavlos protocols. One
package per mathematical object; stdlib only; no package imports
another; no protocol constants (a package here may not know what
a frame, slot, page or vault is). Algorithms are append-only: a
changed algorithm is a new package, not a revision.

- `ascon` -- Ascon-XOF128 and Ascon-AEAD128 (NIST SP 800-232)
- `golay` -- extended Golay(24,12): corrects 3, detects 4
- `lz4s`  -- LZ4 sequence format re-tuned for small texts

Consumers: kv, quietcast, quietcasting-go, almanac (local
`replace repani.com/prim => ../prim`).
