FACT Stream Profile — DRAFT for SPEC v0.4

Status: draft proposal, not normative. SPEC.t v0.3 remains the source of
truth; nothing here changes the grammar. This document proposes a third profile
— stream — alongside config and projection, and is written to be merged
into SPEC.t as a section once field-tested. Terminology and section references
follow SPEC v0.3.

---

# 1. Purpose and scope

A stream is an append-only, totally ordered journal of events: tool-call
records, state transitions, decision-ledger entries, metric ticks. The profile
targets the low-volume, high-value end of the event regime — journals a human
reads, greps, diffs, and occasionally appends by hand — not the high-volume
end (concurrent writers, aggregation queries), which belongs in a database and
is a non-goal (§9).

The apparent conflict: SPEC §5 defines a file as an unordered set — line
order carries no meaning — while a journal's whole semantics is order. The
profile resolves it without touching the data model: order lives in the
data (a sequence id per event), and the id form is engineered so that
canonical form (§8) coincides with event order. This is the same move §4.1
makes for datetime, where fixed-width big-endian fields make bytewise order
equal chronological order.

# 2. The central invariant: append is canonical

Events are instances of the uniform kind ev, with fixed-width, zero-padded,
letter-prefixed ids:

.pre
ev:e000000000041.type: enum(put|err|note) = put
ev:e000000000041.page: int = 11
ev:e000000000042.type: enum(put|err|note) = err
ev:e000000000042.msg: str = "fetch failed: open-meteo 503"
.end

Because every fact of event N shares a key prefix that sorts bytewise after
every fact of event N-1, and the writer MUST emit each event's facts in
bytewise order, a file appended in id order is already in canonical form.
Appending never re-sorts; the canonical-form check (§8) doubles as the order
check; hash equality, clean diffs, and diff-as-delta all carry over unchanged.
The file remains exactly what §5 requires — an unordered set of
self-contained facts — whose bytes merely happen to also be a chronological
journal.

Stream file rules:

.item A stream file contains event facts only (ev:*), plus at most one
header comment line in the stored form (the §11.1 pattern): # FACT stream.
APPEND ONLY — past lines never change.
Non-event singletons are forbidden in the stream file: they would sort after
ev:* and break pure append. Stream metadata lives in the manifest (§5).
.item The uniform kind is ev, never the event type: a type-as-kind scheme
(put:e41, err:e42) would sort by type first and destroy the
append-is-canonical property. The event type is a fact (§7).
.item Append-only invariant (profile rule): lines, once written, never change.
Hand-appending is supported and validator-checked; hand-editing of past
lines is the corruption this profile exists to prevent. This is the
read-only-lens rule of §11.1 with a different invariant: append-only rather
than regenerate-only.

# 3. Identity: derived, dense, on-disk only

Derivation rule. Streams are single-writer (§9). The id authority is the
file itself: the next id is the last line's id plus one (tail -1, parse,
increment). Ids are derived, not reserved — the id is born in the same act
as the line, so a crash before the write consumes nothing. There is no
counter, no oracle, no coordination.

No placeholder operator on disk. An unresolved-id operator (e.g. ev:@.
meaning "next id") MUST NOT appear in a stored file: a line whose meaning
depends on other lines reintroduces inter-line dependence, the original sin
(§4.3). Tooling MAY accept such sugar as input and MUST store only the
resolved form. Files are always fully resolved, self-contained, and
grep-complete; the CLI is a convenience, never a gatekeeper.

Density rule. Ids within a stream MUST be dense (gapless) from the
stream's first id. A gap is a validator error, not a warning. Rationale,
in order of weight:

.item 1. Integrity without cryptography. The base profile has no hash chain (so
humans can append without tools). Density is the only wholeness check:
last - first + 1 == count, and any gap names exactly what is missing.
Without it, truncation, lost writes, and deleted lines are silent —
the failure mode the format exists to make loud.
.item 2. Totality applied to time. §5 distinguishes absent from asserted-none.
Density does the same for events: a missing id is unambiguously an
anomaly, never "maybe skipped." A journal whose absences are ambiguous is
not a record.
.item 3. Cursor protocol. A consumer caught up through id N knows exactly what
"caught up" means and what to request next. Resumption, replay, and sync
collapse to one integer.
.item 4. It is free. The database argument for gaps (concurrent writers burning
reserved ids) does not apply to derived ids under a single writer. Gaps
cannot occur in correct operation — therefore any gap is definitionally
evidence of loss or tampering, which is what makes the check meaningful.

Redaction preserves density. Erasing an event's content (e.g. a legal
erasure obligation) keeps the id and replaces the facts with a tombstone:
ev:e000000000047.type = redacted (plus a reason fact per vocabulary). Content is
removed; the sequence — and every downstream cursor — survives. Redaction of
a sealed segment is a supersession event recorded in the manifest (§5),
never a silent rewrite.

Derived streams renumber. A filtered or ingested stream cannot stay dense
in the source's numbering. It MUST be dense in its own sequence and SHOULD
carry provenance as a fact: ev:e000000000012.src: str = "main:e000000000047".
Density is per-stream, always.

# 4. Id form

.item Marker id: letter prefix + fixed-width zero-padded decimal
(e000000000042), satisfying §3 segment rules (letter-first).
.item Width is declared in the manifest and constant for the stream's lifetime.
RECOMMENDED default: 12 digits (10^12 events). Width exhaustion is a
validator error; rollover does not exist — start a successor stream and
record the succession in the manifest.
.item Fixed width is load-bearing: it is what makes bytewise order equal numeric
order (§2). A validator MUST reject ids of nonconforming width.

# 5. The manifest

