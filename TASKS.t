REPANI -- TASKS AND PARKED WORK
.date 2026-09-03
.rem Repo-wide task ledger. One entry per task, as a .term: the
.rem label names it, the text says what, why, and what triggers
.rem it. Done tasks are removed, not ticked; the decision they
.rem produced lives in the project's DESIGN.t or SPEC.t. Order
.rem within a section is by expected sequence, not priority.

# The typeset family

Candidates to move under repani.com/typeset, one package per
member, every member under the primitive rule (see CLAUDE.md).
Assessed 2026-09-03 after tab and stylebook moved; the ledger
for the family is pica/DESIGN.t after §13.

.term pdf writer, in three packages
Move pica/pdf as typeset/pdf, the writer core, with a face
interface and its one TrueType implementation; pica/pdf/ttf as
typeset/pdf/ttf, unchanged; and the five embedded font files
(JuliaMono and Fira Sans, 1.8M, parsed at package init) as
typeset/fira and typeset/juliamono, which register the faces. A writer that names only a standard font then
imports the core and carries no font data, and the closed font
enum opens: a face is registered, not enumerated. API change
for press and the CLI: pdf.Sans and kin become the face packages' names,
Measure takes a face. Trigger: the first second importer of the
writer, or the fira split being wanted for binary size.
.term standard fourteen faces
A second face implementation for the PDF standard fonts: Type1
dictionary, one-byte WinAnsi text, width tables carried by the
package (Courier is fixed at 600; Helvetica and Times need four
tables each). Cannot set Greek, so no page written today wants
it. Trigger: a consumer that does; the face interface should be
shaped by the TrueType case alone until then.
.term breaker and hyphenation
pica's wrap.go and hyphen.go with the embedded pattern sets
(patterns/, 40K), as typeset/wrap or similar, imported back by
the pica root as it imports tab. Trigger: a tessera panel that
fills a paragraph from a template, or any second consumer of
line breaking.

# Elsewhere

.term raster is due a second design, not a list of fixes
RASTER.t is dated 2026-09-14 and is not frozen, so the window for
everything below is open now and shut after. A session on
2026-09-16 turned up enough interacting questions in both halves
of the format that they are one decision rather than a queue.
In the binary:
.item The width. Forty is fixed in the spec and "a second width
is a second format", but the record's length field is six bits
and already admits 63, so a wider board costs no byte. Real
boards run wider; Milan Centrale's more informative ones are
near 60. The choice is one fixed number or none at all, the
width declared by the enclosing format as the row count already
is.
.item Canonical bytes are not an update; its own term below.
.item A blank cell has two byte forms, 0x00 unwritten and 0x20
filled, so one picture has two encodings, and a blank cell's
foreground is encoded but immaterial. Normalising both is the
difference between promising that identical content gives
identical bytes and promising that identical appearance does.
.item An unassigned glyph counts as written and extends a row's
length. Right, and unsaid.
.item The reader accepts any stream, and the canonical rules
bind the writer only. Unsaid.
In the source:
.item A bare .fill in an alias body, used where a column is
pending, makes a wrong raster three different ways and never
errors: after .at 0 10 it paints the whole row, and after .col
10 or .col 0 the body splits across two rows, leaving a stray
painted bar. A body carrying .fill owns its row, and the use
should be refused.
.item Right-trimming and .fill were both examined and both
hold. Trailing spaces are the one construct that is potent and
invisible, the toolchain deletes them (core.whitespace carries
trailing-space and fix), and a banner of literal spaces cannot
follow a width that moves. What is worth reopening is .fill's
four bare positional numbers, the least legible thing in the
language.
.item Edge-relative placement, a centred run, is justified only
if the width stops being fixed, and then only for what an alias
body cannot express, since a generating tool computes its own
positions. Aliases do expand at non-zero columns, so the case is
real.
.item Scrolling a field too long for its column, which is what
real boards do instead of truncating, needs nothing from the
format: the layer above precompiles the window positions and
sends each as a whole row. That layer does not exist; tessera is
in the attic.
Trigger: ratification of the width, which is the decision the
rest hang from, and which must come before RASTER.t freezes.

