SEAL -- SEALED REQUEST AND RESPONSE
.date 2026-10-20
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem The design of repani.com/seal, before any code. Not normative: on
.rem ratification the wire becomes SPEC.t and this document keeps the reasons.
.rem Nothing here is built.

Seal is a request and response protocol between server processes that share a
symmetric key. Each request and each response is one Ascon-AEAD128 seal; a
request is accepted only if it is authentic, timely, and newer than everything
its sender has sent on the same lane. It takes the place of bearer tokens such
as JWT for a service calling a service, at the rates a person waiting on a page
makes, not millions a second.

Its first consumer is to be hosa (ginko), when its model process leaves the host
for a bank's own model server. Until then hosa's model speaks HTTP over a Unix
socket, where file permissions are the authentication, and seal is not needed.

# Where it comes from

Two protocols already here, each contributing what it does well.

.item FROM KV (kv PROTOCOL.t): the sealed frame. A fixed binary header that is
the associated data, a payload sealed under a key per direction derived from one
root key, hedged nonces, single-byte errors before a channel exists, and the
response echoing its request's nonce.
.item FROM QUIETCASTING (quietcasting SPEC.t, Time and the authenticity claim):
replay by time, with no saved state. Each message carries its sender's clock; a
receiver keeps the newest clock it accepted and refuses anything not newer; a
timeliness gate bounds how far a clock may be from the receiver's; doubt fails
toward refusal.

# Against bearer tokens

.item NOTHING IS PARSED BEFORE IT IS AUTHENTICATED. JWT's worst failures came
from trusting its own JSON header before verifying: `alg: none`, an asymmetric
algorithm swapped for HMAC, key ids steering the verifier. Seal's header is
fixed-size binary with no algorithm field (the version byte fixes the
algorithm), and the payload, JSON or anything else, is read only once the seal
opens.
.item THE WHOLE MESSAGE IS PROTECTED, NOT A TOKEN BESIDE IT. A stolen bearer
token works on any request until it expires; a captured sealed request is one
request, and the replay rule refuses it again.
.item RESPONSES ARE BOUND TO REQUESTS, through the echoed nonce.
.item REVOCATION IS IMMEDIATE: delete a client's line from the server's
configuration and its next request fails.
.item A PROXY SEES CIPHERTEXT, even where TLS ends before the application.

What it gives up, and why that is acceptable here:

.item ONLY THE SERVER CAN CHECK A REQUEST. Symmetric keys mean the verifier
could also forge. With one server holding its clients' keys, nothing is lost;
were several independent services to check one credential, a signature scheme
would be needed, and seal is not it.
.item A KEY IS AS SAFE AS ITS HOST. Clients are server processes whose keys sit
in configuration on the bank's own hosts. Seal is not for browsers or phones,
where a key in page code is exactly as exposed as a JWT in local storage.

# Keys

A client has one secret, a random 20-byte ROOT KEY, held by the client process
and by the server. Everything else derives from it, as in kv and quietcasting,
with CXOF(z, m, n) n bytes of Ascon-CXOF128 over m under the customization label
z:

.pre
ClientID = CXOF("seal-id",  RootKey, 16)   names the client, public
K_c2s    = CXOF("seal-c2s", RootKey, 16)   seals client -> server
K_s2c    = CXOF("seal-s2c", RootKey, 16)   seals server -> client
.end

The labels are seal-id, seal-c2s, seal-s2c and seal-non; there are no others.

The server's configuration has one line per client: a label, the root key, and
the client's number of LANES. Rotating a key is a new line, a reload, and
deleting the old line.

# Frames

.pre
request  = ver(1) || kind(1) || client_id(16) || lane(1) || stamp(8) || nonce(16) || sealed
response = the same header, kind RESP, the request's lane and stamp,
           the server's own nonce, then sealed
header   = everything before sealed                       (43 bytes)
sealed   = AEAD(K, nonce, ad = header, payload)
.end

.item `stamp` is the sender's clock as Q32.32 seconds since the Unix epoch,
quietcasting's form. A client never sends two requests on a lane with the same
stamp; the fraction bits make that free.
.item `nonce` is hedged, as kv's frame nonce: CXOF under seal-non over the
client id, a high-resolution clock and 16 random bytes. Strict stamps already
make it unique per key; the hedge keeps it unique if a clock misbehaves.
.item A request's payload is the application's. A response's payload is
`status(1) || req_nonce(16) || body`, the client accepting it only if it opens
under K_s2c and `req_nonce` is its request's.

