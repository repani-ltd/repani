RASTER -- ROWS OF COLORED CELLS
.date 2026-09-14
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem Format specification. Sections through "Authoring" are normative.

A raster is rows of colored text cells, forty columns wide, a
glyph and an ink per cell. The width, the cell repertoire, the
ink model, the row record and the authoring language are all
fixed here, so every raster tool reads every raster and every
renderer shows the same cells: the same glyph in the same row
and column in the same ink, whatever its font, theme or screen.
The row is the unit of everything: of storage, of update, of
transmission. A file is rows; an update is rows; a radio slot
carries rows. Nothing above the row is defined here -- not a
page, not a screen, not a height -- because nothing above the
row needs to be shared for a raster to be read.

# Rows

A raster is ROWS numbered 0 to 1023, each of 40 CELLS numbered
0 to 39. Every cell is content; there are no special rows, no
headers, no trailers. An unwritten cell is blank in default ink.
A raster has no height of its own: it has the rows written in
it, and a format that shows a screen states how many rows a
screen is.

A row's RECORD is its bytes: a two-byte header, then the row's
cells to its written length. Little-endian, as everything is:

.pre
    bytes 0-1   header: bits 0-5 the LENGTH N (0..40),
                bits 6-15 the ROW (0..1023)
    bytes 2..N+1      N glyph bytes (see Cells)
    bytes N+2..2N+1   N ink bytes (see Ink)
.end

so a record is 2+2N bytes, at most 82. A record is the whole
row: cells N to 39 are blank in default ink, whatever the row
held before. It is never a partial write. The WRITTEN LENGTH of
a row is one past its last cell that is not blank in default
ink, so a row with content only at its right pays full length,
and a blank row is 0.

The BYTES of a raster are its rows as records, ascending, each
row once, each at its written length, blank rows omitted. So
identical content is identical bytes, a blank raster is no
bytes, and a file cut short is refused, not rendered shorter.

A STREAM is records in any order: a row repeated replaces its
earlier value, and a record of length 0 clears its row. Folding
a stream onto a raster is the whole of update: a producer sends
the rows it owns, whole, and a reader replaces them. There is
no delta below the row and none is needed.

The renderer chooses the cell's shape. The format states no
glyph aspect, font, or pixel.

# Cells

All 256 glyph values are defined. Values not assigned below render
as a blank; the table grows by appending, never by reassigning.

.pre
    0x00        blank (a space; the glyph of every unwritten cell)
    0x01..0x02  rules        ─ │
    0x03..0x06  arrows       ← ↑ → ↓
    0x07..0x0A  blocks       ░ ▒ ▓ █
    0x0B..0x10  symbols      ° ± × ÷ • ·
    0x11..0x1F  unassigned: render blank
    0x20..0x7E  ASCII
    0x7F        €
    0x80..0x8F  unassigned: render blank
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

The INK BYTE of a cell holds both indices: the background in
its high nibble, the foreground in its low, so 0x00 is default
on default, 0x41 is red on blue, and bit 3 of each nibble is
zero. A reader rejects an ink byte with either of those bits set;
they are the one place the format could grow a wider palette,
and until it does they are zero. Ink is per cell and nothing
carries from cell to cell or row to row: every cell renders
alone, colored text may stand in any column, glued to text in
another ink, and a row may be full in any ink.

# Authoring

Pages are authored in a line-oriented dot-command language: a
line is one command or one run of content, and the command set
is closed. A page that says everything the language has:

.pre
    .rem A notice: a title bar, a heading, a paragraph, a table.
    .bg blue
    .fill 0
    .fg white
    .at 0
    HARBOUR NOTICE · 02 SEP
    .fg yellow
    .bg
    .at 2
    MELTEMI TONIGHT
    .fg
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
    .at R [C]       the next run lands at row R, column C (default
                    0); one-shot; the source starts at row 0
    .fg [NAME]      the pen's foreground; persists until changed;
                    bare, the default
    .bg [NAME]      the pen's background, likewise
    content         one run at the cursor in the pen's ink; the
                    cursor then moves to the next row, at column 0
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
Rows and columns count from 0.
.item A line that begins with a dot and a lowercase letter is a
command or the use of an alias, and one that is neither is an
error. A line
that begins with "+ " is a continuation; a lone "+" and "+5" are
content. "+" and .col attach to the last run, and there is none
after .at.
.item Content is right-trimmed. Leading spaces position the run
and paint nothing, so a run's text lands at the cursor plus its
leading spaces; interior spaces are painted. An empty line, or
one of only spaces, moves the cursor one row and paints nothing.
.item A run that overflows its row, a cursor below row 1023, and
a rune outside the repertoire are errors. A format that shows a
screen checks the rows itself: the language does not know how
tall a screen is.
.item The state that crosses lines is the pen and the row cursor,
and nothing else. The pen is the author's: nothing resets it.
Position is never carried: a line lands where its own leading
spaces, or the .at or .col just before it, say, else at column
0.
.item Painting is by cell, in source order, later over earlier;
a fill clears what it covers. A cell's ink is the pen's when it
was last painted, so the order of the source never changes a
color elsewhere, and compilation is reproducible: the same
source yields the same bytes.
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
    .at 0
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
raster pages in .pre blocks, which ".end" would close.) Names
are letters, digits and the underscore, and an alias may not
take a command's name.
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

.item No page, no panel, no screen, no height. A raster is rows;
what a screen shows of them, and how many, is the format's or
the viewer's that shows it. A second width is a second format.
.item No navigation, no actions: a link names a target and the
app does the rest; the raster is content only.
.item No mark, no version byte, no reserved fields. Nothing in
the bytes says what format they are: that is declared wherever
the raster itself is. A revision that appends to the cell table
needs no announcement, since an older renderer shows the new
cells as blanks.
.item No compression. A record is its cells, two bytes each,
whatever the medium; a transport that must be smaller packs
whole records and carries short rows short, and that is all the
compactness there is.
.item No partial rows. A record replaces its row whole, so a
producer never needs to know what a row held before, and no
residue can be left on it.
.item No text styles: no underline, no bold, no double height, no
flashing. Emphasis is ink; structure is a rule.
.item No mosaics yet, no general Unicode: see the parked designs.
.item No glyph metrics: the renderer owns the cell's shape.

# Parked designs, with their admission tests

.item Mosaics. The 2×2 quadrant set (16 patterns) fits the
unassigned range and would be the first append; the 2×3
sextants do not fit. ADMISSION TEST: the first page that wants a
chart or a logo.
.item A second repertoire. The table is fixed, which is what lets
every raster tool read every raster; a script beyond it needs a
new format, not a parameter. ADMISSION TEST: the first raster
that needs one.
.item A wider palette. Bit 3 of each ink nibble is zero; set, it
would double the palette to sixteen entries without changing the
cell. ADMISSION TEST: the first page that needs a ninth color.

.width 72
.cols 1
.font sans
