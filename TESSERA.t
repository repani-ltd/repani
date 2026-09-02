TESSERA -- ONE PAGE OF SIXTEEN TILES ON THE QUIETCAST CAROUSEL
.date 2026-09-02
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem Format specification and decision ledger. Sections up to and
.rem including "Authoring" are normative; "Decisions" records why.

A tessera is a mosaic tile. A station's whole quietcast carousel
-- sixteen slots of 238 bytes, 3,808 bytes in all -- is one page
of colored cells, and each slot is one tile of it. The page is
teletext-sized information: a masthead, a weather panel, a
schedule, a notice. Nothing longer lives here; a station that has
more to say has more stations. Tessera takes from zine what
survived contact with the transport (the cell repertoire, the ink
palette, the authoring language) and drops everything the
transport now does for free (page numbers, run headers,
placement, marks) and everything that is the renderer's chrome
(links, navigation).

# The page

A PAGE is 3,808 cells, one byte each, arranged as four PANELS of
28 rows by 34 columns, read in order 0 to 3. The page is a
contiguous raster: byte i of the page is

.pre
    panel  = i div 952
    row    = (i mod 952) div 34        (0..27, within the panel)
    column = i mod 34                  (0..33)
.end

so a panel is 952 consecutive bytes in row-major order, and the
page is the four panels back to back. Every cell is content;
there are no special rows, no headers, no trailers.

A TILE is 238 consecutive bytes of the page: bytes 238k through
238k+237 form tile k, for k in 0..15. Because 238 is exactly
seven rows of 34, a tile is always seven whole rows of one panel:
tile k holds rows 7(k mod 4) through 7(k mod 4)+6 of panel k div
4. Tiles are the transport's unit, never the author's: an author
sees panels, rows and columns, and a tile boundary is invisible
in content.

A panel is the unit of flow. Content may run down a panel's 28
rows freely and may cross tile boundaries; it never continues
from one panel into another. The reason is that a renderer may
show the four panels in any arrangement -- side by side, stacked,
in a two-by-two grid, one at a time -- and a flow that crossed a
panel edge would be broken by every arrangement but one.

The renderer chooses the cell's shape. The format never states a
glyph aspect, a font, or a pixel; a terminal's 1:2 cell makes a
panel a 34-by-56 golden rectangle and the two-by-two page the
same rectangle at twice the size, a bespoke near-square font
makes both close to square, and both are correct.

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
to row, so every tile renders on its own with no knowledge of
its neighbours. A code costs the cell it sits in, which in
practice is the word gap before the colored span.

The palette is teletext's: the renderer's default and seven
hues, which a renderer themes:

.pre
    0 default    2 green     4 blue      6 cyan
    1 red        3 yellow    5 magenta   7 white
.end

Entry 0 is the renderer's own foreground or background -- the
terminal's, the theme's -- so an uncolored page reads correctly
in every theme.

# The carousel binding

Slot k of the station's carousel carries tile k, sealed as any
data packet. Slot order is page order: no byte of the page says
where it goes, and a receiver places a decrypted slot value by
its slot number alone. The page is therefore well-defined under
every reception: each cell has exactly one tile that owns it,
tiles arrive in any order and any subset, and a partial carousel
renders exactly its received tiles in their places. Which tiles
are current is the manifest's claim, and what a renderer does
with a stale or missing tile -- blank it, grey it, keep the last
value -- is presentation.

A station never pads: the page is always exactly 3,808 bytes and
unwritten cells are 0x00, so identical content is identical
bytes, slot by slot, and re-publishing an unchanged page changes
no packet. A one-line edit changes one tile, and only that slot
is re-sealed.

Frozen vector: "HELLO" in yellow at panel 2, row 3, column 5 is
page offset 2×952 + 3×34 + 5 = 2011, which is slot 8 (2011 div
238), bytes 107 through 112 of its value:

.pre
    83 48 45 4C 4C 4F
.end

with the ink code at column 5 and the H at column 6.

