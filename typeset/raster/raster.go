package raster

import "fmt"

// Geometry is a page's shape: panels of Rows by Cols cells, read in
// order. Every dimension is the caller's; the package fixes none.
type Geometry struct {
	Cols, Rows, Panels int
}

// PanelLen is the cells of one panel, row-major.
func (g Geometry) PanelLen() int { return g.Cols * g.Rows }

// Len is the cells of the page: the panels back to back.
func (g Geometry) Len() int { return g.Panels * g.PanelLen() }

// Size is the bytes of the page: two per cell (RASTER.t, "The page").
func (g Geometry) Size() int { return 2 * g.Len() }

// Offset returns the cell index of a cell.
func (g Geometry) Offset(panel, row, col int) int {
	return panel*g.PanelLen() + row*g.Cols + col
}

// check rejects a geometry no page can have.
func (g Geometry) check() {
	if g.Cols < 1 || g.Rows < 1 || g.Panels < 1 {
		panic(fmt.Sprintf("raster: geometry %+v", g))
	}
}

// ColorNames is the palette, by index.
var ColorNames = [8]string{
	"default", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
}

// Ink is a cell's attributes: palette indices, 0 the renderer's default.
type Ink struct{ FG, BG byte }

// A Cell is one cell of a page: its glyph as a cell byte (0x00 empty,
// 0x20 a written space) and its ink. A blank cell (empty or space)
// shows only its background; its foreground is immaterial.
type Cell struct {
	Glyph byte
	Ink
}

// blank reports whether the cell shows no glyph.
func (c Cell) blank() bool { return c.Glyph == 0 || c.Glyph == ' ' }

// A Page is the raster: Cells is Geometry.Len() cells, cell i being
// panel i / PanelLen, row (i % PanelLen) / Cols, column i % Cols.
// Unwritten cells are the zero Cell. The bytes of a page are Bytes;
// Of reads them back.
type Page struct {
	Geometry
	Cells []Cell
}

// New returns a blank page of the geometry.
func New(g Geometry) *Page {
	g.check()
	return &Page{Geometry: g, Cells: make([]Cell, g.Len())}
}

// Reset blanks the page for reuse.
func (p *Page) Reset() { clear(p.Cells) }

// Row returns the cells of one row. The slice aliases the page.
func (p *Page) Row(panel, row int) []Cell {
	o := p.Offset(panel, row, 0)
	return p.Cells[o : o+p.Cols]
}

// Bytes is the page's bytes (RASTER.t, "The page"): two per cell in
// page order, the glyph then the ink, the ink's background in the
// high nibble and its foreground in the low. The same page yields
// the same bytes.
func (p *Page) Bytes() []byte {
	out := make([]byte, 0, p.Size())
	for _, c := range p.Cells {
		out = append(out, c.Glyph, c.BG<<4|c.FG)
	}
	return out
}

// Of reads a page of the geometry from its bytes: exactly Size bytes,
// every ink byte a palette index in each nibble.
func Of(g Geometry, b []byte) (*Page, error) {
	g.check()
	if len(b) != g.Size() {
		return nil, fmt.Errorf("raster: %d bytes for geometry %+v (want %d)", len(b), g, g.Size())
	}
	p := New(g)
	for i := range p.Cells {
		ink := b[2*i+1]
		if ink&0x88 != 0 {
			return nil, fmt.Errorf("raster: cell %d: ink byte %#02x is not two palette indices", i, ink)
		}
		p.Cells[i] = Cell{Glyph: b[2*i], Ink: Ink{FG: ink & 0x07, BG: ink >> 4}}
	}
	return p, nil
}

// A Link is a tappable span of a row (RASTER.t, "Authoring"): the
// cells from an opening bracket to the next closing bracket on the
// row, brackets included, with the text between them as its target.
// Links are derived from the cells, never stored.
type Link struct {
	Col, Len int
	Target   string
}

// Links returns the links of one row, in order.
func (p *Page) Links(panel, row int) []Link {
	var out []Link
	cells := p.Row(panel, row)
	for x := 0; x < len(cells); x++ {
		if cells[x].Glyph != '[' {
			continue
		}
		end := -1
		for y := x + 1; y < len(cells); y++ {
			if cells[y].Glyph == ']' {
				end = y
				break
			}
		}
		if end < 0 {
			break // no closing bracket on the row: no more links
		}
		if end > x+1 {
			target := make([]rune, 0, end-x-1)
			for _, cell := range cells[x+1 : end] {
				target = append(target, CellRune(cell.Glyph))
			}
			out = append(out, Link{Col: x, Len: end - x + 1, Target: string(target)})
		}
		x = end
	}
	return out
}
