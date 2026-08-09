# typeset

A minimal, troff-inspired source language and typesetter.
Documents are parsed once into a typed block model and exported by
writers: a fixed-width text page (for byte-frugal transports like
Quietcasting), an N-column newspaper-style PDF, and a single-column
report PDF -- monospace by default, proportional prose with
`.font sans`.

The design center is the language (see `doc.go` for the spec):

- **Fill mode by default.** Plain lines are paragraphs; the writer
  owns wrapping, hyphenation (Knuth-Liang, English + Greek
  embedded), and justification (Knuth-Plass with a gap-aware cost;
  proportional text adds GPOS kerning, interword shrink, and
  hanging hyphens). Authors never state widths in content.
- **Explicit structure, closed vocabulary.** `# heading` and
  `## subsection` (two levels, permanently), `---` rules,
  `.table … .end` (cells wrap by default, `!` clips, `N` columns
  align on the decimal point, `=` rows are bold totals, `..` rows
  are half-size notes), `.pre [N] … .end` verbatim blocks,
  `.quote … .end` quotations with `.attrib`, `.item` bulleted
  lists, `.link URL [TITLE]` link references (clickable in the
  PDF), `.by`/`.date` bylines, `.rem` comments. Unknown dot
  commands are parse errors, never silent passthrough; additions
  must pass the five-test gate in `doc.go`.
- **Self-contained documents.** Width, paper, columns, body face,
  and hyphenation language live in a layout trailer (`.width` /
  `.paper` / `.cols` / `.font` / `.lang`, defaults 40/a4/3/mono/all
  pattern sets; `.font sans` sets prose proportionally in the PDF).
  No formatting flags anywhere: the same source always produces the
  same output -- the PDF byte-identically (no timestamps).
- **Zero wire cost.** Every typeset command is consumed at
  typesetting time; a rendered text page carries only content plus
  the wire's own markup.

## pica

`cmd/pica` is the toolchain: one generation stage, then a writer.

    pica render page.tmpl data.json        # Go template -> source doc
    pica render ... | pica text            # source -> text page
    pica render ... | pica pdf -o p.pdf    # source -> newspaper PDF
    pica render ... | pica report -o p.pdf # source -> report PDF

`render` executes Go text/templates with value formatters (round,
decimal, trunc, pad, shortTime, shortDate, dur) and one structure
helper, `table`, which emits a `.table` block from a rows slice
plus field names (sparing templates the range boilerplate) -- no
layout functions exist. The PDF writers derive their body point
size from the document geometry (columnWidth / 0.6em / `.width`;
average lowercase advances under `.font sans`), so a text page and
a PDF column hold the same character density; they render justified
columns with orphan/widow control (splits keep >= 2 lines on each
side), headings scaled by level and kept with their story, split
tables repeating their headers (notes riding their rows), `.pre`
blocks atomic, and -- in the newspaper -- a single underfull page
balanced across its columns. `report` is the same engine in a
single-column identity: left title block, hairline table rules,
"Page N of M" footer.

`example/` is a complete bulletin: wrapped English and Greek
prose, tables at two widths, a `.pre` METAR line, `.link` sources.

    pica render -txtar example/page.tmpl example/content.txtar | pica text

The committed `example/expected.txt` is enforced by a golden test.

## pdf/

A zero-dependency PDF 1.3 writer: subset-embedded TrueType (Fira
Mono and Fira Sans, Regular and Bold -- SIL OFL, see
`pdf/fonts/README.md` -- Latin, Greek, and Cyrillic; the mono
faces at a uniform 600/1000 em), GPOS pair kerning, GSUB tabular
figures applied statically at parse time (digits and the figure
space share one advance in both sans weights), ToUnicode CMaps for
selectable text, positioned text, hairlines, grayscale. Nothing
else.

## Notes

- Widths count runes, not display cells: double-width (CJK) glyphs
  misalign. The package targets scripts where one rune is one
  monospace cell.
- The hyphenation patterns under `patterns/` derive from the TeX
  patterns; see `patterns/README.md`.
