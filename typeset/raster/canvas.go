package raster

import "fmt"

// A Cell is one cell of a canvas: its glyph as a cell byte (0x00
// empty, 0x20 a written space) and its ink. A blank cell (empty or
// space) shows only its background; its foreground is immaterial.
type Cell struct {
	Glyph byte
	Ink
}

// blank reports whether the cell shows no glyph.
func (c Cell) blank() bool { return c.Glyph == 0 || c.Glyph == ' ' }

// A Canvas is a page with its ink out of band: every cell carries its
// own glyph and ink, and nothing is in band. The language compiles to
// a canvas; Encode turns it into the page's bytes and Decode reads
// them back. Cells is Geometry.Len() long in the page's order.
type Canvas struct {
	Geometry
	Cells []Cell
}

// NewCanvas returns an empty canvas of the geometry.
func NewCanvas(g Geometry) *Canvas {
	g.check()
	return &Canvas{Geometry: g, Cells: make([]Cell, g.Len())}
}

// Row returns the cells of one row. The slice aliases the canvas.
func (c *Canvas) Row(panel, row int) []Cell {
	o := c.Offset(panel, row, 0)
	return c.Cells[o : o+c.Cols]
}

// Decode reads a page into a canvas: each row from its opening ink
// (its tail codes) through its codes, code cells becoming empty cells
// in the ink they establish.
func Decode(p *Page) *Canvas {
	c := NewCanvas(p.Geometry)
	for panel := range p.Panels {
		for row := range p.Rows {
			decodeRow(p.Row(panel, row), c.Row(panel, row))
		}
	}
	return c
}

// tailStart returns the index where a row's tail begins: the codes at
// its very end, which set its opening ink. Cols if there is no tail.
func tailStart(row []byte) int {
	t := len(row)
	for t > 0 && IsInk(row[t-1]) {
		t--
	}
	return t
}

func decodeRow(row []byte, out []Cell) {
	var s Ink
	t := tailStart(row)
	for _, b := range row[t:] {
		s = s.apply(b)
	}
	for x, b := range row {
		switch {
		case x >= t:
			out[x] = Cell{Ink: s}
		case IsInk(b):
			s = s.apply(b)
			out[x] = Cell{Ink: s}
		default:
			out[x] = Cell{Glyph: b, Ink: s}
		}
	}
}

// Encode turns the canvas into the page's bytes (RASTER.t, "Ink"):
// on every row a change of background at a blank cell takes that
// cell; the changes a glyph needs take the blank cells just before
// it; and the ink of a glyph in the first cell goes to the row's
// tail. A row with no blank cell where a code must go is an error
// naming the panel, row and column.
func (c *Canvas) Encode() (*Page, error) {
	p := New(c.Geometry)
	for panel := range c.Panels {
		for row := range c.Rows {
			if err := encodeRow(c.Row(panel, row), p.Row(panel, row)); err != nil {
				return nil, fmt.Errorf("panel %d row %d: %w", panel, row, err)
			}
		}
	}
	return p, nil
}

func encodeRow(cells []Cell, out []byte) error {
	n := len(cells)
	used := make([]bool, n) // cells holding a code
	for x := range out {
		out[x] = 0
	}
	var s Ink // the state arriving at cell x
	for x, cell := range cells {
		if cell.blank() {
			if cell.BG != s.BG {
				// A background change at a blank cell takes the cell.
				if x == n-1 {
					// The last cell is the tail; a code there would read as
					// opening ink. The cell keeps the arriving background.
					continue
				}
				out[x], used[x] = InkBG+cell.BG, true
				s.BG = cell.BG
			}
			if !used[x] {
				out[x] = cell.Glyph
			}
			continue
		}
		need := codes(s, cell.Ink)
		k := len(need)
		switch {
		case k == 0:
		case x == 0:
			// Opening ink: the tail, which must be free.
			for i, b := range need {
				y := n - k + i
				if y <= 0 || !cells[y].blank() || used[y] {
					return fmt.Errorf("column 0 starts in ink and the row is full: shorten it by %d", k)
				}
				out[y], used[y] = b, true
			}
		default:
			for i, b := range need {
				y := x - k + i
				if y < 0 || !cells[y].blank() || used[y] {
					return fmt.Errorf("column %d needs %d blank cells before it for its ink", x, k)
				}
				out[y], used[y] = b, true
			}
		}
		s = cell.Ink
		out[x] = cell.Glyph
	}
	return nil
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
func (c *Canvas) Links(panel, row int) []Link {
	var out []Link
	cells := c.Row(panel, row)
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
