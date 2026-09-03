TESSERA -- A PAGE OF SIXTEEN TILES
.date 2026-09-02
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem Format specification. Sections through "Authoring" are normative.

A tessera is a mosaic tile. A tessera page is 3,808 cells of
colored text, one byte per cell, in sixteen tiles of 238 bytes.
The tile is the unit of update: a page changes tile by tile, and
each tile is complete on its own. A page holds teletext-sized
information -- a masthead, a weather panel, a schedule, a notice
-- and nothing longer. The tile is sized to a quietcasting slot,
and a station's sixteen slots carry the sixteen tiles in order;
that is the whole of the relation.

# The page

A PAGE is four PANELS of 28 rows by 34 columns, read in order 0
to 3, as one contiguous raster: byte i of the page is

.pre
    panel  = i div 952
    row    = (i mod 952) div 34        (0..27, within the panel)
    column = i mod 34                  (0..33)
.end

so a panel is 952 consecutive bytes in row-major order, and the
page is the four panels back to back. Every cell is content;
there are no special rows, no headers, no trailers.

A panel is the unit of flow. Content may run down a panel's 28
rows freely; it never continues from one panel into another. A
renderer may show the four panels in any arrangement -- side by
side, stacked, in a two-by-two grid, one at a time -- and a flow
that crossed a panel edge would break in every arrangement but
one.

The renderer chooses the cell's shape. The format states no glyph
aspect, font, or pixel; a terminal's 1:2 cell makes a panel a
34-by-56 golden rectangle and the two-by-two page the same
rectangle at twice the size, a bespoke near-square font makes
both close to square, and both are correct.

# The tile

A TILE is seven whole rows: 238 consecutive bytes of the page,
bytes 238k through 238k+237 forming tile k for k in 0..15, which
is rows 7(k mod 4) through 7(k mod 4)+6 of panel k div 4. The
tile is the smallest thing that changes: a one-line edit rewrites
one tile and no other. Tiles are invisible to the author, who
sees only panels, rows and columns; a tile boundary never falls
inside a row.

A tile is identified by its position and by nothing in its bytes.
Tiles may therefore arrive in any order and any subset, and a
page assembled from whatever tiles have arrived is exactly right
where they are and blank where they are not; what a renderer
shows for a missing tile -- blank, grey, the last value -- is
presentation. Every row starts in default ink (see Ink), so a
tile renders correctly with no knowledge of its neighbours.

A page is always exactly 3,808 bytes and unwritten cells are
0x00, so identical content is identical bytes, tile by tile: an
unchanged page changes no tile.

Frozen vector: "TESSERA" in yellow at panel 2, row 3, column
5 is page offset 2×952 + 3×34 + 5 = 2011, which is tile 8 (2011
div 238), bytes 107 through 114 of it:

.pre
    83 54 45 53 53 45 52 41
.end

with the ink code at column 5 and the T at column 6.

# Cells, ink and authoring

A tessera page is a raster (repani.com/typeset/raster, RASTER.t)
of 34 columns by 28 rows by 4 panels. The cell table (all 256
values, the Greek page, the weather glyphs), the in-band
row-scoped ink with its palette of default plus seven hues, and
the dot-command authoring language are RASTER.t's, normative
here by reference and unchanged: .panel takes 0..3, .at rows
0..27 and columns 0..33. Compilation is reproducible: the same
source yields the same 3,808 bytes.

# Non-goals

.item No addresses, no placement, no page numbers: a tile is its
position.
.item No links, no navigation, no buttons: a page is content only.
.item No mark, no version byte, no reserved fields. Nothing in
the bytes says what format they are: that is declared wherever
the page itself is. A revision that appends to the cell table
needs no announcement, since an older renderer shows the new
cells as blanks; a revision that changes the layout is a new
format, never a version of this one.
.item No mosaics yet, no general Unicode, no double height, no
flashing: see the parked designs, here and in RASTER.t.
.item No glyph metrics: the renderer owns the cell's shape.

# Parked designs, with their admission tests

.item Tile placement. A header that lets a tile name its
position. ADMISSION TEST: a page that must reorder content
without rewriting itself, which a living page has no reason to
do.
.item Mosaics and a second repertoire: RASTER.t, parked there
for every raster format at once.

.width 72
.cols 1
.font sans
