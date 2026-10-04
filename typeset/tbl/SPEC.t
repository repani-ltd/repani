TBL -- THE TABLE LANGUAGE
.date 2026-10-04
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem The technical specification of repani.com/typeset/tbl. Rationale
.rem and history live in repani-lab/board/DESIGN.t.

Package `repani.com/typeset/tbl` parses and lays out the table
language that pica's `.table` blocks and board's `.fmt` documents
share. It reads FORMAT lines (a column spec) and ROW lines, keeps
the current format, and lays rows out as lines of placed cells on
a monospace grid. It imports `repani.com/typeset/tab` for column
fitting and cell alignment, `repani.com/typeset/wrap` with
`repani.com/typeset/wrap/hyphen` for cell line breaking, and
`repani.com/typeset/raster` for the rule a cell's text keeps, and
nothing above them. The host -- pica
or board -- recognises its own directives, hands tbl the spec text
and the row lines, supplies the measure, and renders what tbl lays
out.

# Conventions

.item TEXT is UTF-8. Widths and columns count code points: one code
point, one column. A cell's content is taken in NFC with each run
of breaking spaces (wrap.Fields) made one space, and every code
point in it must take one column by raster's rule
(raster.Columns): no control, combining or zero-width code point,
nothing wide. So a cell is one width on every grid, and clipped,
numeric and wrapped cells show the same text.
.item INDEXES in the API count from zero: a grid column, a cell, a
format column. A stretch of grid columns is start to end, the end
exclusive.
.item SOURCE POSITIONS in errors count from one, in code points:
every parse function takes the text and the column at which it
starts in its source line, and reports absolute columns. The host
adds the line number.
.item SPACE means a breaking space as typeset/wrap splits words
(wrap.IsBreakingSpace: Unicode white space but the no-break
spaces) wherever tokens or cells are trimmed or split, so a cell's
words are the words the breaker sets.
.item No regular expressions: specs and rows are read by a
hand-written scanner.

# Colour codes

.pre
code    = [fg] ["/" bg]          at least one of the two present
fg, bg  = d | r | g | y | b | m | c | w
.end

.table 12L 8R *L
^letter | index | colour
d | 0 | the theme's default
r | 1 | red
g | 2 | green
y | 3 | yellow
b | 4 | blue
m | 5 | magenta
c | 6 | cyan
w | 7 | white
.end

A code sets each of foreground and background or leaves it UNSET.
`r` sets fg 1, bg unset; `/y` sets bg 3, fg unset; `r/b` sets both;
`d` sets fg 0, which is a colour, not unset. Any other character,
an empty code, a bare `/`, or a letter repeated (`rr`) is an error.

# Format lines

A format line is the spec text a host hands over: for board, what
follows `.fmt`; for pica, what follows `.table`. It is a sequence
of tokens separated by spaces:

.pre
spec      = [rowcode] [narrow] {column}
rowcode   = code
narrow    = digits                       a positive integer
column    = [width] align [code] ["!"]
width     = digits | "*"                 digits: a positive integer
align     = L | R | C | N | P | S
.end

A token's first character decides what it is: a lower-case colour
letter or `/` begins the row code, which may only be the first
token; a digit begins the narrowing width when the token is digits
alone, which may only come before any column; otherwise a digit,
`*` or an upper-case letter begins a column. Anything else is an
error.

.term align
L left, R right, C centred (the extra space on the right), N on the
decimal point as `typeset/tab` aligns it, P prose (laid out as L),
S span: the column joins the column on its left.
.term "!"
The column clips instead of wrapping.
.term code on a column
The column's colours.
.term row code
The colours of every row under the format, including the gaps
between cells and any width the format leaves unused.
.term narrow
The format's width when less than the measure the host gives;
otherwise the measure.

A FULL format has at least one column and a width on every column;
a format with no columns is therefore relative. It defines the GRID -- the number of columns, their widths, which
is auto (`*`, at most one) -- and the narrowing. It inherits
nothing.

A RELATIVE format has no width on any column. It may not carry a
narrowing. It is relative to the last full format, never to an
earlier relative one; a relative format with no full format before
it is an error. It lists every column of the grid or none:

.item NO COLUMNS: every column is the full format's.
.item EVERY COLUMN: each token gives the column's alignment and its
clip as written (no `!` means wrap); its width and auto come from
the full format; each of its fg and bg is the token's if set, else
the full format's.
.item THE ROW CODE: each of fg and bg is the relative format's if
set, else the full format's.

A bare format -- no tokens -- is therefore the last full format
again. A spec with widths on some columns and not others, a
relative format with a column count other than zero or the grid's,
an S in the first column, an S column with a
code or `!`, a second `*`, a narrowing in a relative format, and a
token out of order are errors.

`typeset/tab`'s column spec -- width and alignment, `L R C N`, `*`
-- is an exact subset of a full format and means the same columns
there. tbl keeps it so: a change to the token grammar that made a
valid tab spec invalid here, or mean other columns, is a change to
both packages or none.

The resolved format, which every row is laid out by, holds the row
code (fg and bg, each set or unset), the width, and per column:
width, auto, align, clip, fg and bg (each set or unset).

