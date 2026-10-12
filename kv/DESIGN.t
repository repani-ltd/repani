ASCON-KV DESIGN
.date 2026-10-12
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem Why ASCON-KV is the way it is: the reasons behind PROTOCOL.t and
.rem CLIENT.t, the decisions taken, the known gaps, and what is not
.rem built yet. Not normative: where this and a spec differ, the spec
.rem holds. History lives in git, not here.

# One envelope

Once two parties share a key, a secure protocol is an authenticated
encrypted envelope. So every frame is one Ascon-AEAD128 seal, and
the protocol needs nothing else: no MACs beside it, no signatures,
no handshake, no asymmetric keys. One primitive family, Ascon (NIST
SP 800-232), does encryption, authentication, hashing and key
derivation.

The associated data is always the WHOLE cleartext header, for frames
and for entries alike. One rule binds every routing byte: a sealed
body cannot be moved to another vault, turned around to the other
direction, or given another version. In an entry it binds `ts_ms`,
so a relay cannot re-date an entry to change who wins a merge. The
nonce is in the header too, which makes that binding redundant there,
and harmless.

Entries pushed in a frame are sealed twice: each under AEAD_Key,
inside, and the frame under its channel key, outside. The inner seal
keeps contents from the server; the outer one keeps entry sizes,
counts and timestamps from anyone watching the wire. It costs one
more Ascon pass over a small body.

# Keys

The RootKey is 20 bytes, 160 bits: above the 128-bit strength of
every derived key, and exactly 32 Crockford base32 characters with
no padding bits.

Each direction of the channel has its own key, so each has its own
nonce space and one generator: the client owns client-to-server
nonces, the server owns the others. Neither has to coordinate.

The server holds AuthKey and never AEAD_Key. A leaky server can forge
frames for a vault, which at worst disturbs a sync; it can never read
an entry or forge one a client would accept.

A server holds several RegisterKeys so that each application (iou is
one) has its own, and revoking one leaves the rest and every existing
vault untouched. Nothing on the wire says which key a client holds.

# Nonces

A nonce is hedged, random and clock together, so no single failure
repeats one. The random part gives uniqueness; the clock covers a
generator that repeats, as after a fork or at early boot.

Entry nonces matter most. Every device of a vault seals entries
under the same AEAD_Key with no coordinator, so two equal nonces
from two devices would leak the XOR of two secret values. The random
part keeps devices apart; the clock covers one device whose
generator has failed. The clock is read in nanoseconds because
milliseconds alone would repeat within one millisecond and void the
hedge.

# Errors before the channel

When a frame does not parse or does not open, the server has no key
to seal an answer with. It sends one plain byte. Those bytes are
hints, and safe as hints, because none of them leads a client to
change its vault: the worst a forged one does is delay a sync, or
cause a REGISTER that repeats a known AuthKey under a K_reg the
forger does not hold.

# Sync

The server keeps no session. It answers two stateless questions,
"what comes after this cursor" and "add these", and one equality
test, the set hash. So a steady-state sync is one fixed-size PROBE
and its answer, whatever the size of the vault.

The reference loop walks only a recent window first. A concurrent or
slightly back-dated write lands just behind the newest entry, so
looking back a little catches it without a full walk; a wider gap
falls to the full walk of the next round, which always converges.
The window never reaches the wire, so any choice interoperates.

The push is tolerant of duplicates: the client re-sends the window
and the server drops what it holds. The client therefore keeps no
record of what the server has.

After a local write the client pushes just the new entries and
probes. When those were the whole difference the probe matches, so
writing one key moves one entry, whatever the vault's size. The other
common case is the mirror image, another device having written: the
pull brings the difference, so the client probes again before it
pushes, and a match spares sending the window back. The probe is one
fixed-size frame; the push it saves is the whole window.

On the devices, an entry is hashed once, when it enters the store,
which keeps the canonical order as entries arrive; a read decrypts
only the entries the store gained since the last read. The cost of a
write or a read is therefore what changed, not the vault's size. What
still grows with the window is the pull after a difference: entries
written within it come down again, and are hashed to be recognised.
Catalogue reconciliation (Not built yet) is the answer if that cost is
ever measured in practice.

The loop converges because a pull walked to an empty page leaves the
client holding every server entry in the window, and the push leaves
the server holding every client entry in it. Once the window spans
the difference the probe matches, and a window from zero spans
everything. Every page is committed as it arrives, so an interrupted
sync loses nothing.

Recovery is not a mode of its own. A new or wiped client has no
entries, so its first walk pulls everything; a wiped server is
refilled by a client's push. A server that lost its data but kept
its RegisterKeys lets each client register again under the same
AuthKey.

A client bounds its own sync: a round budget, a cursor that must
advance, a page backstop. A hostile relay can stall it but cannot
make it loop, and by the merge and the double seal it can neither
read nor corrupt the data.

# Versioning

The `ver` byte at the head of every frame is the version mechanism.
A protocol change gets a new `ver` value and its own document; a
server can speak several versions side by side, and answers any
other with BAD_VERSION. A version, once defined, does not change.

# Values

A value carries one format byte so that a client can tell later
encodings from raw bytes without a protocol change. Only raw is
assigned. Reservations for formats nobody uses (a typed document
format, compression) were taken out: a code point is assigned when
its first consumer arrives. Compression in particular would be a
security decision, not a format one: compressed plaintext leaks
through ciphertext length, and the server sees every length.

