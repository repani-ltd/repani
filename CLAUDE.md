# tessera

One page of sixteen tiles on the quietcast carousel: a station's
whole carousel (16 × 238 = 3,808 bytes) is one page of colored
cells, four panels of 28 rows × 34 columns, one slot = one tile of
seven rows. Teletext-sized information only; the successor
direction to `../zine` (parked), which it borrows the cell
repertoire, palette and authoring language from.

- TESSERA.t is the specification AND the decision ledger — read
  it before proposing changes. Sections through "Authoring" are
  normative; "Decisions" records why; "Parked designs" carry
  admission tests.
- Core commitments: slot order is page order (no placement field,
  no page number, no mark, no version byte); the page is a
  contiguous 3,808-byte raster; ink is in band, row-scoped
  (0x80+fg, 0x88+bg, palette of default plus seven hues), so
  every tile renders alone; all 256 cell
  values defined, the table grows append-only; nothing flows
  across a panel; no links or navigation (the renderer's chrome);
  the renderer owns the cell's shape.
- No code yet: go.mod claims `repani.com/tessera`; the compiler
  (zine language → raster) and renderers come when a station
  needs them. Zine's transcoder and Greek tables are reused
  byte-for-byte.
