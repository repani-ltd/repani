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
    0x01..0x02  rules        ─ │
    0x03..0x06  arrows       ← ↑ → ↓
    0x07..0x0A  blocks       ░ ▒ ▓ █
    0x0B..0x10  symbols      ° ± × ÷ • ·
    0x11..0x1F  unassigned: render blank
    0x20..0x7E  ASCII
    0x7F        €
    0x80..0x87  INK: foreground palette 0..7 (see Ink)
    0x88..0x8F  INK: background palette 0..7
    0x90..0x96  weather      ☀ ☁ ☂ ☾ ❄ ↯ ⚠
    0x97..0x9C  typographic  ‘ ’ “ ” – —
    0x9D..0xA2  marks        ☺ ☹ ♥ ★ ✓ ✗
    0xA3..0xA5  status, currency  ● ○ £
    0xA6..0xBF  unassigned: render blank
    0xC0..0xD8  Greek lowercase  α β γ δ ε ζ η θ ι κ λ μ ν ξ ο π
                ρ ς σ τ υ φ χ ψ ω
    0xD9..0xE3  accented        ά έ ή ί ό ύ ώ ϊ ϋ ΐ ΰ  (monotonic)
    0xE4..0xFB  Greek uppercase Α Β Γ Δ Ε Ζ Η Θ Ι Κ Λ Μ Ν Ξ Ο Π
                Ρ Σ Τ Υ Φ Χ Ψ Ω   (no tonos on capitals, the
                standard Greek typographic convention)
    0xFC..0xFF  « » … ―
.end

Every glyph is one column wide in a monospace renderer: its
Unicode East Asian Width is not Wide, and it has text
presentation by default. A glyph that fails this test is not
admitted, whatever its demand, because a cell is a column.

Content is authored in UTF-8 and transcoded; the repertoire is
the contract, and a rune outside it is an authoring error, never
a substitution. The one stated exception is the Greek capital
with tonos or dialytika (Ά Έ Ή Ί Ό Ύ Ώ Ϊ Ϋ), which transcodes to
its plain capital: Greek typography drops the tonos on capitals,
and a place name such as Άραξος is set Αραξος.

# Ink

Every cell has an ink: a FOREGROUND and a BACKGROUND, each an
index into an eight-entry palette. A blank cell shows only its
background. The palette is teletext's: the renderer's default
and seven hues, which a renderer themes:

.pre
    0 default    2 green     4 blue      6 cyan
    1 red        3 yellow    5 magenta   7 white
.end

Entry 0 is the renderer's own foreground or background -- the
terminal's, the theme's -- so an uncolored page reads correctly
in every theme.

Ink travels in band, teletext-style. An INK CODE occupies a
cell, renders as a blank in the state it establishes, and sets
one attribute for the rest of its row: 0x80+n sets the
foreground to palette entry n, 0x88+n the background. Nothing
carries from row to row, so every row renders alone.

A row's TAIL is the codes at its very end: the longest suffix of
the row that is all ink codes. Codes in the tail set the row's
OPENING INK, the state in which the row begins; elsewhere in the
row they render as empty cells and are not applied again. A row
with no code in its last cell has no tail and begins in default
ink. So a red word in the first column costs the row's last cell,
not its first, and a row that is a bar in one background is one
code in its first cell or its last.

The page is therefore two things at once: the CANVAS, every cell
with its glyph and its ink, which is what an author paints and a
renderer shows; and the BYTES, which encode the canvas with the
codes hidden in cells that show nothing. Decoding is a scan of
each row: the tail first, then left to right. Encoding is
canonical, so the same canvas yields the same bytes:

.item A change of background at a blank cell takes that cell.
.item The changes a glyph needs -- background first, then
foreground -- take the blank cells immediately before it, one
per attribute. A glyph in the first cell has none before it, and
its codes go to the tail instead.
.item A cell that a code takes shows a blank in the new ink: the
space before colored text takes its color. The last cell of a
row cannot change background, since a code there would be tail.
.item A canvas that needs a code where no blank cell is -- text
in a new ink glued to text, or a full row that begins in ink --
cannot be encoded, and the compiler says so with the column.

# Authoring

Pages are authored in a line-oriented dot-command language: a
line is one command or one run of content, and the command set
is closed. A page that says everything the language has:

.pre
    .rem A notice: a title bar, a heading, a paragraph, a table.
    .bg blue
    .fill 0
    .fg white
    .at 0 2
    HARBOUR NOTICE · 02 SEP
    .fg yellow
    .bg default
    .at 2
    MELTEMI TONIGHT
    .fg default
    North 7 to 8 from 1800, gusts 9
    in the channel. Double up lines.
    .at 6
    .fg cyan
    FUEL
    .fg
    .col 8
    06:00-14:00, south quay
    .fg red
    ALERT
    .fg
    + north quay closed
    .at 10
    Tap [tides] for the tide table.
.end

The commands:

