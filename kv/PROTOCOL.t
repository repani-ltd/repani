ASCON-KV PROTOCOL, VERSION 1
.date 2026-10-12
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem The wire protocol of repani.com/kv: normative throughout. The
.rem reasons behind it are in DESIGN.t; client conventions above the
.rem wire (value format, key blobs, the reference sync loop) are in
.rem CLIENT.t.

ASCON-KV is an end-to-end encrypted, authenticated key-value store
that a user's devices synchronise through a blind relay server.
Every frame, request and response, is one Ascon-AEAD128 seal under a
key both ends hold. There are no other MACs, no signatures and no
asymmetric cryptography.

The primitives are Ascon-AEAD128 and Ascon-XOF128 (NIST SP 800-232).
XOF(m, n) is n bytes of Ascon-XOF128 over m, and || is
concatenation. All integers are little-endian.

# Version

Every frame begins with a version byte, `ver`. This document defines
version 1, `ver` = 0x01, and does not change. A later version is
defined in its own document under its own `ver` value. A server
answers a frame whose version it does not speak with BAD_VERSION
(see Errors before the channel).

# Keys

A vault has one secret: a random 20-byte RootKey, shared by every
device of the vault. Everything else derives from it:

.pre
AEAD_Key = XOF("kv/aead/" || RootKey, 16)   encrypts entries
VaultID  = XOF("kv/vid1/" || RootKey, 16)   names the vault on the wire
AuthKey  = XOF("kv/auth/" || RootKey, 16)   the channel root
K_c2s    = XOF("kv/c2s0/" || AuthKey, 16)   seals client -> server
K_s2c    = XOF("kv/s2c0/" || AuthKey, 16)   seals server -> client
.end

AEAD_Key never leaves the devices. VaultID is public. The server
learns AuthKey once, at registration, and derives the channel keys
from it.

A server holds one or more RegisterKeys, 16 random bytes each,
given out of band to whoever may create vaults on it. Each yields a
registration key:

.pre
K_reg = XOF("kv/reg0/" || RegisterKey, 16)  seals REGISTER
.end

Every domain prefix is 8 bytes.

# Entries

A vault is a set of entries. Each entry is sealed on its own:

.pre
entry      = ts_ms(8) || nonce(16) || ct_len(2) || ciphertext
header     = ts_ms || nonce || ct_len                     (26 bytes)
plaintext  = key_len(1) || key || value_len(2) || value
ciphertext = AEAD(AEAD_Key, nonce, ad = header, plaintext)
.end

.item `ts_ms` is the time of the write in Unix milliseconds, set by
the writing device.
.item `nonce` is an entry nonce (see Nonces).
.item `ct_len` is the length of the ciphertext: the plaintext plus
the 16-byte tag. A ciphertext shorter than the tag is malformed.
.item The associated data is the whole header, so the tag covers
the timestamp, the nonce and the length.
.item `key` is 1 to 255 bytes. `value` is 0 to 65535 bytes.
.item An empty value is a tombstone: it deletes `key`.
.item A marshaled entry is at most MAX_ENTRY_BYTES (see Limits); a
writer refuses a larger one, and every party rejects one on parse.

An entry's id is

.pre
entry_id = XOF("kv/eid1/" || entry, 16)
.end

over the whole marshaled entry. Equal bytes are the same entry: the
set holds each id once.

# Frame

Every message, in either direction, is one frame:

.pre
frame  = ver(1) || kind(1) || vault_id(16) || nonce(16) || sealed
header = ver || kind || vault_id || nonce                 (34 bytes)
sealed = AEAD(K, nonce, ad = header, payload)
.end

`kind` names the message and the key K that seals it:

.table 70 6L 10L 8L *L
^kind | name | K | direction
0x01 | REGISTER | K_reg | client to server
0x02 | SYNC | K_c2s | client to server
0x03 | RESP | K_s2c | server to client
.end

The associated data is the whole header. `vault_id` is the VaultID
and `nonce` a frame nonce. A frame shorter than its header plus a
tag is malformed. A frame that does not open is rejected and changes
nothing.

# Nonces

Nonces are hedged: a fresh random part and a high-resolution clock,
hashed together.

