# tessera

A raster (`repani.com/typeset/raster`) of 34 columns by 28 rows by
4 panels, 3,808 bytes, carried as sixteen tiles of 238 bytes, one
per quietcasting slot in order. Teletext-sized information only.
Import path `repani.com/tessera`, a project of the public module.

- TESSERA.t is the specification: the geometry and the tile. Cells,
  ink and the authoring language are RASTER.t's, normative by
  reference; never restate or re-implement them here. "Parked
  designs" carry admission tests. Rationale is not recorded here;
  if a decision ever needs its history, it gets a separate
  document.
- Core commitments: a tile is its position (no placement field,
  no page number, no mark, no version byte); the page is a
  contiguous 3,808-byte raster; a change of geometry or tile is a
  new format.
- Code: tessera.go is the constants, the tile, the raster view and
  Compile. The CLI (`tessera/cmd/tessera`) ships `spec` (TESSERA.t,
  then RASTER.t, then its usage) and `check`; rendering goes
  through the raster view.
