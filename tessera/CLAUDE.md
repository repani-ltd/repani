# tessera

A page of sixteen tiles: 3,808 cells of colored text, four panels
of 28 rows × 34 columns, in tiles of seven rows (238 bytes), the
tile being the unit of update. Sized to quietcasting's slots, one
slot per tile in order. Teletext-sized information only. Import path
`repani.com/tessera`, a project of the public module.

- TESSERA.t is the specification — read it before proposing
  changes. It states the page and the tile; the cell table, ink
  and authoring language are the raster's
  (`repani.com/typeset/raster`, RASTER.t), normative by
  reference. "Parked designs" carry admission tests. Rationale is
  not recorded here; if a decision ever needs its history, it
  gets a separate document.
- Core commitments: a tile is its position (no placement field,
  no page number, no mark, no version byte); the page is a
  contiguous 3,808-byte raster; ink is in band, row-scoped
  (0x80+fg, 0x88+bg, palette of default plus seven hues), so
  every tile renders alone; all 256 cell values defined, the
  table grows append-only; nothing flows across a panel; no links
  or navigation (the renderer's chrome); the renderer owns the
  cell's shape.
- Code: tessera.go is the geometry, the tile and thin methods
  over the raster (compile, text, ANSI, HTML); the compiler and
  renderers live in typeset/raster. The CLI (`tessera/cmd/tessera`)
  ships `spec` (tessera's reference, then raster's) and `check`.
  Never re-implement a cell, ink or language rule here: it is
  raster's, shared by every raster format.