.pre
frame nonce = XOF("kv/rnon/" || VaultID || u64(t) || random16, 16)
entry nonce = XOF("kv/enon/" || u64(t) || random16, 16)
.end

`random16` is 16 bytes from a cryptographic random generator. `t`
is the current time in nanoseconds since the Unix epoch, as finely
as the platform resolves it. The client makes every K_c2s and K_reg
frame nonce, the server every K_s2c one. Every device makes its own
entry nonces. A nonce travels with what it seals; no one recomputes
it.

# Payloads

.pre
REGISTER = authkey(16) || [flags(1)]
  flags bit 0  EPHEMERAL; every other bit is zero
SYNC     = op(1) || body
  op 0x00  PROBE  body = set_hash(16)
  op 0x01  PULL   body = cursor(24)
  op 0x02  PUSH   body = count(4) || entries
  op 0x03  DROP   body = empty
RESP     = status(1) || req_nonce(16) || count(4) || entries
cursor   = ts_ms(8) || entry_id(16)
.end

.item A REGISTER payload without `flags` sets none.
.item `count` is the number of entries that follow. Entries delimit
themselves through `ct_len`; bytes left over after the last one make
the payload malformed.
.item `req_nonce` is the nonce of the request's frame. A client
rejects a response whose `req_nonce` is not its request's.
.item Only the RESP to a PULL carries entries; every other RESP
has `count` = 0.

# Status

A RESP carries one status. It is sealed, so it is authentic.

.table 76 4L 12L *L
^code | status | meaning
0x00 | OK | accepted; a PULL's page follows
0x01 | DIFFER | PROBE only: the sets differ
0x02 | QUOTA | a PUSH that would exceed a vault limit, or that brings new entries to a vault refusing pushes; a REGISTER of a new vault on a server at its vault limit
0x03 | BAD_REQUEST | the payload is malformed, has an unknown op, carries an entry dated further ahead than TS_SKEW_MS, or is a DROP of a vault that is not ephemeral
.end

# Errors before the channel

Some failures leave the server no key to seal a reply with. It then
answers with ONE unauthenticated byte and nothing else. A RESP is
always longer than one byte, so the two never collide.

.table 76 5L 14L *L
^byte | error | when
0xE0 | BAD_FRAME | the frame is malformed, or a REGISTER payload is not 16 or 17 bytes or sets an unknown flag
0xE1 | BAD_VERSION | `ver` is not one the server speaks
0xE2 | BAD_KIND | `kind` is unknown
0xE3 | UNKNOWN_VAULT | a SYNC names a vault the server does not hold
0xE4 | BAD_AUTH | the frame does not open under any key the server holds for it, or a REGISTER brings a different AuthKey for an existing vault, or names a revoked vault
0xE5 | TRY_LATER | a transient failure; the request may be retried
.end

An attacker on the path can forge or drop these bytes. A client uses
them only to choose what to do next: register on UNKNOWN_VAULT, retry
on TRY_LATER, report anything else. It never changes vault state
because of one.

# Sync

Canonical order sorts entries by `ts_ms`, then by `entry_id` as
bytes, both ascending. A cursor is a position in that order. The set
hash fingerprints a whole set of N entries:

.pre
set_hash = XOF("kv/sset/" || u32(N) || entry_ids in canonical order, 16)
.end

The server keeps no state between requests. The three operations a
sync uses (the fourth, DROP, is under Ephemeral vaults):

.term PROBE(set_hash)
OK if the server's set hash equals `set_hash`, else DIFFER.
.term PULL(cursor)
the entries strictly after `cursor` in canonical order, as many as
fit PAGE_BYTES (see Limits) and always at least one if any remain.
An empty page means nothing lies after `cursor`.
.term PUSH(entries)
the server adds those it does not hold and answers OK, or rejects
the whole frame with QUOTA or BAD_REQUEST and adds none.

A client is in sync when a PROBE answers OK. How it gets there is
the client's own: CLIENT.t gives the reference loop. Any loop that
pulls and pushes until a PROBE matches interoperates, since the
server sees only the cursors and entries a client sends.

# Registration

