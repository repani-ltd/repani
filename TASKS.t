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

# Elsewhere

.term quietcasting binding of raster
Raster's records (RASTER.t, "Records": a row record whole, and the
row count) are the unit the radio carries, unchanged: a slot
holds whole records, packed first-fit by a scheduler, and a
receiver applies every slot's records in slot order, a station
sending whole pages (FF 00, rows, FF H) so a receiver holding
anything is correct. The slot counts measured on 2026-09-14 --
a departures screen in seven 238-byte slots, a weather screen in
eight -- were for the first raster's row record and are to be
measured again for this one. Open: which rows a station carries,
a row published in two slots (a publisher error, or slot order
decides), and how a whole page spans slots without a receiver
showing half of one. Tessera, the former binding, is in the attic
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

.term pica tables onto typeset/tbl, one table language with board
Settled 2026-10-04 (repani-lab/board/DESIGN.t, Authoring): the
grammar inside pica's `.table` and board's `.fmt` is one, built
once in a new package, typeset/tbl, above typeset/tab, with a
hand-written scanner and no regular expressions. Pica keeps
`.table` and `.end`. What changes for pica: the header is
explicit, a `^` prefix on its row, and the first-row rule and the
`-` headerless flag go -- 321 of the 323 tables under ~/repos on
2026-10-04 take a `^` on their first row, 2 drop their `-`, one
mechanical substitution; a short row spans (a row with fewer cells
than columns puts its last cell across the rest, separators
included; empty cells keep the columns, `a | |`) where pica now
leaves the missing cells blank -- a sweep on 2026-10-03 found
seven short data rows, six in research/EU-POSITION.t and one in
pica-forth's corpus/tables-clip.t, each to be given its empty
cells or let span; and pica reads S spans, full and relative
formats, colour codes and `@` cell links, its writers deciding
what they draw of them. `=` totals and `..` notes stay as they
are. Order: tbl with its tests, then pica moved onto it under its
own tests, then board's parser. Trigger: tbl.

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