.pre
    .panel N        switch to panel N (0..P-1); the page starts in
                    panel 0, at row 0
    .margin C       the column where lines start (default 0); persists
    .at R [C]       the next run lands at row R, column C (default:
                    the margin); one-shot
    .fg [NAME]      the pen's foreground; persists until changed;
                    bare, the default
    .bg [NAME]      the pen's background, likewise
    content         one run at the cursor in the pen's ink; the
                    cursor then moves to the next row, at the margin
    + content       continue on the row of the last run, where it
                    ended; the run is everything after the "+"
    .col C          the next run lands at column C of the row of the
                    last run; one-shot, the cursor does not move
    .fill R [C [ROWS [COLS]]]   a region of spaces in the pen's ink;
                    defaults: column 0, one row, to the right edge
    .rem TEXT       comment, dropped
    .def NAME PARAM...          an alias: the lines to .enddef, with
    .enddef         $PARAM standing for a use's words (see Aliases)
.end

The rules:

.item Names are default red green yellow blue magenta cyan white.
Rows, columns and panels count from 0.
.item A line that begins with a dot and a lowercase letter is a
command, and one that is not in the table is an error. A line
that begins with "+ " is a continuation; a lone "+" and "+5" are
content. "+" and .col attach to the last run, and there is none
after .panel or .at.
.item Content is right-trimmed. Leading spaces position the run
and paint nothing, so a run's text lands at the cursor plus its
leading spaces; interior spaces are painted. An empty line, or
one of only spaces, moves the cursor one row and paints nothing.
.item A run that overflows its row, a cursor below the last row,
and a rune outside the repertoire are errors.
.item The pen and the margin are the author's: nothing resets
them, .panel included, which moves only the cursor.
.item Painting is by cell, in source order, later over earlier;
a fill clears what it covers. The bytes are encoded from the
finished canvas, so the order of the source never changes a
color, and compilation is reproducible: the same source on the
same geometry yields the same bytes.
.item Colored text spends cells that show nothing: the blank
before it, one per attribute changed, and, in the first column,
the row's tail. The one full row the format cannot hold is a
full row that begins in ink; the error names the line.
.item A LINK is a bracketed span: an opening bracket and the next
closing bracket on the same row, with at least one cell between
them. The whole span, brackets included, is the tappable region,
and the text between the brackets is its TARGET. What a tap does
with the target is the app's; the page only names it. A link is
derived from the cells, never stored, so it costs nothing in the
bytes and survives every renderer: plain text shows the
brackets, HTML makes the span an anchor, a phone makes it a tap
target. Brackets mean link and nothing else on a raster page.

# Aliases

An ALIAS names a block of lines, so a page's idioms -- a title
bar, a label and its value -- are one line each to write. The
mechanism is raster's; the words are the page's or the app's,
never this specification's.

.pre
    .def bar TITLE
    .fg white
    .bg blue
    .fill 0
    .at 0 2
    $TITLE
    .fg
    .bg
    .enddef
    .def field LABEL VALUE
    .fg cyan
    $LABEL
    .fg
    .col 6
    $VALUE
    .enddef

    .bar HARBOUR NOTICE · 02 SEP
    .field WIND NW 040° 18 kt
.end

The rules, and they are the whole of it:

.item A definition is ".def NAME PARAM..." through ".enddef"; the
lines between are its body. (Not ".end": a pica document quotes
raster pages in .pre blocks, which ".end" would close.) Names are letters, digits and the underscore,
and an alias may not take a command's name.
.item A use is ".NAME" followed by its arguments: one word per
parameter, the last parameter taking the rest of the line as
written, so a title needs no quotes. Too few words is an error.
.item In the body, "$PARAM" is replaced by the argument's text,
as text. Nothing is computed; any other "$" is literal.
.item A body may use aliases defined before it; they are inlined
when the definition closes, so a use expands one level and
recursion cannot arise. A definition inside a definition is an
error, as is a use that a definition has not preceded.
.item An error in an expanded line names the alias and the line
of its body, after the line of the use.
.item There is no conditional, no loop, no default value, no
arithmetic, and none will be admitted: a page that needs them is
written by a program, which has all of those.

# Non-goals

.item No geometry of its own: a raster format states its P, R
and C; this specification states none.
.item No navigation, no actions: a link names a target and the
app does the rest; the page is content only.
.item No mark, no version byte, no reserved fields. Nothing in
the bytes says what format they are: that is declared wherever
the page itself is. A revision that appends to the cell table
needs no announcement, since an older renderer shows the new
cells as blanks.
.item No text styles: no underline, no bold, no double height, no
flashing. Emphasis is ink; structure is a rule.
.item No mosaics yet, no general Unicode: see the parked designs.
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
.item A canvas file. The canvas is a type in the package with no
bytes of its own; the page's bytes are its only serialization.
ADMISSION TEST: a page richer than the in-band bytes can hold.

.width 72
.cols 1
.font sans
