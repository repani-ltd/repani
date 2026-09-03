TESSERA -- A PAGE OF SIXTEEN TILES
.date 2026-09-02
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem Format specification. Sections through "The tile" are normative.

A tessera is a mosaic tile. A tessera page is a raster (RASTER.t,
repani.com/typeset/raster) of 34 columns by 28 rows by 4 panels:
3,808 cells of colored text, one byte per cell, carried as sixteen
tiles of 238 bytes. The tile is the unit of update: a page changes
tile by tile, and each tile is complete on its own. A page holds
teletext-sized information -- a masthead, a weather panel, a
schedule, a notice -- and nothing longer. The tile is sized to a
quietcasting slot, and a station's sixteen slots carry the sixteen
tiles in order; that is the whole of the relation.

Everything about cells, ink and authoring is RASTER.t's, normative
here by reference and unchanged. This document states only what
tessera adds: the geometry and the tile.

# The page

The geometry is C = 34, R = 28, P = 4, so a panel is 952 bytes,
the page 3,808, and RASTER.t's formula reads

.pre
    panel  = i div 952
    row    = (i mod 952) div 34        (0..27)
    column = i mod 34                  (0..33)
.end

The authoring bounds follow: .panel takes 0..3, .at rows 0..27
and columns 0..33.

The shape is the renderer's, as RASTER.t says; on this geometry
a terminal's 1:2 cell makes a panel a 34-by-56 golden rectangle
and the two-by-two page the same rectangle at twice the size, a
bespoke near-square font makes both close to square, and both
are correct.

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
presentation. Every row carries its own ink (RASTER.t, "Ink"),
so a tile renders correctly with no knowledge of its neighbours,
and unwritten cells are 0x00, so an unchanged page changes no
tile.

Frozen vector: "TESSERA" in yellow at panel 2, row 3, column
6, its ink code in the gap at column 5, which is page offset
2×952 + 3×34 + 5 = 2011, tile 8 (2011 div 238), bytes 107
through 114 of it:

.pre
    83 54 45 53 53 45 52 41
.end

the source being ".panel 2", ".at 3 6", ".fg yellow", "TESSERA".

# Non-goals

Beyond RASTER.t's:

.item No addresses, no placement, no page numbers: a tile is its
position.
.item A revision that changes the geometry or the tile is a new
format, never a version of this one.

# Parked designs, with their admission tests

.item Tile placement. A header that lets a tile name its
position. ADMISSION TEST: a page that must reorder content
without rewriting itself, which a living page has no reason to
do.

.width 72
.cols 1
.font sans