.term canonical bytes are not an update: the stale row
Bytes omits blank rows (RASTER.t, "Rows"), so a raster's
canonical bytes are correct only against an empty raster. Fold
them onto a receiver holding an earlier version and every row
the new version blanked keeps its old content, silently: a
withdrawn delay notice stays on the board until something else
writes that row. Measured 2026-09-16 on two sources one row
apart, where the second's bytes are the first's minus row 1
rather than the first's plus a clear, so row 1 survives the
fold. The zero-length record is the only way to clear a row and
nothing in the tree emits one, so the update model the spec
states -- a producer sends the rows it owns, whole, and a reader
replaces them -- has no producer.
Two resolutions. Either the producer holds its previous raster
and emits a stream by comparison, so clearing is its
responsibility and the format is untouched; or the format states
that an update is taken against a known prior raster, and the
package grows the call that makes one. Recommendation: the
first, with a line in RASTER.t saying canonical bytes are a
snapshot and not an update, since it keeps "nothing above the
row is defined here" true and costs the format nothing.
Trigger: before the first raster goes out over quietcasting, or
before any second consumer folds a stream.

.term quietcasting binding of raster
Raster's row record (RASTER.t, "Rows": up to 82 bytes, a row
whole) is the unit the radio carries, unchanged: a slot holds
whole records, packed first-fit by a scheduler, and a receiver
folds every slot's records in slot order, a replaced slot
clearing the rows it no longer carries. On the trial 40-column
boards a departures screen packs into seven 238-byte slots and
a weather screen into eight, so a sixteen-slot carousel carries
two screens. Open: which rows a station carries (a screen is 24
rows; the row index has room for ten), a row published in two
slots (a publisher error, or slot order decides), and whether a
compact per-row body (glyphs then ink runs) is ever wanted; it
is not now. Tessera, the former binding, is in the attic
(~/repos/_attic/tessera, 2026-09-14). Trigger: the first raster
to go out over quietcasting.
.term pictograms redrawn from Noto Sans Symbols
JuliaMono draws its pictograms (sun, moon, cloud, the marks ● ○ ★
✓ ✗, the faces) at about two thirds of cap height, which is why
the weather set was withdrawn (2026-09-14). The icons that read
well on limasoul.com are a system fallback (Apple Symbols; Fira
Mono lacks them), which differs by platform and drifts a row by
its own advance. Tried the same day and judged very nice: the
embedded JuliaMono subset with those glyphs redrawn from Noto
Sans Symbols and Noto Sans Symbols 2 (OFL, like JuliaMono),
each scaled to fill a 0.56 by 0.74 em box on the baseline at
the 0.6 em advance, so they are full height, exactly aligned,
embedded and identical on every screen (~/repos/tmp/geometry:
composite2.py, out/symbols.html, fonts/julia-noto.woff2). Not
adopted yet. When it is: the weather set returns to the table
(appended, not at its old codes), the subset carries the Noto
outlines for it and for the marks, and fonts.go records the two
donors and the box. Trigger: the first board that wants a
pictogram.
.term QR codes: a primitive and a CLI verb, not a command
A raster carries a QR code as half blocks, one module a column
and two a row on a white bar with a two-module quiet zone
(RASTER.t, "Cells"; a version-2 code is 29 by 15 cells and
scans from a screen, tried 2026-09-14). The encoder is a
primitive, typeset/qr: text in, rows of ▀ ▄ █ out, standard
library only, known-answer tests against a reference encoder;
composers call it and emit the rows as content, and "raster qr
TEXT" prints the source lines for a hand author, with a .rem
beside them naming the target. Decided against a .qr command:
the language computes nothing, and a command would pin version,
error level and mask selection in the spec for every
implementation. Trigger: the first board that carries a code.
.term trudge as a primitive
trudge imports ascon and sits outside the primitive list in
README and CLAUDE.md. Since 2026-09-03 primitives may import
primitives, so it qualifies; listing it is a call to make, not
work.
.term alarm mark
Refused for pica and parked with its readmission test in
pica/DESIGN.t §11; in tessera it is a template condition over
the data, not a language mark. Listed here only so the two
records point at each other.


