RASTER -- A PAGE OF COLORED CELLS
.date 2026-09-03
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem Format specification. Sections through "Authoring" are normative.

A raster is a page of colored text cells, one byte per cell, in
panels of rows by columns. The geometry -- columns, rows, panels
-- is the instantiating format's, and nothing else is: the cell
repertoire, the ink model and the authoring language are fixed
here, so every raster format shares them and every raster tool
reads every raster page. Tessera (repani.com/tessera) is the
first instantiation, 34 by 28 by 4, sized to quietcasting's
slots; a notice board of forty-column cards is another.

# The page

A PAGE is P PANELS of R rows by C columns, read in order 0 to
P-1, as one contiguous raster: byte i of the page is

.pre
    panel  = i div (R×C)
    row    = (i mod (R×C)) div C     (0..R-1, within the panel)
    column = i mod C                 (0..C-1)
.end

so a panel is R×C consecutive bytes in row-major order, and the
page is the panels back to back. Every cell is content; there
are no special rows, no headers, no trailers. Unwritten cells
are 0x00, so identical content is identical bytes.

A panel is the unit of flow. Content may run down a panel's rows
freely; it never continues from one panel into another. A
renderer may show the panels in any arrangement -- side by side,
stacked, in a grid, one at a time -- and a flow that crossed a
panel edge would break in every arrangement but one.

The renderer chooses the cell's shape. The format states no
glyph aspect, font, or pixel.

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
    0x90..0x97  weather and marine  ☀ ☁ ☂ ☾ ❄ ↯ ⚓ ⚠
    0x98..0xBF  unassigned: render blank
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
to row, so every row renders alone. A code costs the cell it
sits in, which in practice is the word gap before the colored
span.

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
    .panel N         target panel 0..P-1 (required first); cursor
                     to row 0 column 0, pen to default ink
    .at R C          cursor to row R (0..R-1), column C (0..C-1);
                     invalidates the "+" pen
    .ink FG          set the pen, by palette name: FG on the
    .ink FG on BG    default background, or FG on BG
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
from the row's state at that cell (background first), then the
run; a change of foreground alone costs one cell, foreground and
background two. A fill emits its codes at its left edge on every
row it covers, and closes them at its right edge when that edge
is inside the row. Text placed over a fill inherits the fill's
background from the codes to its left, so restating it is
unnecessary; a run that would overwrite a code cell is an error,
not a silent recolor. Compilation is reproducible: the same
source on the same geometry yields the same bytes.

# Non-goals

.item No geometry of its own: a raster format states its P, R
and C; this specification states none.
.item No links, no navigation, no buttons: a page is content only.
.item No mark, no version byte, no reserved fields. Nothing in
the bytes says what format they are: that is declared wherever
the page itself is. A revision that appends to the cell table
needs no announcement, since an older renderer shows the new
cells as blanks.
.item No mosaics yet, no general Unicode, no double height, no
flashing: see the parked designs.
.item No glyph metrics: the renderer owns the cell's shape.

# Parked designs, with their admission tests

.item Mosaics. The 2×2 quadrant set (16 patterns) fits the
unassigned range and would be the first append; the 2×3
sextants do not fit. ADMISSION TEST: the first page that wants a
chart or a logo.
.item A second repertoire. The table is fixed for every raster
format, which is what lets every raster tool read every page; a
script beyond it needs a new format, not a parameter. ADMISSION
TEST: the first page that needs one.
.item A canvas with out-of-band ink. A per-cell model (rune,
foreground, background) with Decode from the bytes, expanding
the codes, and Encode back, paying each code's cell where the
ink changes and refusing where no blank cell can hold it.
ADMISSION TEST: a writer that paints cells before it knows the
gaps -- the pica-to-tessera writer.

.width 72
.cols 1
.font sans
