package raster

import "fmt"

// Geometry is a page's shape: panels of Rows by Cols cells, read in
// order. Every dimension is the caller's; the package fixes none.
type Geometry struct {
	Cols, Rows, Panels int
}

// PanelLen is the bytes of one panel, row-major.
func (g Geometry) PanelLen() int { return g.Cols * g.Rows }

// Len is the bytes of the page: the panels back to back.
func (g Geometry) Len() int { return g.Panels * g.PanelLen() }

// Offset returns the page offset of a cell.
func (g Geometry) Offset(panel, row, col int) int {
	return panel*g.PanelLen() + row*g.Cols + col
}

// check rejects a geometry no page can have.
func (g Geometry) check() {
	if g.Cols < 1 || g.Rows < 1 || g.Panels < 1 {
		panic(fmt.Sprintf("raster: geometry %+v", g))
	}
}

// Ink codes: 0x80+n sets the foreground to palette entry n, 0x88+n
// the background, each for the rest of its row. A code renders as a
// blank in the state it establishes.
const (
	InkFG   = 0x80
	InkBG   = 0x88
	inkLast = 0x8F
)

// ColorNames is the palette: the renderer's default and teletext's
// seven hues.
var ColorNames = [8]string{
	"default", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
}

// A Page is the raster: Cells is Geometry.Len() bytes, byte i being
// panel i / PanelLen, row (i % PanelLen) / Cols, column i % Cols.
// Unwritten cells are 0x00.
type Page struct {
	Geometry
	Cells []byte
}

// New returns a blank page of the geometry.
func New(g Geometry) *Page {
	g.check()
	return &Page{Geometry: g, Cells: make([]byte, g.Len())}
}

// Of views existing bytes as a page of the geometry; the page
// aliases cells, which must be exactly Geometry.Len() long.
func Of(g Geometry, cells []byte) *Page {
	g.check()
	if len(cells) != g.Len() {
		panic(fmt.Sprintf("raster: %d cells for geometry %+v (want %d)", len(cells), g, g.Len()))
	}
	return &Page{Geometry: g, Cells: cells}
}

// Row returns the cells of one row. The slice aliases the page.
func (p *Page) Row(panel, row int) []byte {
	o := p.Offset(panel, row, 0)
	return p.Cells[o : o+p.Cols]
}

// IsInk reports whether b is an ink code.
func IsInk(b byte) bool { return b >= InkFG && b <= inkLast }

// ink is a row's attribute state: palette indices.
type ink struct{ fg, bg byte }

// stateAt returns the ink arriving at column col of a row: the effect
// of the codes in columns 0..col-1, from the row's clean start.
func stateAt(row []byte, col int) ink {
	var s ink
	for _, b := range row[:col] {
		switch {
		case b >= InkFG && b < InkBG:
			s.fg = b - InkFG
		case b >= InkBG && b <= inkLast:
			s.bg = b - InkBG
		}
	}
	return s
}

// codes returns the ink codes that take the state from have to want:
// none, one, or two bytes, background first, so that a bar whose text
// is also recolored starts whole at its first cell.
func codes(have, want ink) []byte {
	var out []byte
	if have.bg != want.bg {
		out = append(out, InkBG+want.bg)
	}
	if have.fg != want.fg {
		out = append(out, InkFG+want.fg)
	}
	return out
}

// Text renders one panel as Rows rows of Cols runes, plain: ink codes
// and blanks render as spaces. Rows are trimmed on the right.
func (p *Page) Text(panel int) []string {
	out := make([]string, p.Rows)
	for r := range p.Rows {
		runes := make([]rune, p.Cols)
		for c, b := range p.Row(panel, r) {
			runes[c] = CellRune(b)
		}
		end := p.Cols
		for end > 0 && runes[end-1] == ' ' {
			end--
		}
		out[r] = string(runes[:end])
	}
	return out
}