# Registration and rotation

There is no registration without a RegisterKey: a vault is created
by someone the operator authorised, or by the operator directly.

There is no in-band key ratchet. A deterministic one would hand the
retired key's holder the next key; a random one would still need the
out-of-band channel to carry its entropy. Rotation is therefore out
of band: a fresh RootKey, a fresh vault.

# TLS

TLS is optional. Everything of substance is sealed, including the
AuthKey at registration, so TLS adds only the metadata: which vault,
when, and how much. The leak it would guard against elsewhere, a
channel key captured in clear, cannot happen here.

# Publishing a RootKey

Symmetric cryptography cannot express read without write. Whoever
can derive AEAD_Key can seal entries, and whoever can derive AuthKey
can push them. A published, read-only vault is read-only by server
policy alone: the server refuses its pushes. Publish a RootKey only
where every reader is trusted not to write, or where that one server
is the only path entries travel.

# Server

## Quotas

The log store keeps every entry in memory as well as on disk, so the
quotas are a memory budget before they are a disk one. The defaults
are sized for a small host: 10,000 entries and 8 MiB per vault,
1,000 vaults. 8 MiB is also what someone holding a leaked RootKey
can fill a vault with: junk entries the clients cannot open but the
server counts. Recovery from that is rotation.

## One server lock

One mutex guards the map of vaults. Creating a vault holds it across
the disk work (directory, auth key, log open), so while a vault is
being created every other request waits. That is deliberate: the
check that a vault is new and its insertion must be atomic, or two
REGISTERs for one new vault would both build a store and race for
one log. Registrations are rare, so the cost is nil today. If they
become frequent, reserve the map slot with a placeholder holding its
own lock, release the server lock, and do the disk work under the
placeholder.

## The log

A vault's log is append-only: a magic header, then length-prefixed
entries, each write synced before the next. On open, a record that
does not parse can only be a torn tail, since every committed record
was synced before the next one began; it is cut off, and sync
fetches whatever it held.

The log takes an exclusive flock. POSIX record locks do not exclude
a second descriptor in the same process and are all dropped when any
descriptor of the file closes; flock belongs to the open file, so it
refuses a second open in-process too. flock does not exclude a
writer on another machine, so the data directory stays on a local
disk.

## The store is protocol, not a primitive

The store's identity of an item (the entry id), its order (canonical
order) and its fingerprint (the set hash) are all protocol
definitions. It is the protocol's entry set made durable, not a
general log, and it stays in kv. A general log set is worth
extracting only when a second protocol needs one: two consumers
shape an interface, one does not.

# Dependencies

Beyond the standard library and this module's ascon and kiosk, kv
admits two packages of the Go project, each for one job:
golang.org/x/term, for the CLI's table width on a terminal, and
golang.org/x/sys/windows, for the entry log's lock on Windows (flock
covers Unix). kv.js's tests drive Chrome through this module's
chrometest.

# Known gaps

.item No real deletion. A tombstone hides a key; no operation removes
entries from a set, and nothing compacts one. A set only grows.
.item No bound on the whole population: every quota is per vault, so
1,000 full vaults hold 8 GiB whatever one vault's cap. A total-bytes
limit across vaults would close it.

# Not built yet

Two designs, both learnt from the Fossil SCM sync protocol, which is
the same design as kv's (a set of content-addressed artifacts
reconciled through a stateless server) with two decades of use.
Neither is implemented, promised or given a wire code.

## Catalogue reconciliation

The problem: a walk finds differences by moving whole entries. The
pull re-sends entries the client holds and the push re-sends the
window, so a one-entry difference back-dated below the window can
cost the whole vault in transfer.

Fossil exchanges bare ids first and then asks for exactly what is
missing. For kv that is two more SYNC operations: IDS(cursor), a page
of `ts_ms(8) || entry_id(16)` pairs after the cursor, about 2,700 to
a 64 KiB page; and GET(ids), the whole entries for listed ids within
the page budget. The client walks IDS to hold the server's catalogue,
compares it with its own store, GETs what it lacks and PUSHes what the
server lacks. The server stays stateless; both operations read what it
already indexes. Fossil's cluster artifacts are not needed: canonical
order and a cursor page the catalogue already.

Build it when a full walk is a measured cost, on large vaults that
diverge below the window. The steady state and the fast path do not
change.

## Purge and shun

The problem: deleting entries locally does not stick in a replicated
set, since the next sync brings them back from any device that still
holds them. So a secret written by mistake cannot be removed, and a
vault cannot shrink.

Fossil's answer is the shun: delete the artifact and remember its id,
refusing it when offered again. For kv that is a PURGE(ids) SYNC
operation, by which the server deletes entries (sealed under K_c2s,
so only a channel-key holder can purge), and a shun entry: an
ordinary sealed entry under a reserved key, holding the shunned ids.
It replicates like any entry; each device then drops shunned entries
from its log, its set hash and its pushes, so a lagging device learns
the shun before it can push the corpse back.

Open questions: whether PURGE needs the shun entry to exist first,
how a shun entry is itself ever compacted, and how the timestamp skew
treats replaying old shun entries on a new device.

Build it when a vault nears its quota, or the first time a secret
must be removed from every device and the server. It is the more
important of the two: it is a capability kv lacks outright, and every
long-lived replicated set comes to need it.
