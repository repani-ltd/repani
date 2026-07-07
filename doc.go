/*
Package typeset parses a minimal, troff-inspired source language
into a typed document and renders it for fixed-width monospace
surfaces: a plain-text writer (Doc.Text) for width-limited pages,
and -- via the block model -- richer writers such as the pica
newspaper PDF.

The language inherits troff's principles, not its vocabulary:
fill mode is the default (the writer owns wrapping, justification,
and width; authors never state widths in content), commands are
line-oriented dot requests, and layout parameters live in the
document itself, so a document is self-contained: the same source
always produces the same output.

# The language

A document is UTF-8 text, structured by line:

	TITLE            the first non-blank line is the title, no prefix
	prose            consecutive plain lines form one paragraph;
	                 a blank line ends it; the writer wraps it
	# heading        a section heading (one level, as on the wire)
	---              a horizontal rule (3+ dashes, nothing else)
	.table [W] SPEC  a table block: rows follow, first row is the
	  a | b | c      header, cells separated by "|"; ends with .end.
	.end             W (optional) fixes the table's width in runes;
	                 it applies only when smaller than the document
	                 width (see Block.TableWidth).
	.pre [N]         a verbatim block: the writer never refills it,
	  lines...       only truncates overlong lines; ends with .end.
	.end             N (optional) is the number of leading lines a
	                 column-splitting writer repeats after a split.
	.link URL        wire metadata, passed through to text output
	KEY   VALUE      a line with 2+ consecutive internal spaces is
	                 a single verbatim line, no .pre needed

Inline forms (@NN page references, #word tags) are ordinary words
to the typesetter and are never split by wrapping.

The dot-command vocabulary is CLOSED: a line that lexes as a dot
command (dot followed by a lowercase letter; ". " and ".." begin
ordinary text) but is not in the registry is a parse error. The
lexing rule matches the wire's, with one authoring-side tolerance:
leading whitespace is ignored here, while the wire lexes at
column 0. The
registry has two classes: typeset commands, consumed here and
never reaching the output (.table, .pre, .end, and the layout
commands below), and wire commands, typed and re-emitted for the
next layer (.link).

# Layout trailer

Layout commands are document-global and must appear after all
content -- content following a layout command is an error. Each
has a default, so an attribute-free document is fully determined:

	.width N    characters per line/column     (default 40)
	.paper P    pdf paper: a4, a5, or letter   (default a4)
	.cols N     pdf columns per page           (default 3)

There is no font-size attribute: with a monospace face the PDF
body size is derived -- columnWidth / (0.6em x .width) -- so both
writers compose at the same character width and a text page is
typographically one column of the PDF.

# Tables

The column spec is space-separated <width><align> tokens:

	width   integer rune count, or "*" to auto-fill the remainder
	        (at most one auto column)
	align   L = left, R = right, C = center

Cells WRAP by default: overflow continues on following lines,
other cells padded blank, and such a multi-line row is an atomic
unit for column-splitting writers. Append "!" to a token to clip
that column instead ("5L! 4R!"). Columns are joined by a single
space; a header is followed by a dashed separator row.

# Wrapping

Paragraph filling is Knuth-Plass optimal line breaking with
Knuth-Liang hyphenation (embedded TeX patterns for English and
Greek). Justification uses a gap-aware cost model that prefers
hyphenation over wide inter-word gaps, since monospace output
distributes slack as whole spaces. Wrap, Justify, and TruncLines
are exported as prose utilities; structured documents go through
Parse.

Widths count runes, not display cells: double-width (CJK) glyphs
misalign. The package targets scripts where one rune is one
monospace cell.

# Non-goals

No inline emphasis (the monospace wire cannot express it), one
heading level, no block nesting, no page-control commands in
content, and no round-tripping: rendered output is a final
artifact, not re-parseable source.
*/
package typeset
