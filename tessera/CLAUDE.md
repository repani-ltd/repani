# tessera

A page of sixteen tiles: 3,808 cells of colored text, four panels
of 28 rows × 34 columns, in tiles of seven rows (238 bytes), the
tile being the unit of update. Sized to quietcasting's slots, one
slot per tile in order. Teletext-sized information only. Import path
`repani.com/tessera`, a project of the public module.

- TESSERA.t is the specification — read it before proposing
  changes. Sections through "Authoring" are normative; "Parked
  designs" carry admission tests. Rationale is not recorded here;
  if a decision ever needs its history, it gets a separate
  document.
- Core commitments: a tile is its position (no placement field,
  no page number, no mark, no version byte); the page is a
  contiguous 3,808-byte raster; ink is in band, row-scoped
  (0x80+fg, 0x88+bg, palette of default plus seven hues), so
  every tile renders alone; all 256 cell values defined, the
  table grows append-only; nothing flows across a panel; no links
  or navigation (the renderer's chrome); the renderer owns the
  cell's shape.
- No code yet: the compiler (source → raster) and renderers come
  when a station needs them; when a CLI ships it follows the
  module's rules (`tessera/cmd/tessera`, `spec` and `check`,
  pkg.fact). The predecessor zine
  (`~/repos/_attic/zine`) holds a transcoder and Greek tables
  that carry over byte-for-byte.