Nothing in the 3,808 bytes says what format they are or which
revision of it: there is no in-band mark and no version byte. A
receiver cannot read a station's slots without that station's
key and trust anchor, which it was given out of band, and the
same out-of-band record that carries the key says "this station
is a tessera page". A revision that only appends to the cell
table needs no announcement, since an older renderer shows the
new cells as blanks. A revision that changes the layout is a new
convention, declared in that record for a new station, never a
version of this one.

# Authoring

Pages are authored in the zine language, unchanged in its lexical
family and its five commands, with the coordinate space and the
ink mechanism of this format:

.pre
    .panel N         target panel 0..3 (required first); cursor
                     to row 0 column 0, pen to default ink
    .at R C          cursor to row R (0..27), column C (0..33);
                     invalidates the "+" pen
    .ink FG          set the pen: foreground, optionally
    .ink FG on BG    "on" a background, by palette name
    content          one run at the cursor in the pen's ink; the
                     cursor then drops one row, same column.
                     Right-trimmed; ". " and ".." begin content
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

Long documents are not authored here at all. A pica document
pressed at width 34 into panels of 28 rows is a pica writer's
output and a tessera source's input; that writer does not exist
yet, and this format knows nothing of pica.

# Non-goals

.item No transport, no store, no schedule: quietcasting owns the
air and the manifest owns currency; what to re-send and when is
the station's policy.
.item No page numbers, no addresses, no placement: a station is a
page; a slot is a position.
.item No links, no navigation, no buttons: a page is content only.
Moving between stations is the renderer's chrome, and the page
spends no row on it.
.item No mark, no version byte, no reserved fields: the appended
cell table and a successor convention are the only two gears.
.item No mosaics yet, no general Unicode, no double height, no
flashing: see the parked designs.
.item No glyph metrics: the renderer owns the cell's shape.

# Decisions

