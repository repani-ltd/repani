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

# Cells

All 256 byte values are defined. Values not assigned below render
as a blank; the table grows by appending, never by reassigning.

.pre
    0x00        blank (a space; the value of every unwritten cell)
    0x01..0x0B  box drawing  ─ │ ┌ ┐ └ ┘ ├ ┤ ┬ ┴ ┼
    0x0C..0x11  double box   ═ ║ ╔ ╗ ╚ ╝
    0x12..0x15  arrows       ← ↑ → ↓
    0x16..0x18  shades       ░ ▒ ▓
    0x19..0x1F  symbols      ° ± × ÷ • · §
    0x20..0x7E  ASCII
    0x7F        €
    0x80..0x87  INK: foreground palette 0..7 (see Ink)
    0x88..0x8F  INK: background palette 0..7
    0x90..0xBF  unassigned: render blank
    0xC0..0xD8  Greek lowercase  α β γ δ ε ζ η θ ι κ λ μ ν ξ ο π
                ρ ς σ τ υ φ χ ψ ω
    0xD9..0xE3  accented        ά έ ή ί ό ύ ώ ϊ ϋ ΐ ΰ
    0xE4..0xFB  Greek uppercase Α Β Γ Δ Ε Ζ Η Θ Ι Κ Λ Μ Ν Ξ Ο Π
                Ρ Σ Τ Υ Φ Χ Ψ Ω   (no tonos on capitals, the
                standard Greek typographic convention)
    0xFC..0xFF  « » … ―
.end

Content is authored in UTF-8 and transcoded; the repertoire is
the contract, and a rune outside it is an authoring error, never
a substitution.

# Ink

Color travels in band, teletext-style. An INK CODE occupies a
cell, renders as a blank in the state it establishes, and sets
one attribute for the rest of its row: 0x80+n sets the
foreground to palette entry n, 0x88+n the background. Every row
begins in foreground 0 on background 0; nothing carries from row
to row. A code costs the cell it sits in, which in practice is
the word gap before the colored span.

The palette is teletext's: the renderer's default and seven
hues, which a renderer themes:

.pre
    0 default    2 green     4 blue      6 cyan
    1 red        3 yellow    5 magenta   7 white
.end

Entry 0 is the renderer's own foreground or background -- the
terminal's, the theme's -- so an uncolored page reads correctly
in every theme.

# Authoring

Pages are authored in a line-oriented dot-command language:
content lines are content, the command set is closed, and a line
that lexes as a command (a dot, then a lowercase letter) but is
not one of the five is an error.

.pre
    .panel N         target panel 0..3 (required first); cursor
                     to row 0 column 0, pen to default ink
    .at R C          cursor to row R (0..27), column C (0..33);
                     invalidates the "+" pen
    .ink FG          set the pen: foreground, optionally
    .ink FG on BG    "on" a background, by palette name
    content          one run at the cursor in the pen's ink; the
                     cursor then drops one row, same column.
                     Right-trimmed; ". " and ".." begin content;
                     an empty line flows one row and writes nothing
    + content        continue on the same row where the last run
                     ended
    .fill R C W H    a W-by-H region of spaces at R C in the pen's
                     ink: bars, panels, grounds
    .rem TEXT        comment, dropped
.end

The compiler owns the code cells. Before a run it emits, at the
cursor, one ink code for each attribute in which the pen differs
from the row's state at that cell, then the run; a change of
foreground alone costs one cell, foreground and background two.
A fill emits its codes at its left edge on every row it covers,
and closes them at its right edge when that edge is inside the
row. Text placed over a fill inherits the fill's background from
the codes to its left, so restating it is unnecessary; a run that
would overwrite a code cell is an error, not a silent recolor.
Compilation is reproducible: the same source yields the same
3,808 bytes.

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
flashing: see the parked designs.
.item No glyph metrics: the renderer owns the cell's shape.

# Parked designs, with their admission tests

.item Mosaics. The 2×2 quadrant set (16 patterns) fits the
unassigned range 0x90..0x9F and would be the first append; the
2×3 sextants do not fit. ADMISSION TEST: the first page that
wants a chart or a logo.
.item Tile placement. A header that lets a tile name its
position. ADMISSION TEST: a page that must reorder content
without rewriting itself, which a living page has no reason to
do.
.item General Unicode. The repertoire is 256 values and the
ink codes share the byte with it; a script beyond the repertoire
needs a new format, not an append. ADMISSION TEST: the first
page that needs one.

.width 72
.cols 1
.font sans
