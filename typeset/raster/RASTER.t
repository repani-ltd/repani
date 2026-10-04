RASTER -- ROWS OF SEGMENTS
.date 2026-10-04
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem The format specification of repani.com/typeset/raster. Normative
.rem throughout. Rationale and history live in DESIGN.t beside it.

A raster is rows of forty columns and a row count. Each row is one
record: its number, its role, its text of exactly forty code
points, and the segments that give stretches of the text their
colours and links. A raster holds no content type -- a producer
sets tables, paragraphs or anything else on rows -- and nothing
above the row: which page it is, its freshness and its atomic
replacement belong to the container that carries it.

# Conventions

.item Every field is one byte except a target's characters and the
text. Bytes are written in the order given here, with no padding.
.item Every INDEX counts from zero: a row, a column, a segment, a
byte offset. A STRETCH of columns is start to end, the end
exclusive; a whole row is 0 to 40. Each position has one origin:
a column counts from the start of its row, a byte offset from the
start of the input. The row count, k and a target's length are
counts, not indexes.

# Records

The first byte of a record says its kind:

.table 12L *L
byte | record
00..FE | a row record, the byte its row number, 0 to 254
FF | a row count record
.end

.pre
row record:        row  role  k  (end fg bg tlen target) x k  text
row count record:  FF   N
.end

.term row
The row number, 0 to 254.
.term role
What the row is, from an append-only registry: 0 none, 1 header
(the labels of the rows below), 2 total (a row that sums the rows
above). 3 to 255 are reserved: valid in the bytes, kept by a
decoder and re-encoded unchanged, and read as none by a renderer
that does not know them.
.term k
The number of segments, 1 to 40.
.term end
A segment's end column, 1 to 40, rising strictly; the last
segment's end is 40. A segment starts where the one before it
ends, the first at 0.
.term fg, bg
The segment's foreground and background, palette indices 0 to 7.
8 to 255 are reserved and invalid.
.term tlen, target
0 for a segment that is not a link. Otherwise 1 to 255, followed by
that many bytes of target: a URI reference as RFC 3986 writes it,
in ASCII from its unreserved and reserved sets, any other
character percent-encoded as `%` and two hex digits.
.term text
Exactly 40 code points of UTF-8, 40 to 160 bytes, with no length
field: a reader decodes 40 code points and the record ends there.
Segment i's text is its end less its start code points of it, in
order.
.term N
The row count, 0 to 255.

Records follow each other with no separator.

# The palette

.table 8R *L
index | colour
0 | the theme's default: the text colour as foreground, the page as background
1 | red
2 | green
3 | yellow
4 | blue
5 | magenta
6 | cyan
7 | white
.end

A theme maps the indices to colours. There is no black.

# The text

Every code point of the text is one column wide. The text is in
NFC and holds no control character (Cc), no line or paragraph
separator (U+2028, U+2029), no combining or zero-width code point
(Mn, Me, Cf), and nothing of East Asian Wide or Fullwidth width.
Code points Unicode calls ambiguous in width count as one column.

# Segments and links

A SEGMENT is a stretch of a row in one foreground and one
background, and a LINK if it has a target. A link is exactly one
segment. A renderer that lets a link be activated reports the
row, the segment's start and end, and the target; the format never
resolves or follows a target.

# The canonical form

A row is valid only in its one canonical form. A SPACE here is
U+0020 alone, and a segment HOLDS A GLYPH when its text has a code
point other than a space.

.item The segments cover the row's columns in order.
.item A segment that holds no glyph has foreground 0.
.item Two segments that meet and are not links, on one background,
differ in foreground; the earlier holds a glyph, and the later
starts on one.
.item Two links that meet differ in target.

A decoder rejects a row that is not valid and canonical; it does
not normalise. What decodes re-encodes to the same bytes.

# The page

A renderer that takes updates holds a PAGE: a row count H and rows
0 to H - 1. A BLANK row is one segment of forty spaces, foreground
and background 0, with no link and role 0; a row never written is
blank.

.item A row record replaces its row and, if the row is H or more,
raises H to the row plus one, the rows between growing blank.
.item A row count record sets H to N: rows from N on are removed,
and a page shorter than N grows blank rows to N.
.item Records apply in order.

A WHOLE PAGE is encoded as FF 00, its non-blank rows in ascending
order, then FF H; applied to a page in any state it leaves that
page. An update is any sequence of records.

# Rendering

.term the column grid
A renderer gives every column a box of whole pixels -- the font's
character advance by a fixed line height -- and places each code
point in its column's box, clipped to it, never letting a font's
advance decide a position.
.term the drawn set
These seventeen code points are drawn as shapes to their box's
edges, never taken from a font: ─ │ ┌ ┐ └ ┘ ├ ┤ ┬ ┴ ┼ as lines from
the box's centre to its edges; █ ▀ ▄ as blocks; ░ ▒ ▓ as shades
patterned on absolute pixels, so they continue across boxes. The
set grows only by appending.

A renderer owes the text, the column grid and the links; colours
and roles are optional, and what it draws for a role is its own.
A renderer that cannot draw shapes, a plain text dump, uses the
font. Rows beyond a fixed screen stay reachable by scrolling or
paging. Rows are positions from the top.

# Example

Row 3, role none: `[1]` a link to 888 in the default colours, the
story in red from its mark through the spaces before the age, the
age in the default colours.

.pre
[1] ● Fog closes Larnaca approach    12s

03 00 03  03 00 00 03 38 38 38  25 01 00 00  28 00 00 00  <text>
row r k   end fg bg len "888"   end fg bg len end fg bg len
.end

18 bytes of record and 42 of text, 60 in all.

Rationale, history and what is open: `DESIGN.t`, beside this file.

.width 72
.cols 1
.font sans