.item Why 34 by 7 is the tile. The carousel is sixteen 238-byte
registers and 238 is 2×7×17; the only row widths that divide it
are 1, 2, 7, 14, 17, 34, 119 and 238. At 34 a tile is seven
whole rows with no leftover byte, at 56 it is four rows plus 14
bytes nothing consumes, at 68 three rows plus 34. A raster cut at
56 or 68 would put a slot boundary inside twelve rows of the
page, so a mixed-vintage carousel would show rows half old and
half new. Zero padding, no torn rows, and the razor all point at
34.
.item Why the panel is 28 rows. Four tiles are 952 bytes, which
is 28 rows of 34 -- the panel is a contiguous raster with no
arithmetic across tiles -- and 952 cells is a teletext screen to
within eight cells (40×24 is 960). At the terminal's 1:2 cell a
panel is 34 by 56, a golden rectangle to three decimals
(1.647 against 1.618), and the two-by-two page is the same
rectangle doubled. The 34-wide measure is below teletext's 40
and well below Bringhurst's 66; for tables, listings and
notices it holds, and the genre was scoped to those before the
width was chosen. Prose is the press's problem, and the press
is pica's.
.item Why four panels and not two halves or one sheet. Two
halves of 56 rows are strips a terminal cannot show whole; one
sheet of 112 rows is a scroll. Four panels give the renderer
every layout -- a row, a column, a grid, a carousel of four --
and give the author one rule: nothing crosses a panel. Sixteen
independent tiles were considered and rejected: a seven-row cap
on every table is too tight for the very content the page is
for.
.item Why slot order is page order. A placement field would let
a station move a tile without rewriting the others, which
matters for messages and files, not for a living page that is
re-aired whole on every rotation anyway. What the field would
cost is not a lookup but a set of rules: two slots claiming one
place, a place nobody claims, a stale slot still pointing
somewhere. Fixed positions make the page well-defined by
construction and keep zine's invariant -- no cell painted by two
live slots -- without an allocator. If placement is ever
demanded it arrives as a successor convention with a header.
.item Why ink went in band. The run header that carried zine's
attribute byte is gone with the run; the choices were no color,
or teletext's spacing attribute. Color is structural to the genre
(mastheads, highlighted values), so the code cell
stays, row-scoped so that a tile never depends on its neighbour
and every row starts clean. The code takes the new state itself
(set-at) rather than teletext's set-after, which was a hardware
pipeline artifact: a bar starts at its code and ends at its
closing code, with no off-by-one for authors to learn. The true
cost is paid in every cell, not only the colored ones: sixteen
of the byte's 256 values are codes, so a cell carries one of 240
meanings, about a tenth of a bit less than a full byte. That,
and the cell a code occupies, is disliked and accepted: the
alternatives all pay more elsewhere. A per-cell attribute needs a second byte per
cell, halving the page; an attribute bit inside the cell byte
halves the repertoire, which is the Greek page; an attribute row
per panel spends a row on every panel whether colored or not;
and no color at all loses the masthead and the highlighted
value. Sizes were weighed too: thirty-two codes (sixteen
foregrounds and backgrounds, with bright variants) doubled the
codespace cost to carry variants that are a terminal artifact no
theme can map honestly; eight codes (seven hues and teletext's
"new background") cannot give independent foreground and
background, so a bar with colored text costs three cells and a
small state machine. Sixteen is the cheapest price on offer,
and it stands until a better one is proposed.
.item What made room for the ink codes. Zine's 64 mosaics
occupied 0x80..0xBF; their only consumer was the parked .pic
drawing language, whose admission test (a station wanting a
chart) has not been met. Sixteen ink codes take the first
quarter of that range; the rest is unassigned and renders blank.
The symbol set, ASCII, the euro and the Greek page carry over
byte-for-byte, so zine's transcoder and Greek tables are reused
unchanged.
.item Why there is no mark. Zine marked itself with 0xFE because
a carousel's default content was plain text and several formats
could share slots. Here the whole carousel is the page and the
receiver holds the station's key before it hears a byte; the
declaration that a station is a tessera page lives where the key
lives. Self-description on the wire would be a byte with no
reader.
.item What was dropped from zine, and why. Page numbers (the
slot is the address). The 5-byte run and the run stream (the
page is a raster). The flag bit and format byte (no minor or
major seam is needed when the page is fixed-size and the cell
table is append-only). The 2,048-row tall page and the board
custom (the panel is the board; long text is excluded by scope).
Blank-never-transmitted (a blank is a byte like any other, and
determinism is worth more than the bytes). Links, in every form
(a page is content; where a reader goes next is the renderer's
chrome, and a row spent on navigation is a row not spent on
information).
.item What was kept. The cell repertoire and the Greek page. The
semantic palette, cut to teletext's seven hues plus the
renderer's default. The five-command language and its rules (right-trim, "+"
continuation, .fill as the explicit way to clear). Conventions
over mechanism, with the customs below left to the community.

# Customs (non-normative)

Row 0 of panel 0 is the page's title. Which other rows carry
which roles -- a clock line, a priority line -- is decided when a
station needs them. None of this is wire.

# Parked designs, with their admission tests

.item Mosaics. The 2×2 quadrant set (16 patterns) fits the
unassigned range 0x90..0x9F and would be the first append; the
2×3 sextants do not fit and would need the Greek page's
neighbour or a second ink strategy. ADMISSION TEST: the first
station that wants a chart or a logo, as for zine's .pic.
.item Panel placement. A header that lets a tile name its
position. ADMISSION TEST: a station that must reorder content
without re-sealing the page, which a living page has no reason to
do.
.item General Unicode. The repertoire is 256 runes and the
ink codes now share the byte with it; a script beyond the
repertoire needs a successor convention, not an append.
ADMISSION TEST: the first station that needs one.
.item The press. A pica text writer at width 34 emitting tessera
source in panels of 28 rows, headings in yellow as zine
designed. ADMISSION TEST: the first station with
a document rather than a board.

.width 72
.cols 1
.font sans