.term a sans face for the PDF
JuliaMono replaced Fira Mono on 2026-09-05 (pica/DESIGN.t §15),
which broke the Fira pair: the sans face is now free to choose,
and Fira Sans stays only because it was there. Choose on the
page, beside JuliaMono tables and code: candidates are any OFL
humanist or grotesque sans with Greek and tabular figures. The
choice moves the five-file embed noted under "pdf writer, in
three packages". Trigger: the next sans document anyone minds
the look of, or that split.

.term W003, field drift: the missing-field lint
fact validate cannot catch a missing field, and that is the one
generator error class it misses: dropping a line's intent from
nyx's 1600-record train.fact validates clean ("ok: 4820
facts"), and nyx's own loader errors on unknown keys, never on
absent ones. The class was caught in the field only downstream,
by nyx corpus's per-register counts failing to sum to the line
count -- and only because SPEC §5's asserted none makes the
absence countable at all, which is what TSV could not do. §5's
totality rule is a rule for authors that no validator enforces;
a schema'd TSV validator would catch a short row by arity, so
this is the single check where the row format wins.
The lint, sibling to W001 enum drift and computable for the
same reason (the file is the schema): for each kind, the field
set held by the majority of its instances is the kind's implied
shape; warn on any instance missing one of those fields, naming
instance and field. No external artifact, no grammar change,
and no inter-line dependence in the format -- a lint reads the
set, the grammar stays line-local. Trigger: the next corpus or
registry generated by an agent, or nothing -- the check is
cheap enough to do on the argument alone.
One computation, three consumers, so build them together. The
majority field set per kind is the lint's input, a report's
output, and a table's header:
.item fact shape FILE -- per kind, the instance count and each
field with its type. An oracle in the sense ~/repos/CLAUDE.md
already requires of a CLI: computed from the file on demand,
nothing stored, nothing to keep fresh, which is the test go doc
-u -short passed and the retired projection failed. It answers
the one question grep cannot answer in one pass, because it is
an aggregate over the set rather than a match on a line; nyx
corpus is this report hand-rolled, and this session hand-rolled
it twice more as ad-hoc regexes that silently miss any field
they did not anticipate. Print it line-oriented so it pipes.
.item fact rows FILE KIND -- the field set as a TSV header, one
row per instance, for the k>=2 conjunctive query that is the
format's one standing cost (grep per conjunct plus a join).
awk, cut, sort -k and join are present everywhere; jq is not
(not installed on this machine), and the CLI exposes only the
flat triples of encode, not Bind's record view, so the query
today is six lines of jq group_by. Constraints, ratified
2026-09-10 and recorded in SPEC Appendix A: export only, an
importer prohibited; not named as a sibling of the bijective
encode/decode pair; none prints as the bare word, so the view
keeps the asserted-vs-absent distinction a hand-written TSV
cannot hold; a list(T) cell prints its canonical [a, b] token
verbatim, since CORPUS.t's objection to a parser inside a
column was about a format people write and nothing reads this
one back.
.item W003 itself, the same field set used as a check.

.term fact validate as an edit-time hook
Nothing validates a .fact file when an agent edits one: gohook
knows nothing about .fact (grep -c fact ~/bin/gohook = 0), no
hook config mentions it, and the only validation in the tree
runs inside consumer Go at load time (nyx/load.go,
gist/forge/registry.go, reckon/build) -- so a turn can end with
an invalid file and nothing objects until the program runs. The
fix is the pattern that already works for Go: validate each
edited .fact file after the edit, refuse Stop while one is
invalid. Considered and rejected instead: fact assert/retract,
verbs that would apply and check a single line. They add no
check fact validate does not already make -- a sed-deleted
instance breaking a ref was caught this session by validate
alone -- only a guarantee that the check ran, and an agent that
skips validate would skip assert equally. Enforcement is a
hook's job, not a verb's. Trigger: ratification; the change is
to shared infrastructure every session inherits, so it is not
made unilaterally.

