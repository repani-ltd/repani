ASCON-KV CLIENT CONVENTIONS
.date 2026-10-12
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem What two ASCON-KV clients agree on above the wire: normative for
.rem the reference clients (Go client package, js/kv.js). The server
.rem sees none of it. The wire is PROTOCOL.t; the reasons are DESIGN.t.

Two clients share a vault only if they share these conventions. The
server neither sees nor depends on any of them: they live inside the
encrypted entries, or on the devices.

# Values

A non-empty entry value is a format byte, then the payload:

.pre
value = fmt(1) || payload
.end

.item `fmt` 0x00 is raw: the payload is the value, byte for byte.
.item No other `fmt` is assigned. A reader that meets one treats the
value as opaque and does not decode it.
.item A tombstone is the protocol's empty value, with no `fmt` byte.
So an empty raw value is 0x00 alone (`value_len` = 1), distinct from
a tombstone.

# Key blobs

Keys travel between devices as Crockford base32 text, upper case:

.pre
0123456789ABCDEFGHJKMNPQRSTVWXYZ
.end

.table 76 12L 6R *L
^blob | length | content
RootKey | 32 | the 20-byte RootKey, no padding bits
RegisterKey | 26 | the 16-byte RegisterKey; the last 2 bits are zero padding
backup | 58 | the RootKey's 32 characters, then the RegisterKey's 26
.end

A decoder ignores spaces, tabs, line breaks and dashes, so a blob
may be shown in groups. It accepts lower case, reads O as 0 and I or
L as 1, and rejects any other character, any other length, and a
RegisterKey whose padding bits are not zero. A 32-character blob
gives a RootKey alone; a 58-character one gives both keys.

# Timestamps

A client gives each entry it writes the current time in Unix
milliseconds, or one more than its previous entry's `ts_ms` when the
clock has not passed that. Its own entries are therefore in write
order, and last-writer-wins follows it.

# The reference sync

PROTOCOL.t leaves the sync loop to the client. The reference clients
run this one.

## Registration

A client that holds a RegisterKey and does not believe the server
has its vault sends a REGISTER first; a caller that knows the vault
exists may tell it so, and save the round trip. One without a RegisterKey
skips it and syncs. When the server answers UNKNOWN_VAULT, a client
with a RegisterKey registers and starts the round again; one without
fails, since the vault does not exist and it cannot create it.

## The loop

.pre
round 0 only: push the entries written since the last sync
PROBE                                     OK: done
anchor = round 0: newest ts_ms in the store, less the window
         later:   zero (the whole set)
PULL from (anchor, zero id), page by page, until a page is empty
PUSH own entries from the same anchor, a page at a time
next round
.end

.item The window is 10 minutes by default.
.item A sync gives up after 64 rounds.
.item Every pulled page goes into the store as one batch before the
next request, so an interrupted sync keeps every page it received.
.item The pull cursor must move strictly forward with each page. A
page that does not move it, or more than 2^20 pages in one walk,
ends the sync with an error.
.item Pushes are grouped into batches of at most PAGE_BYTES of
entries. A push may carry entries the server holds; it drops them.
.item A response must be a RESP for the client's vault, open under
K_s2c, and echo the request's nonce. Its body is refused over
MAX_FRAME_BYTES.
.item A sync that fails keeps the entries it was to push first, ahead
of any written since, for the next sync.

## The view

A client's reads (get one key, list the keys, list every live pair)
answer from the Merge of PROTOCOL.t over its whole store. Entries
that do not open stay in the store and are left out of the view.

# Rotation

To retire a device, create a fresh RootKey on a trusted device, copy
the live keys into the new vault, and give the new RootKey to the
remaining devices. The old vault stays readable to the retired
device but no longer changes.
