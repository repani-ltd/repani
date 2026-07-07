# typeset

A minimal, troff-inspired source language and typesetter for
monospace surfaces. Documents are parsed once into a typed block
model and exported by writers: a fixed-width text page (for
byte-frugal transports like Quietcasting) and an N-column
newspaper-style PDF.

The design center is the language (see `doc.go` for the spec):

- **Fill mode by default.** Plain lines are paragraphs; the writer
  owns wrapping, hyphenation (Knuth-Liang, English + Greek
  embedded), and justification (Knuth-Plass with a monospace
  gap-aware cost). Authors never state widths in content.
- **Explicit structure, closed vocabulary.** `# heading`, `---`
  rules, `.table … .end` (cells wrap by default, `!` clips),
  `.pre [N] … .end` verbatim blocks, `.link` and `.set KEY VALUE`
  wire metadata. Unknown
  dot commands are parse errors, never silent passthrough.
- **Self-contained documents.** Width, paper, and columns live in a
  layout trailer (`.width` / `.paper` / `.cols`, defaults 40/a4/3).
  No formatting flags anywhere: the same source always produces the
  same output -- the PDF byte-identically (no timestamps).
- **Zero wire cost.** Every typeset command is consumed at
  typesetting time; a rendered text page carries only content plus
  the wire's own markup.

## pica

`cmd/pica` is the toolchain: three orthogonal stages.

    pica render page.tmpl data.json     # Go template -> source doc
    pica render ... | pica text         # source -> text page
    pica render ... | pica pdf -o p.pdf # source -> newspaper PDF

`render` executes Go text/templates with value formatters only
(round, decimal, trunc, pad, shortTime, shortDate, dur) -- no
layout functions exist. `pdf` derives its body point size from the
document geometry (columnWidth / 0.6em / `.width`), so a text page
and a PDF column are the same typographic object; it renders
justified columns with orphan/widow control (splits keep >= 2
lines on each side), headings bold and kept with their story,
split tables repeating their headers, `.pre` blocks atomic, and a
single underfull page balanced across its columns.

`example/` is a complete bulletin: wrapped English and Greek
prose, tables at two widths, a `.pre` METAR line, `.link` sources.

    pica render -txtar example/page.tmpl example/content.txtar | pica text

The committed `example/expected.txt` is enforced by a golden test.

## pdf/

A zero-dependency PDF 1.3 writer: subset-embedded TrueType (Fira
Mono Regular/Bold -- SIL OFL, see `pdf/fonts/README.md` -- Latin,
Greek, and Cyrillic at a uniform 600/1000 em), ToUnicode CMaps for
selectable text, positioned text, hairlines, grayscale. Nothing
else.

## Notes

- Widths count runes, not display cells: double-width (CJK) glyphs
  misalign. The package targets scripts where one rune is one
  monospace cell.
- The hyphenation patterns under `patterns/` derive from the TeX
  patterns; see `patterns/README.md`.
