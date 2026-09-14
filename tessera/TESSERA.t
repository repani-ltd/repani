TESSERA -- A PAGE OF 34 BY 28 BY 4
.date 2026-09-14
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem Format specification. "The page" is normative; "The tile" is
.rem withdrawn, see below.

A tessera is a mosaic tile. A tessera page is a raster (RASTER.t,
repani.com/typeset/raster) of 34 columns by 28 rows by 4 panels:
3,808 cells of colored text, 7,616 bytes. A page holds
teletext-sized information -- a masthead, a weather panel, a
schedule, a notice -- and nothing longer.

Everything about cells, ink and authoring is RASTER.t's, normative
here by reference and unchanged. This document states only what
tessera adds: the geometry.

# The page

The geometry is C = 34, R = 28, P = 4, so a panel is 952 cells,
the page 3,808 cells and 7,616 bytes, and RASTER.t's formula reads

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

Withdrawn 2026-09-14. The tile was seven whole rows of one-byte
cells, 238 bytes, one quietcasting slot, sixteen to the page.
When RASTER.t went to two bytes a cell, the page became 7,616
bytes, which no whole number of rows divides into 238-byte
slots, and the in-band ink that let a tile render alone went
with it. The radio's representation of a page is now a binding
of its own, to be specified separately (TASKS.t, "quietcasting
binding of raster"); until it is, tessera is the geometry alone.

The vector that survives the change: "TESSERA" in yellow at
panel 2, row 3, column 6 is cell 2×952 + 3×34 + 6 = 2012, bytes
4024 and 4025 of the page, 54 03, the T and its ink, and nothing
else on the page is nonzero.

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
