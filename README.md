# typeset

Text shaping for fixed-width monospace display: Knuth-Liang
hyphenation (English + Greek patterns embedded), Knuth-Plass optimal
line wrapping and justification tuned for monospace output, and
fixed-width table layout. Output is plain text; the intended
consumers are systems that store or transmit preformatted monospace
content and display it verbatim.

See `doc.go` for the library API and conventions.

## pica

`cmd/pica` is the command-line renderer and the reference consumer:
it executes a Go text/template with the typesetting helpers, then
expands `.table` blocks in the result.

    pica render page.tmpl data.json
    pica render -txtar page.tmpl content.txtar
    echo '{"body":"..."}' | pica render page.tmpl -

Template helpers take the width as their first argument
(`{{wrap 40 .body}}`, `{{.body | justify 32}}`); `.table` blocks
default to 40 columns unless the spec starts with a width
(`.table 44 3L *L 4R`). Run `pica help` for the full helper list
and the txtar input conventions.

`example/` is a complete weather-bulletin page exercising the whole
surface -- wrapped English and Greek prose, a justified outlook,
formatters, a hard-cut METAR line, and tables at two widths:

    pica render -txtar example/page.tmpl example/content.txtar

The committed `example/expected.txt` is enforced by a golden test.

## Notes

- Widths count runes, not display cells: double-width (CJK) glyphs
  will misalign. The package targets scripts where one rune is one
  monospace cell.
- The hyphenation pattern files under `patterns/` derive from the
  TeX hyphenation patterns; see `patterns/README.md`.