A sibling config-profile FACT file (<stream>.manifest.fact, ordinary §5
totality rules) carries everything the stream file must not:

.pre
stream.id: str = "session-a078edfe"
stream.width: int = 12
stream.vocabulary: str = "docs/session-vocabulary.md"
stream.sealed: bool = false
segment:s0001.file: str = "session.0001.fact"
segment:s0001.first: str = "e000000000001"
segment:s0001.last: str = "e000000004096"
segment:s0001.sha256: str = "9f2c..."
segment:s0002.file: str = "session.0002.fact"
segment:s0002.first: str = "e000000004097"
segment:s0002.last: str? = none
segment:s0002.sha256: str? = none
.end

.item Rotation: when a segment reaches its size bound, seal it — record
last and sha256 — and open the next. A sealed segment is an immutable,
verifiable constant: regenerate-and-compare or pin by hash, the §11.5
trust model reapplied. The active segment is the only appendable file.
.item Density and width are validated per segment and across segment boundaries
(segment:sN.first == segment:s(N-1).last + 1).
.item The manifest is hand-editable config; the stream files are not. The
division of labor is deliberate: policy and metadata in the editable layer,
history in the append-only layer.

# 6. Sealed variant: hash-chained streams

For audit-grade journals (decision ledgers, financial trails) the profile
defines an OPTIONAL variant: each event carries
ev:eN.prev: str = "<sha256 of event N-1's canonical lines>".

.item This deliberately introduces generated inter-line dependence and therefore
makes the stream tool-mediated. That is fundamental, not a format weakness:
hand-editable tamper-proofing is a contradiction in any format. The choice
is per-stream, declared in the manifest (stream.sealed: bool), and the
base profile remains chain-free and hand-appendable.
.item Chain verification composes with segment hashes: the manifest pins segments,
the chain pins intra-segment order and content.

# 7. Event vocabulary

.item Every event asserts ev:eN.type: enum(...) — the enum restated per use
site per §4.3.
.item Per-type facts follow conditional totality: for each type, a documented
fact set, all asserted when the type matches (the "dispatch-only facts"
config pattern). Vocabularies MUST be documented (§5 completeness rule for
generators applies to stream writers).
.item Timestamps are decoration; the sequence is the authority. Events MAY
carry at: datetime (second precision) and, where sub-second resolution
matters, at_ms: int (epoch milliseconds) per the §4.3 key-name
convention. Ordering claims rest on ids alone; fractional-second datetime
is not relitigated (Appendix A).
.item The content boundary (§7) stands. Prose and blob payloads live in
sibling files or archive members referenced by a str fact
(ev:eN.body_file), never as escaped one-line strings. Streams carry the
fact-like skeleton of events; content is content.

# 8. Validation additions

Draft codes, renumber on merge:

.table 5L 22L *L
Code | Condition | Example message
S001 | Id gap | stream: gap after e000000000046 (next is e000000000048)
S002 | Id width mismatch | line 12: id "e42" does not match declared width 12
S003 | Out-of-order event | line 30: e000000000041 after e000000000042 (append order violated)
S004 | Non-event fact in stream file | line 1: singleton "stream.id" belongs in the manifest
S005 | Sealed-segment mismatch | segment s0001: sha256 does not match manifest
S006 | Chain break (sealed variant) | e000000000042.prev does not hash e000000000041
.end

S003 is subsumed by the canonical-form check but deserves its own message:
in this profile non-canonical means history was reordered, which is a
different accusation than "run fmt." Duplicate ids need no new code — they
are E007 duplicates already.

# 9. Non-goals

.item Multi-writer streams. One writer per stream; partition by writer
(per-agent, per-station, per-subject) and merge at read time by
(stream, id). Global total order across writers is a consumer concern.
.item Aggregation. Anything needing sums, joins, or windows projects into a
database; the stream is the record, not the query engine.
.item High volume. The profile's ceiling is "a human might grep this." Beyond
that, the stream is the wrong layer regardless of format.

# 10. CLI surface (sketch)

.pre
fact append <stream> <facts...>   resolve @ ids, emit sorted, atomic append
fact verify <stream|manifest>     density, width, order, seals, chain
fact seal <stream>                close active segment, hash, open next
fact follow <stream>              tail -f with per-event framing
.end

append and seal are conveniences over rules a careful hand can follow;
verify is the enforcement. Reading requires no tool at all.

---

# Appendix — Design decisions ledger (this profile)

Tested against the discussion of 2026-08-17 and rejected (do not
re-litigate without new evidence):

.table 26L 14L *L
Proposal | Verdict | Reason
Unresolved id operator on disk (ev:@.) | Rejected | Inter-line dependence — the original sin; sugar is CLI input only, disk is always resolved
Event type as marker kind (put:e41) | Rejected | Sorts by type before id; destroys append-is-canonical
Stream metadata as singletons in the stream file | Rejected | Sorts after ev:*, breaking pure append; manifest owns metadata
Sparse (gaps-allowed) ids | Rejected | Silent loss becomes undetectable; density is free under derived ids; derived streams renumber instead
Mandatory hash chain | Rejected | Would make every stream tool-mediated; chain is a per-stream opt-in for audit-grade use
Id rollover on width exhaustion | Rejected | Rollover breaks bytewise order; successor stream + manifest succession record
Fractional-second datetime for event times | Not relitigated | Appendix A rejection stands; ids are the ordering authority, at_ms: int covers resolution
.end

Open items for the draft: redaction protocol details for sealed segments
(supersession record shape); merge tooling for per-writer stream families;
whether ev should be spec-fixed or manifest-declared; first field test
(candidate: the session.fact journal of the agent-supervision console).