# Row lines

A host hands tbl every line of a table that is not one of its
directives. Leading and trailing spaces of the line are ignored
for classification.

.term blank row
A line that is empty or only spaces.
.term rule row
A line that is exactly `---`.
.term data row
Any other line. It may start with one ROLE PREFIX, tested in this
order: `..` NOTE, `=` TOTAL, `^` HEADER; with none the role is
DATA. The rest of the line is its cells.

CELLS are the text between `|` characters: n `|` make n + 1 cells,
so `a | |` is three cells and `a |` two. There is no escape for a
literal `|`. Each cell is trimmed of spaces at both ends, then may
start with MARKS, each ending at the next space or the end of the
cell:

.term :code
The cell's colours, a code as above.
.term @target
The cell is a link to target: 1 to 255 bytes of ASCII from RFC
3986's unreserved and reserved sets, `%` followed by two hex
digits; anything else in it is an error.

Marks come in either order, each at most once; what follows the
last mark, trimmed, is the cell's content. Content therefore cannot
start with `:` or `@`, and a data row's first cell cannot start with
a role prefix; there is no escape. Content may be empty.

A note row annotates the data row above it, so a table must have a
data row before it -- a blank or rule row does not count. Header
and total rows may stand anywhere and repeat.

# Cells on the grid

A format's columns form GROUPS: a column that is not S begins a
group, and each S column after it joins that group. Cell i of a row
fills group i. A cell's BOX runs from its group's first column's
start to its last column's end, so the gaps inside the group belong
to the box.

A row with more cells than groups is an error. A row with fewer --
a SHORT ROW -- puts its last cell across every remaining group, so
its box ends at the last column's end. A spanning or joined cell
takes the alignment, clip and colours of its box's first column.

A cell's colours resolve per property, fg and bg each apart: the
cell's mark if set, else the format's row code if set, else the
box's first column if set, else 0. Its link is its own mark or
none: formats carry no links.

# Layout

`Layout` takes one TABLE -- the rows from a full format to the next
full format, each with the resolved format it is under -- the
MEASURE (board 40, pica its document width) and a gap of 1. It
returns, per row, its kind, role, resolved row colours
and lines of placed cells. `Grid` returns the grid the rows are
laid on -- each column's span and each N column's decimal metrics
-- for a host that draws numbers itself, and `Format.Boxes` the
first and last column of each cell of a row of n cells.

.term fitting
The width is the narrowing if set and less than the measure, else
the measure. Columns are fitted with `tab.Fit` against the width
and the gap: the auto column takes what the others and the gaps
leave, at least 1; columns that cannot fit are an error. Each
format's fitting is its full format's, so every row of a table
shares one grid.
.term wrapping
A cell whose box column is neither clip nor N is broken to the
box's width by `wrap.Cell` with `hyphen.Default`: Knuth-Plass
with hyphenation at the cell penalty, a word wider than the box
broken at its hyphenation points where one fits and cut where none
does, every line at most the box's width, empty text one empty
line. A clip cell is one line, cut to the box's
width. An N cell is one line, aligned as `tab` aligns it and cut
to the box's width; an N cell whose box is a span or a join is
aligned as R. A number wider than its N box is an error: cut, it
would read as another number. Text that is not a number cuts.
.term height
A data row is as many lines as its tallest cell; a shorter cell's
missing lines are empty.
.term alignment
Each line of a cell is padded to exactly the box's width by its
alignment, every line on its own.
.term decimal metrics
For each N column, the metrics `tab` gathers are measured over the
table's data and total rows whose cell in that column is a single,
unjoined, unspanned box under a format where the column is N.
Header and note rows are aligned with them but do not weigh in.
.term placed cell
start, end (grid columns), text (exactly end less start code
points), fg, bg (resolved, 0 when unset at every level), target.
Every line of a row carries every one of its boxes, with the
cell's colours and link on each.
.term what is not placed
The gaps between boxes and the width beyond the last column take
the row's resolved colours; the host fills them. Blank and rule
rows have no lines; the host draws them across the width.

Every host breaks cells with the same breaker, so a cell breaks the
same on a board and in a pica document. A host that sets a P
column in a proportional face may break that cell again with
`typeset/wrap` under its own measurer; its monospace layout is
still tbl's.

# What a host decides

.item Which lines are its directives, where a table starts and
ends, and how a full format starts a new table.
.item What it renders for a role, a rule row and a blank row, and
which roles it accepts: board refuses notes; pica sets them under
their row. A refusal is an error at the row, never a silent drop.
.item Whether it renders colours and links at all.
.item The measure, and any check its medium needs on the text.

# Errors

Every error names its kind and its source column, from one. The
kinds: a bad colour code; a token out of order or unknown; widths
on some columns and not others; a relative format with the wrong
column count, with a narrowing, or with no full format before it;
S first, or S with a code or `!`; a second
`*`; columns that cannot fit the width; a bad or repeated mark;
cell text a grid cannot show; a
bad target; more cells than groups; a note row with no row above;
a number wider than its N box.

Rationale, history and the choices weighed: `repani-lab/board/DESIGN.t`.

.width 72
.cols 1
.font sans