# Acceptance

The server judges a request in this order, and does no work for it until every
step passes:

.item The frame is well-formed and its version and kind known, and the client id
is configured; otherwise one unauthenticated error byte, as in kv.
.item The seal opens under the client's K_c2s; otherwise BAD_AUTH.
.item The lane is below the client's configured number of lanes.
.item TIMELY: the stamp is within SKEW of the server's clock (two minutes, say),
and later than the moment this server process started.
.item NEWER: the stamp is greater than the newest stamp accepted on this
client's lane, the lane's MARK.
.item The mark becomes the stamp, before the request is handled, so a replay
arriving during the work is refused too.

A request failing the last three gets a sealed STALE: the client restamps and
resends at once. One that cannot be queued gets a sealed BUSY. Other statuses
are the application's, in its body.

The server's whole replay state is an array of marks per client, a lane each, in
memory. Nothing is saved: at start every mark is the start time, so a request
captured before a restart is not timely, and one stamped after it is above a
mark only if it was never accepted, which a replay by definition was.

# Lanes

A lane is a sequence of requests with at most one outstanding: the client sends
the next only after the previous one's response, or after giving up on it.
Within a lane, stamps cannot arrive out of order, so the strict rule never
refuses an honest request. Concurrency is more lanes: a worker with eight
sessions in flight uses lanes 0 to 7 under one root key.

The number of lanes is fixed per client in the server's configuration, never
chosen by the client: lanes a client could invent would grow the server's state
without bound, and a mark forgotten to bound it would let that lane's old
requests replay.

A client that gives up on a request and resends it restamps it. If the first
copy is still being worked on, the server works on both: that is a duplicate,
not a replay, and an application whose requests must not run twice carries its
own request id in the body.

# Transport

One HTTP POST per request, the frame as the body, as kv does; seal does not
depend on HTTP and runs over any exchange of byte strings. TLS is advisable for
the metadata (client ids, sizes, timing), not for the content.

# Decided

.table 10L *L
^date | decision
2026-10-20 | Symmetric Ascon seals rather than JWT-style bearer tokens: nothing parsed before it is authenticated, no algorithm field; throughput is not a goal.
2026-10-20 | Clients are server processes only. End users authenticate to those systems as they already do.
2026-10-20 | Replay by quietcasting's strict rule, kept per (client, lane), with marks in memory and none saved.
2026-10-20 | Lanes, fixed per client in the server's configuration, in place of a window of recently seen stamps (the IPsec approach, considered first): the quietcasting rule unchanged, a fixed array of marks, nothing ever forgotten.
2026-10-20 | A general protocol in the public module, named seal.
2026-10-25 | Derivations by Ascon-CXOF128 under labels, as quietcasting, in place of 8-byte XOF prefixes; kv version 1 moved to the same construction the same day.
.end

# Open questions

.item SHARING CODE WITH KV. Seal's header differs from kv's (client and lane and
stamp, where kv has a vault id), so the formats stay separate; whether a common
frame package earns its place is decided when seal is built, kv's rule applying:
two consumers shape an interface.
.item SHARING CODE WITH QUIETCASTING: nothing, as of 2026-10-20. What they share
is already a package, repani.com/ascon. Beyond it the constructions differ
(quietcasting's deterministic SIV over Ascon-CXOF, which byte-stable broadcast
slots need; seal's Ascon-AEAD128 with nonces, as kv), and so do the time rules
(quietcasting's TESLA gate against an interval's end, seal's window around the
server's clock): one idea, not one function. The common code would be the Q32.32
clock format, a line or two, too little for a package; and quietcasting is
private while seal is public, so a shared package would have to go public ahead
of quietcasting. Reconsider if seal comes to need quietcasting's chain or SIV.
.item THE NUMBERS: SKEW, the largest frame, and the clock bound a server must
hold to (NTP-disciplined hosts assumed), set from the first consumer.
.item STREAMING, chunks of one response each sealed under the request's nonce
and a counter, waits for a consumer; hosa does not stream.
