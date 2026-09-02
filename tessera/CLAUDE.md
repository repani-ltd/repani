# tessera

One page of sixteen tiles on the quietcasting carousel: a station's
whole carousel (16 × 238 = 3,808 bytes) is one page of colored
cells, four panels of 28 rows × 34 columns, one slot = one tile of
seven rows. Teletext-sized information only. Import path
`repani.com/tessera`, a project of the public module.

- TESSERA.t is the specification — read it before proposing
  changes. Sections through "Authoring" are normative; "Parked
  designs" carry admission tests. Rationale is not recorded here;
  if a decision ever needs its history, it gets a separate
  document.
- Core commitments: slot order is page order (no placement field,
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
