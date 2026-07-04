# Changelog

All notable changes to ASCON-KV are recorded here. Versions follow
semantic versioning; the protocol version carried on the wire is tracked
separately in `PROTOCOL.txt`.

## v1.0.0 — 2026-07-04

First official release. End-to-end encrypted, authenticated key-value
store synchronised across a user's own devices through a blind relay
server. Wire/on-disk format: protocol v1.

### Cryptography
- Single primitive family: Ascon-AEAD128 (encryption + authentication)
  and Ascon-XOF128 (hashing + key derivation), NIST SP 800-232. No
  asymmetric crypto.
- The `ascon` implementation is verified against the official ascon-c
  Known-Answer Test vectors (1025 XOF + 1089 AEAD) vendored under
  `ascon/testdata/` and run by `kat_test.go`.
- Keys are derived from one 32-byte RootKey: `AEAD_Key` (device-only,
  encrypts entries), `VaultID` (public handle), `AuthKey` (shared with
  the server at registration) and, from it, directional channel keys
  `K_c2s` / `K_s2c`. A server-side `RegisterKey` gates vault creation via
  `K_reg`.
- Nonces are hedged (random + high-resolution timestamp) so a single RNG
  failure cannot cause reuse.

### Protocol
- Every frame — request and response, both directions — is one AEAD seal
  under a shared key; there are no separate MACs. The cleartext header
  (`ver | kind | vault_id | nonce`) is the associated data, binding
  direction, routing, and version; responses echo the request nonce to
  prevent replay-stitching.
- Failures before a channel key exists return a single unauthenticated
  pre-channel byte (`BAD_FRAME`, `UNKNOWN_VAULT`, `BAD_AUTH`, …).
- Registration is gated (no permissionless/TOFU path) and idempotent.

### Sync
- Stateless, paged reconciliation: `PROBE` (whole-set hash), `PULL`
  (cursor-paged), `PUSH` (idempotent, content-addressed). The server is a
  pure range-query + add relay and keeps no session.
- Fast path: after a local write the client pushes just the new entries
  and probes — a match ends the sync with no pull and no walk, so a
  single `put` on a large vault moves a single entry.
- Otherwise a windowed reconcile (look-back below the frontier, default
  10 min, `KV_SYNC_WINDOW`-tunable) widens to a full walk if needed.
- Loop protection: bounded rounds, a strictly-advancing pull cursor, and
  a page-count backstop — a misbehaving relay cannot hang the client.
- Recovery is not a special mode: a fresh/wiped client rebuilds from the
  server, and a wiped server rebuilds from a client, via the same loop.

### Entries & storage
- Content-addressed entries: `ts_ms(8) | nonce(16) | ct_len(2) |
  ciphertext`; plaintext `key_len(1) | key | value_len(2) | value`.
  `MAX_ENTRY_BYTES` = 64 KiB. Last-writer-wins merge; empty value =
  tombstone.
- Client value convention (`CLIENT.txt`): `fmt(1) || payload`, `0x00` =
  raw, reserving room for future encodings.
- Append-only on-disk log with a magic header that rejects a stale
  format.

### CLI (`kv`) & server (`kvserve`)
- `kv`: init/pair, put/get/del/ls, sync, import/export (CSV), backup/
  restore, search (alias `s`), and `set-register-key`.
- `del` is `rm`-style: it refuses an absent key (exit 1, no dead
  tombstone) unless `-f/--force`.
- Verbose sync tracing and stats: `-v`/`--verbose` (before or after the
  subcommand) or `KV_VERBOSE`, printing per-frame traffic
  (REGISTER/PROBE/PULL/PUSH) and a bytes/entries summary.
- `kvserve`: blind relay with per-vault quotas and skew checks; plain
  HTTP or TLS (file cert or autocert). Deploy preserves vault data by
  default (`WIPE_DATA=1` for a one-time reset).

### Testing
- Unit + integration suites cover the crypto KATs, the wire codec, the
  paged sync/merge (including bidirectional divergence, LWW, delete and
  backdated propagation, recovery, and convergence under concurrent
  writers), loop protection, and the delta-window / fast-path behaviour.
  The full suite passes under the race detector.
