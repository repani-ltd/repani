# pica (repani.com/pica)

Text typesetting library: wrapping, hyphenation, tables (the root package,
stdlib-only), with PDF primitives (`pdf/`, `pdf/ttf/`), the compositor and
its two presentations (`press/`: `press.PDF` the default, `press.Report`),
the copy desk (`desk/`: the template vocabulary and the validating
`Render`; values are formatted by `repani.com/typeset/format`), the cell
writer (`cell/`: a document set on a raster page, columns across panels,
inked through a replaceable alias vocabulary; DESIGN.t section 15), and
the `pica` CLI (`cmd/pica`), a thin flag surface over all of it. The
design ledger for the split is DESIGN.t section 10.