.term comments are destroyed by fact fmt -w
fmt -w silently deletes every comment line: nine of them in
live quietcasting-editors/subjects/football.fact, which are the
run instructions an agent arriving cold needs most. §8 requires
canonical output to carry no comments, so this cannot be fixed
by preserving them -- equal fact sets would stop producing
equal bytes. nyx/CORPUS.t already works around it ("Nothing is
said in a comment, because the parser drops comments"), which
is the tell: a construct whose own project tells authors not to
use it. Two coherent resolutions, and the choice is Pavlos's
because one of them is a grammar removal affecting 48 live
files: remove comments from the grammar, and the football.fact
header moves to a sibling .md/.t where §7's content boundary
says prose belongs anyway; or keep them and make fmt -w REFUSE
on a file containing comments rather than eat them. Blank lines
are the same class and harmless (sorting loses no information).
Recommendation: the removal, as one less concept and a uniform
§7 boundary. Trigger: ratification.

.term pica tables: a short row spans
Board's authoring format (repani-lab/board/DESIGN.t, Authoring)
settled on 2026-10-03 that a row with fewer cells than its table
has columns puts its last cell across the remaining columns,
separators included, keeping its first column's alignment and
wrap; empty cells keep the columns (`a | |`). Pica leaves the
missing cells blank. Adopt the same rule in pica, so a table
written in either reads the same and a heading, a note or an
address can span a table without a second table. It changes how
existing documents lay out: a sweep of the .t files under ~/repos
on 2026-10-03 (212 tables, `..` note rows excluded) found seven
short data rows in two files, six in research/EU-POSITION.t (a
five-column spec, four cells a row) and one in pica-forth's
corpus/tables-clip.t, each to be given its empty cells or let
span. Trigger: the board compiler's table layout, which pica can
share.

# Primitives

.term lz4s: the frame is at its optimum; a dictionary is not a feature
Measured 2026-09-05 on real raster pages
(~/repos/research/lz4s-lab/FINDINGS.t): every field of the token
pays for itself (the W bit ten percent, the 3/4 split within a
percent of any other, repeat offsets a loss, transposition a
disaster); optimal parsing, encoder only, gives two percent for a
hundred lines, a megabyte per call and a tuning constant, and was
tried and not admitted -- the greedy parser gained a hash-chain
matcher instead, byte-identical output, five to thirty times
faster on pages. A dictionary the decoder starts with
would take 10 to 30 percent off first pages and 90 to 97 off a
page's next version, but it is a shared secret between sender
and receiver -- an identity, a version, a silent failure when
they differ -- which the raster line "nothing in the bytes says
what format they are" refuses, and on the web the saving is
under a packet. Not a candidate. Reconsider only for a transport
that pays per byte for first pages, and then as an app's
optimisation over its own held page, never as the format's.
.term span diff: a positional page delta, not admitted
Measured 2026-09-05 against lz4s Delta on the fixture pairs in
lz4s/testdata. A span diff over the page bytes ([offset u16]
[len u8] bytes, gaps of three or fewer merged) needs nothing from
raster -- the page is equal-length bytes with ink in band, so a
byte diff is a canvas diff -- and would be its own primitive
beside lz4s (Diff, Apply), never in lz4s (append-only) nor in
raster (no API of raster's would know deltas exist). On a page's
next version, six cells changed, it is 19 bytes to Delta's 29:
lz4s pays a token, offset and extension for every unchanged
stretch it copies, spans pay nothing. On a different page of the
same app, or any edit that shifts content (an inserted row moves
every cell below it), spans are near the raw page, 831 against
Delta's 294 and a full Compress of 343, since they compare by
position and cannot say "moved". Speed: spans encode 10 to 70
times faster (1 us against 11 to 424 us) and apply four times
faster (0.3 against 1.1 us, 1.4 against 5.9 us on tessera), all
under a millisecond and dwarfed by the render; the delta encoder
allocates 20K to 79K per page, spans nothing. Not a candidate:
it wins ten bytes on the one case where both are already tiny
and loses everywhere else. Trigger: a consumer with live
cell-level updates in place, where the receiver wants the dirty
rows without a compare; then a primitive with round-trip and
known-answer tests over the same pairs.
.width 72
