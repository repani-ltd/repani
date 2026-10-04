# pica (repani.com/pica)

Text typesetting library: the language, paragraphs and tables (the root
package; it imports only the stdlib-only primitives `typeset/tab`, the
grid, and `typeset/wrap` with `typeset/wrap/hyphen`, the line breaker
and its hyphenation, DESIGN.t sections 12, 16 and 17), with PDF primitives (`pdf/`, `pdf/ttf/`), the compositor and
its two presentations (`press/`: `press.PDF` the default, `press.Report`),
the copy desk (`desk/`: the template vocabulary and the validating
`Render`; values are formatted by `repani.com/typeset/format`), and
the `pica` CLI (`cmd/pica`), a thin flag surface over all of it. The
design ledger for the split is DESIGN.t section 10.

