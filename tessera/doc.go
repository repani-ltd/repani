/*
Package tessera is a page format: 3,808 cells of colored text, one
byte per cell, in sixteen tiles of 238 bytes. The tile is the unit of
update and each tile is complete on its own; the tile is sized to a
quietcasting slot, and a station's sixteen slots carry the sixteen
tiles in order. This comment is the operating reference; TESSERA.t is
the specification.

# The page

A page is four panels of 28 rows by 34 columns, read in order 0..3,
as one contiguous raster: byte i is panel i/952, row (i%952)/34,
column i%34. Tile k is bytes 238k through 238k+237, always seven
whole rows of one panel; a tile is identified by its position and by
nothing in its bytes, so tiles arrive in any order and any subset.
Content flows freely down a panel and never from one panel into
another, because a renderer may arrange the four panels as it likes.
The renderer also owns the cell's shape: the format states no glyph
aspect, font or pixel.

# Cells

All 256 values are defined; unassigned values render blank and the
table grows by appending:

	0x00        blank (the value of every unwritten cell)
	0x01..0x0B  box drawing  ─ │ ┌ ┐ └ ┘ ├ ┤ ┬ ┴ ┼
	0x0C..0x11  double box   ═ ║ ╔ ╗ ╚ ╝
	0x12..0x15  arrows       ← ↑ → ↓
	0x16..0x18  shades       ░ ▒ ▓
	0x19..0x1F  symbols      ° ± × ÷ • · §
	0x20..0x7E  ASCII
	0x7F        €
	0x80..0x87  ink: foreground palette 0..7
	0x88..0x8F  ink: background palette 0..7
	0x90..0xBF  unassigned
	0xC0..0xD8  Greek lowercase  α β γ δ ε ζ η θ ι κ λ μ ν ξ ο π
	            ρ ς σ τ υ φ χ ψ ω
	0xD9..0xE3  accented        ά έ ή ί ό ύ ώ ϊ ϋ ΐ ΰ
	0xE4..0xFB  Greek uppercase Α Β Γ Δ Ε Ζ Η Θ Ι Κ Λ Μ Ν Ξ Ο Π
	            Ρ Σ Τ Υ Φ Χ Ψ Ω   (no tonos on capitals)
	0xFC..0xFF  « » … ―

# Ink

Color travels in band. An ink code occupies a cell, renders as a
blank in the state it establishes, and sets one attribute for the
rest of its row: 0x80+n the foreground to palette entry n, 0x88+n
the background. Every row begins in foreground 0 on background 0, so
every tile renders alone. The palette is the renderer's default and
teletext's seven hues:

	0 default  1 red  2 green  3 yellow  4 blue  5 magenta  6 cyan  7 white

# Authoring

Source is line-oriented dot-command text: content lines are content,
the command set is closed, and a line that lexes as a command (a dot,
then a lowercase letter) but is not one of the five is an error. ". "
and ".." begin ordinary content.

	.panel N         target panel 0..3 (required first); cursor to
	                 row 0 column 0, pen to default ink
	.at R C          cursor to row R (0..27), column C (0..33);
	                 invalidates the "+" pen
	.ink FG          set the pen's foreground, and optionally its
	.ink FG on BG    background, by palette name
	content          one run at the cursor in the pen's ink; the
	                 cursor then drops one row, same column. Lines
	                 are right-trimmed; an empty line, or one of
	                 only spaces, flows one row and writes nothing
	+ content        continue on the same row where the last run
	                 ended (only the leading "+ " is the marker)
	.fill R C W H    a W-by-H region of spaces at R C in the pen's
	                 ink: bars, panels, grounds. A fill overwrites
	                 everything under it, codes included
	.rem TEXT        comment, dropped

The compiler owns the code cells. Before a run it emits, at the
cursor, one code for each attribute in which the pen differs from
the row's state arriving there, then the run: a foreground change
costs one cell, foreground and background two. A code sets its
attribute to the end of the row, so author a row left to right. A
run's content never lands on a code cell (an error); a code may
replace a code. A fill emits its codes at its left edge on every row,
and at its right edge, when that edge is inside the row, the codes
that restore what arrived there before; a closing code that would
land on content is an error. Text placed over a fill inherits the
fill's background from the codes to its left. Content is UTF-8,
transcoded to the repertoire above; a rune outside it is an error.
Compilation is reproducible: the same source yields the same 3,808
bytes.

Frozen vector: ".panel 2", ".at 3 5", ".ink yellow", "QUIETCASTING"
puts 83 51 55 49 45 54 43 41 53 54 49 4E 47 at bytes 107..119 of tile
8 and nothing else.
*/
package tessera
