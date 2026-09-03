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
typeset/pdf/ttf, unchanged; and the five embedded Fira files
(1.6M, parsed at package init) as typeset/fira, which registers
the faces. A writer that names only a standard font then
imports the core and carries no font data, and the closed font
enum opens: a face is registered, not enumerated. API change
for press and the CLI: pdf.Sans and kin become fira's names,
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

.term trudge as a primitive
trudge imports ascon and sits outside the primitive list in
README and CLAUDE.md. Since 2026-09-03 primitives may import
primitives, so it qualifies; listing it is a call to make, not
work.
.term pica-to-tessera writer
The way tessera pages get generated content: a pica document
per panel (or per page with atomic blocks), RenderBlock at
width 34, one ink per construct page-wide. Its arrival is the
second real backend that promotes press's fblock/sline to a
shared contract (pica/DESIGN.t §1, §10). Trigger: a station
that generates a tessera page from data.
.term alarm mark
Refused for pica and parked with its readmission test in
pica/DESIGN.t §11; in tessera it is a template condition over
the data, not a language mark. Listed here only so the two
records point at each other.

.width 72
