/*
Package raster is a page of colored text cells, one byte per cell:
panels of rows by columns, in a geometry the caller chooses, with a
fixed cell repertoire, in-band row-scoped ink, a line-oriented
authoring language that compiles to the bytes, and renderers to
plain text, ANSI and HTML. It is the engine of tessera
(repani.com/tessera), which is a raster of 34 by 28 by 4 sized to
quietcasting's slots; any page format with the same cell model is
another geometry. This comment is the operating reference;
RASTER.t is the specification.

# The page

A page is Geometry.Panels panels of Geometry.Rows rows by
Geometry.Cols columns, read in order, as one contiguous raster of
Geometry.Len() bytes: byte i is panel i / PanelLen, row
(i % PanelLen) / Cols, column i % Cols. Every cell is content; there
are no special rows, no headers, no trailers. Content flows freely
down a panel and never from one panel into another, because a
renderer may arrange the panels as it likes. The renderer also
owns the cell's shape: the format states no glyph aspect, font or
pixel.

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
	0x90..0x97  weather and marine  ☀ ☁ ☂ ☾ ❄ ↯ ⚓ ⚠
	0x98..0xBF  unassigned
	0xC0..0xD8  Greek lowercase  α β γ δ ε ζ η θ ι κ λ μ ν ξ ο π
	            ρ ς σ τ υ φ χ ψ ω
	0xD9..0xE3  accented        ά έ ή ί ό ύ ώ ϊ ϋ ΐ ΰ
	0xE4..0xFB  Greek uppercase Α Β Γ Δ Ε Ζ Η Θ Ι Κ Λ Μ Ν Ξ Ο Π
	            Ρ Σ Τ Υ Φ Χ Ψ Ω   (no tonos on capitals)
	0xFC..0xFF  « » … ―

Content is authored in UTF-8 and transcoded; the repertoire is the
contract, and a rune outside it is an error, never a substitution.

# Ink

Color travels in band. An ink code occupies a cell, renders as a
blank in the state it establishes, and sets one attribute for the
rest of its row: 0x80+n the foreground to palette entry n, 0x88+n
the background. Every row begins in foreground 0 on background 0,
so every row renders alone. The palette is the renderer's default
and teletext's seven hues:

	0 default  1 red  2 green  3 yellow  4 blue  5 magenta  6 cyan  7 white

# Authoring

Source is line-oriented dot-command text: content lines are content,
the command set is closed, and a line that lexes as a command (a dot,
then a lowercase letter) but is not one of the five is an error. ". "
and ".." begin ordinary content.

	.panel N         target panel (required first); cursor to row 0
	                 column 0, pen to default ink
	.at R C          cursor to row R, column C; invalidates the
	                 "+" pen
	.ink FG          set the pen, by palette name: FG on the
	.ink FG on BG    default background, or FG on BG
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
the row's state arriving there (background first), then the run: a
foreground change costs one cell, foreground and background two. A
code sets its attribute to the end of the row, so author a row left
to right. A run's content never lands on a code cell (an error); a
code may replace a code. A fill emits its codes at its left edge on
every row, and at its right edge, when that edge is inside the row,
the codes that restore what arrived there before; a closing code
that would land on content is an error. Text placed over a fill
inherits the fill's background from the codes to its left.
Compilation is reproducible: the same source and geometry yield the
same bytes.
*/
package raster
