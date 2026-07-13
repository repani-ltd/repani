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
	                 a blank line ends it; the writer wraps it.
	                 EVERY unmarked line is prose (fill mode, as in
	                 troff): structure is never inferred from
	                 spacing, so aligned content must use .pre
	# heading        a section heading (one level, as on the wire)
	---              a horizontal rule (3+ dashes, nothing else)
	.table [W] SPEC  a table block: rows follow, first row is the
	  a | b | c      header, cells separated by "|"; ends with .end.
	.end             W (optional) fixes the table's width in runes;
	                 it applies only when smaller than the document
	                 width (see Block.TableWidth). A "-" before
	                 SPEC makes the table headerless: every row is
	                 data, no separator rule.
	.pre [N]         a verbatim block: the writer never refills it,
	  lines...       only truncates overlong lines; ends with .end.
	.end             N (optional) is the number of leading lines a
	                 column-splitting writer repeats after a split.
	.link URL [T]    a link reference: URL plus an optional single-
	                 word title T. Writers whose medium supports
	                 links render a real one with T (or the URL) as
	                 the anchor text

The dot-command vocabulary is CLOSED: a line that lexes as a dot
command (dot followed by a lowercase letter; ". " and ".." begin
ordinary text) but is not in the registry is a parse error. The
lexing rule matches the wire's, with one authoring-side tolerance:
leading whitespace is ignored here, while the wire lexes at
column 0.

Every command is part of the typesetting language: Parse types each
into a Doc block, and each writer decides that block's rendering in
its own output format. The text writer's target format is the
quietcasting wire markup, so it serializes Heading as "# text" and
LinkBlk as a ".link" meta line (which wire clients render as a
proper link); the pdf writer renders headings bold and links as
gray anchor text carrying a clickable annotation. A consumer that
wants the data rather than a rendering walks Doc.Blocks directly.
One rule binds all writers: every block occupies the same line
count in each, so a text page and a PDF column stay the same
object. Structural commands (.table, .pre, .end) and the layout
trailer never appear in any output.

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
distributes slack as whole spaces. JustifyParagraph is the
paragraph-level primitive for writers holding parsed Para blocks;
all document structure goes through Parse.

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
