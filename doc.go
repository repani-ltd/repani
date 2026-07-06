/*
Package typeset shapes plain text for fixed-width monospace
display: hyphenation, optimal line wrapping, justification, and
table layout. Output is ordinary text -- the intended consumers are
systems that transmit or store preformatted monospace content
(status pages, broadcast text, terminal reports) where the reader
displays it verbatim.

# Hyphenation

Knuth-Liang pattern hyphenation with embedded TeX patterns for
English and Greek. Words shorter than 5 runes are never hyphenated,
and no break is placed in the first or last 2 characters of a word.

# Wrapping and justification

Wrap reflows blank-line-separated paragraphs to a caller-supplied
width using Knuth-Plass optimal line breaking with hyphenation,
leaving lines ragged-right.

Justify reflows with the same optimizer but a gap-aware cost
function: it models the actual inter-word gap widths after
justification and prefers hyphenation over wide gaps, since
monospace output cannot hide fractional space adjustments. Non-final
lines are then padded so both edges are flush.

TruncLines hard-cuts every line to the width with no reflow.

All three treat a line containing two or more consecutive internal
spaces as preformatted (column-aligned key/value pairs, hand-drawn
tables) and pass it through unchanged; Wrap and Justify also pass
blank lines through, preserving paragraph structure.

Widths count runes, not display cells: double-width characters
(CJK) will misalign. The package targets scripts where one rune is
one monospace cell.

# Tables

Tables are built with a column spec like "3L *L 4R" where each
token is <width><align>:

	width   integer character count, or "*" to auto-fill remaining
	align   L = left, R = right, C = center

At most one column may use "*" to absorb the remaining width. Cells
exceeding their column width are truncated (hard cut). Columns are
joined with a single space separator.

	tbl, err := typeset.NewTable("3L *L 4R", 40)
	if err != nil { return err }
	tbl.Header("Day", "Forecast", "Temp")
	tbl.Row("Mon", "Sunny breeze", "25")
	out := tbl.Render()

# Table blocks

ExpandTables replaces .table blocks embedded in text with rendered
tables. A block is a .table marker line carrying the spec, rows of
|-separated cells (first row is the header), and a .end marker:

	.table 3L *L 4R
	Day | Forecast | Temp
	Mon | Sunny breeze | 25
	.end

The spec may begin with an integer total width (".table 44 3L *L
4R"); without one the block renders at DefaultWidth (40).
*/
package typeset
