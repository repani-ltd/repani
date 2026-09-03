/*
Package tessera is a page format: 3,808 cells of colored text, one
byte per cell, in sixteen tiles of 238 bytes. The tile is the unit of
update and each tile is complete on its own; the tile is sized to a
quietcasting slot, and a station's sixteen slots carry the sixteen
tiles in order. This comment is the operating reference; TESSERA.t is
the specification.

A tessera page is a raster (repani.com/typeset/raster) of 34
columns by 28 rows by 4 panels: the cell table, the in-band ink and
the authoring language are raster's, printed after this reference
by tessera spec, and everything below is what tessera adds.

# The page

A page is four panels of 28 rows by 34 columns, read in order 0..3,
as one contiguous raster: byte i is panel i/952, row (i%952)/34,
column i%34. Content flows freely down a panel and never from one
panel into another, because a renderer may arrange the four panels
as it likes; the renderer also owns the cell's shape.

# The tile

Tile k is bytes 238k through 238k+237, always seven whole rows of
one panel: rows 7(k mod 4) through 7(k mod 4)+6 of panel k div 4.
A tile is identified by its position and by nothing in its bytes, so
tiles arrive in any order and any subset, and a page assembled from
whatever tiles have arrived is exactly right where they are and blank
where they are not. Every row starts in default ink, so a tile
renders correctly with no knowledge of its neighbours. A page is
always exactly 3,808 bytes and unwritten cells are 0x00, so identical
content is identical bytes, tile by tile: an unchanged page changes
no tile.

# Authoring

Source is raster's language (raster spec, "Authoring") on this
geometry: .panel takes 0..3, .at rows 0..27 and columns 0..33.
Compilation is reproducible: the same source yields the same 3,808
bytes.

Frozen vector: ".panel 2", ".at 3 5", ".ink yellow", "TESSERA" puts
83 54 45 53 53 45 52 41 at bytes 107..114 of tile 8 and nothing else.
*/
package tessera