A client creates a vault with a REGISTER frame sealed under K_reg,
carrying its AuthKey. The server tries each K_reg it holds; one that
opens the frame is the authorisation, and none that opens it is
BAD_AUTH. The server then stores the AuthKey for the VaultID and
answers OK under K_s2c. A REGISTER for an existing vault answers OK
if its AuthKey is the stored one, and BAD_AUTH if not. Creating a
vault fails with QUOTA when the server holds its maximum number of
vaults, and with TRY_LATER when the server cannot store it.

A server may also hold vaults created by its operator without a
REGISTER. A server with no RegisterKey answers every REGISTER with
BAD_AUTH. Its operator may REVOKE a vault: the server then no longer
serves it and answers every REGISTER for it with BAD_AUTH, so a
device still holding the RootKey cannot create it again.

## Ephemeral vaults

A REGISTER that creates a vault may set EPHEMERAL: the vault is for
one exchange, such as handing a key from one device to another, and
ends once it has served. The mark is fixed when the vault is
created; a REGISTER for an existing vault leaves it as it is,
whatever its flags. An ephemeral vault ends in either of two ways:

.item DROP, a SYNC operation any holder of the vault's channel key
may send. The server ends the vault and answers OK. A DROP for a
vault that is not ephemeral is BAD_REQUEST, and changes nothing.
.item EXPIRY: EPHEMERAL_TTL_MS after its creation, the server ends
it, used or not.

Ending a vault deletes its entries and revokes it. A SYNC for it is
then UNKNOWN_VAULT and a REGISTER is BAD_AUTH, so its RootKey opens
nothing again, on any device. An ephemeral vault counts toward the
server's vault limit while it lives.

# Merge

A device's view of a vault is the replay of its whole set in
canonical order: an entry with a value sets its key to that value,
and a tombstone removes its key. Two devices that hold the same set
have the same view.

An entry that does not open under AEAD_Key, or whose plaintext is
malformed, is left out of the view. It stays in the set, so that set
hashes still agree.

# Limits

.table 76 16L 9R *L
^limit | value | meaning
MAX_ENTRY_BYTES | 65536 | the largest marshaled entry
PAGE_BYTES | 262144 | the entry bytes in one PULL page; clients batch PUSHes to it too
MAX_FRAME_BYTES | 1048576 | the largest frame either side accepts
TS_SKEW_MS | 86400000 | default furthest a pushed entry's ts_ms may be ahead of the server's clock; an entry is never refused for being old
EPHEMERAL_TTL_MS | 3600000 | default lifetime of an ephemeral vault, from its creation
.end

PAGE_BYTES exceeds MAX_ENTRY_BYTES, so every entry fits one page,
and MAX_FRAME_BYTES exceeds a full page with its framing.

A server sets per vault MAX_ENTRIES and MAX_VAULT_BYTES, may cap the
number of vaults, and may change TS_SKEW_MS and EPHEMERAL_TTL_MS. It judges a PUSH only by
the entries it does not already hold: pushing an entry again is
never refused.

# HTTP

A client sends each request frame as the body of

.pre
POST <base>/v1/sync
Content-Type: application/octet-stream
.end

where `<base>` is the server's URL, which may carry a path prefix.
The server answers 200 with a body that is either a RESP frame or
one error byte. Any other HTTP status is a failure of the transport,
not an answer. Each side refuses a body over MAX_FRAME_BYTES, and a
client sets a timeout on every request.

# Security considerations

On the wire a frame shows its `ver`, `kind`, VaultID, nonce, size
and timing. The operation, cursors, set hash, entries, statuses and
the AuthKey at registration are all sealed. A server sees the
ciphertext, sizes and timestamps of entries and the rate of writes
per vault, never keys, values or which device wrote.

Whoever holds AuthKey, such as a compromised server, can forge
frames for that vault but cannot read or forge its entries, which
need AEAD_Key.

Whoever holds the RootKey can read the vault and also write to it:
symmetric keys cannot grant one without the other. A vault whose
RootKey is published is read-only only where a server refuses its
pushes.

A tombstone removes a key from the view, not entries from the set.
A set only grows.
.width 48
.cols 2
.font sans
