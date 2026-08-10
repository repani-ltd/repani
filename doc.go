/*
Package typeset parses a minimal, troff-inspired source language
into a typed document and renders it through width-disciplined
writers: a plain-text writer (Doc.Text) for fixed-width monospace
pages, and -- via the block model -- richer writers such as the
pica PDFs, monospace by default and proportional prose with
.font sans.

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
	# heading        a section heading; "## heading" is a
	## heading       subsection. Two levels, deliberately and
	                 permanently ("###" is an error): two served
	                 man pages for fifty years, and unbounded
	                 depth is where document structure rots
	---              a horizontal rule (3+ dashes, nothing else)
	.table [W] SPEC  a table block: rows follow, first row is the
	  a | b | c      header, cells separated by "|"; ends with .end.
	.end             W (optional) fixes the table's width in runes;
	                 it applies only when smaller than the document
	                 width (see Block.TableWidth). A "-" before
	                 SPEC makes the table headerless: every row is
	                 data, no separator rule. Rows may be marked:
	                 "=" opens a total row, ".." a note row -- see
	                 the Tables section
	.pre [N]         a verbatim block: the writer never refills it,
	  lines...       only truncates overlong lines; ends with .end.
	.end             N (optional) is the number of leading lines a
	                 column-splitting writer repeats after a split.
	.link URL [T]    a link reference: the first field is the URL
	                 (a URL cannot contain a literal space), and
	                 everything after it is the optional title
	                 phrase T -- even if it looks like another
	                 URL. Writers whose medium supports links
	                 render a real one with T (or the URL) as the
	                 anchor text
	.quote           a quotation: the body's non-blank lines fill
	  prose...       as ONE paragraph, inset 2 characters on both
	  .attrib WHO    sides; ends with .end. An optional final
	.end             ".attrib WHO" line sets the attribution,
	                 rendered "-- WHO" right-aligned to the
	                 quote's right margin. An empty quote, a
	                 second .attrib, or content after .attrib is
	                 an error
	.item TEXT       one bulleted list item: a bullet (U+2022),
	                 then TEXT, rendered with turnover lines
	                 hanging 2 characters. TEXT fills like a
	                 paragraph: unmarked lines directly under the
	                 command continue the item's text, and a blank
	                 line or any marked line ends it — hard-wrapped
	                 source means the same thing everywhere.
	                 Consecutive .item lines set tight (no blank
	                 line between); items do not nest
	.by TEXT         byline and dateline metadata: rendered under
	.date TEXT       the title as "by TEXT -- DATE" (whichever
	                 parts are present; see Doc.Byline). Each may
	                 appear once, and only before the first
	                 content block
	.rights TEXT     imprint/copyright metadata (publisher name,
	                 rights notice). The PDF writers join it to
	                 the page number in one small gray centered
	                 footer line on every page; the text page
	                 closes with it as a final line. Once, before
	                 the first content block, like .by
	.rem TEXT        a comment: dropped from every output, valid
	                 anywhere -- it does not end a paragraph and
	                 may follow the layout trailer. Inside .pre,
	                 .table, or .quote bodies it is content, not
	                 a comment

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
Line counts are deterministic in every writer, and for plain blocks
a text page and a mono PDF column stay the same object. Two things
deliberately trade that exact identity away: .font sans trades it
for proportional prose (line breaks measured, not counted), and the
PDF writers' size roles trade it for hierarchy and density (a
heading occupies a taller slot at a larger scale; a table note row
sets half-size on half the leading, where the text page renders it
as an ordinary full-size row; a tight item run in which any item
turns over gains half-line gaps between items, where the text page
stays tight).
Structural commands (.table, .pre, .end) and the layout trailer
never appear in any output.

# Layout trailer

Layout commands are document-global and must appear after all
content -- content following a layout command is an error. Each
has a default, so an attribute-free document is fully determined:

	.width N    characters per line/column     (default 80, 10-200)
	.paper P    pdf paper: a4, a5, or letter   (default a4)
	.cols N     pdf columns per page           (default 1, 1-6)
	.font F     pdf body face: mono or sans    (default mono)

Values outside the given ranges are parse errors. Hyphenation
always uses every embedded pattern set: the embedded scripts are
disjoint, so the merged set hyphenates English and Greek prose
correctly side by side. (A .lang selector existed and was removed
-- it only earns existence when sets sharing a script are added,
where mixed patterns would mis-hyphenate.)

There is no font-size attribute: the PDF body size is derived so a
column holds exactly .width characters -- runes for the mono face
(columnWidth / (0.6em x .width)), average lowercase advances for
the sans face -- and both writers compose at the same character
density, so a text page stays typographically one column of the
PDF. Note the direction: the writer owns the page margins, derived
from the layout (a single-column page takes a book margin,
multi-column pages the newspaper's thin one), and the paper fixes
the page, so a LARGER .width means SMALLER type (more characters
across the same fixed measure) and a smaller .width means larger
type. With .font sans, prose and headings set proportionally with
measured justification; verbatim blocks and tables keep their
monospace layout (the text writer ignores .font entirely).

# Tables

The column spec is space-separated <width><align> tokens:

	width   integer rune count, or "*" to auto-fill the remainder
	        (at most one auto column)
	align   L = left, R = right, C = center, N = numeric: cells
	        align on the decimal point and never wrap; accounting
	        negatives "(1.23)" reserve a trailing paren slot for
	        the whole column, and non-numeric cells (a header,
	        "n/a") right-align at the units position

Cells WRAP by default: overflow continues on following lines,
other cells padded blank, and such a multi-line row is an atomic
unit for column-splitting writers. Append "!" to a token to clip
that column instead ("5L! 4R!"). Columns are joined by a single
space; a header is followed by a separator rule (a dash row on the
text page, a hairline in the PDFs). "|" always
separates cells (there is no escape for a literal one), and cells
beyond the spec's columns are dropped.

A row starting with "=" is a TOTAL ROW: formatted like a data row
(its numbers weigh into N-column metrics) but set bold under a
rule — a dash row in plain text, a hairline in writers whose
medium draws real rules. There is no escape for a leading literal
"=".

A row starting with ".." is a NOTE ROW: an annotation attached to
the data row above it (to the header when no data row precedes).
Half-line writers render note cells at half the body size on half
the leading, left-aligned under their columns with twice the rune
budget; the plain-text writer renders them as ordinary full-size
rows. A note never separates from its row at a column split, and
a note row after the header travels with it when a table's header
repeats. There is no escape for a leading literal "..".

# Wrapping

Paragraph filling is Knuth-Plass optimal line breaking with
Knuth-Liang hyphenation (embedded TeX patterns for English and
Greek). Justification uses a gap-aware cost model that prefers
hyphenation over wide inter-word gaps. All widths flow through a
Measurer: the monospace measurer counts one unit per rune and
distributes slack as whole spaces; proportional writers supply
font-metric measurers (thousandths of an em) and spread slack
continuously across the gaps. Under a proportional measurer,
justified gaps may also shrink up to a third of a space
(HangHyphen and the shrink allowance are both zero on the
monospace grid), and a line-final hyphen hangs 70% of its width
into the right margin so the flush edge stays optically straight.
A word wider than the measure is hyphenated at whatever point
fits; only a fragment with no valid break overflows the line.
JustifyParagraph is the monospace paragraph-level primitive for
writers holding parsed Para blocks; WrapLines and JustifyLines are
the measured structured primitives; all document structure goes
through Parse. Every primitive hyphenates with every embedded
pattern set.

Widths count runes, not display cells: double-width (CJK) glyphs
misalign. The package targets scripts where one rune is one
monospace cell.

# Growing the vocabulary

The vocabulary stays closed by policy, not accident. A new command
enters the registry only if it passes five tests:

 1. It is expressible as a block with a deterministic line count
    under the monospace measurer -- the flow arithmetic every keep
    and split rule relies on (document metadata like .by lives in
    the title zone, outside block flow, and passes by not flowing).
 2. It renders meaningfully in EVERY writer, the plain-text page
    included. Uniformly invisible (.rem) is meaningful; ignored by
    one writer but not another is not.
 3. It states content semantics, never geometry: what something IS
    (a quotation, an item), not where it goes. Page and column
    control stay writer-owned.
 4. It has a troff or TeX ancestor to point to (.quote is ms's
    .QP, .item is .IP, .rem is troff's comment): four decades of
    typesetting have already named most things worth naming.
 5. A real document is already waiting to use it: demand precedes
    entry, so every command ships together with its first user.
    A command admitted for a hypothetical future is a permanent
    promise made on no evidence -- the other four tests measure
    quality, this one measures need.

# Non-goals

No inline emphasis (the monospace wire cannot express it), no
third heading level (two, permanently), no block nesting, no
page-control commands in content, and no round-tripping: rendered
output is a final artifact, not re-parseable source.
*/
package typeset
