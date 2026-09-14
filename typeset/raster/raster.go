package raster

import (
	"fmt"
	"slices"
)

// Cols is the width of every row (RASTER.t, "Rows"): a raster is 40
// columns wide, and nothing else about its shape is stated.
const Cols = 40

// MaxRows is the rows a raster can address: the record header holds
// the row in ten bits.
const MaxRows = 1024

// ColorNames is the palette, by index.
var ColorNames = [8]string{
	"default", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
}

// Ink is a cell's attributes: palette indices, 0 the renderer's default.
type Ink struct{ FG, BG byte }

// A Cell is one cell of a row: its glyph as a cell byte (0x00 empty,
// 0x20 a written space) and its ink. A blank cell (empty or space)
// shows only its background; its foreground is immaterial.
type Cell struct {
	Glyph byte
	Ink
}

// blank reports whether the cell shows no glyph.
func (c Cell) blank() bool { return c.Glyph == 0 || c.Glyph == ' ' }

// A Row is Cols cells.
type Row [Cols]Cell

// end returns the row's written length: one past its last cell that
// is not blank in default ink, so 0 for a blank row.
func (r *Row) end() int {
	n := Cols
	for n > 0 && r[n-1].blank() && r[n-1].Ink == (Ink{}) {
		n--
	}
	return n
}

// A Raster is rows of cells: Rows[i] is row i, and rows past the
// slice are blank. The zero Raster is blank. The bytes of a raster
// are Bytes; Read reads them, or any stream of row records, back.
type Raster struct {
	Rows []Row
}

// New returns a blank raster.
func New() *Raster { return &Raster{} }

// Row returns row i, growing the raster to hold it. The pointer
// aliases the raster until it next grows.
func (r *Raster) Row(i int) *Row {
	if i < 0 || i >= MaxRows {
		panic(fmt.Sprintf("raster: row %d outside 0..%d", i, MaxRows-1))
	}
	if i >= len(r.Rows) {
		// Rows only ever grow, so the capacity past len is fresh, zeroed
		// memory: extend into it without a second allocation.
		r.Rows = slices.Grow(r.Rows, i+1-len(r.Rows))[:i+1]
	}
	return &r.Rows[i]
}

// Height is the rows the raster holds: one past the highest row
// written, which may be blank.
func (r *Raster) Height() int { return len(r.Rows) }

// Bytes is the raster's canonical bytes (RASTER.t, "Rows"): its
// non-blank rows in order, each a record of a two-byte header, the
// row in the high ten bits and the length N in the low six,
// little-endian, then N glyph bytes, then N ink bytes, background in
// the high nibble and foreground in the low. N is the row's written
// length. The same raster yields the same bytes.
func (r *Raster) Bytes() []byte {
	var out []byte
	for i := range r.Rows {
		row := &r.Rows[i]
		n := row.end()
		if n == 0 {
			continue
		}
		out = appendRecord(out, i, row, n)
	}
	return out
}

func appendRecord(out []byte, i int, row *Row, n int) []byte {
	h := uint16(i)<<6 | uint16(n)
	out = append(out, byte(h), byte(h>>8))
	for _, c := range row[:n] {
		out = append(out, c.Glyph)
	}
	for _, c := range row[:n] {
		out = append(out, c.BG<<4|c.FG)
	}
	return out
}

// Read reads a raster from a stream of row records: in any order,
// a row repeated replacing its earlier value, a record of length 0
// clearing its row. Cells past a record's length are blank in
// default ink. A record cut short, a length past Cols, or an ink
// byte with a reserved bit set is an error.
func Read(b []byte) (*Raster, error) {
	r := New()
	for at := 0; at < len(b); {
		if len(b)-at < 2 {
			return nil, fmt.Errorf("raster: byte %d: record header cut short", at)
		}
		h := uint16(b[at]) | uint16(b[at+1])<<8
		i, n := int(h>>6), int(h&0x3F)
		at += 2
		if n > Cols {
			return nil, fmt.Errorf("raster: byte %d: row %d has length %d, past %d", at-2, i, n, Cols)
		}
		if len(b)-at < 2*n {
			return nil, fmt.Errorf("raster: byte %d: row %d cut short", at-2, i)
		}
		row := r.Row(i)
		*row = Row{}
		for k := range n {
			ink := b[at+n+k]
			if ink&0x88 != 0 {
				return nil, fmt.Errorf("raster: byte %d: ink byte %#02x is not two palette indices", at+n+k, ink)
			}
			row[k] = Cell{Glyph: b[at+k], Ink: Ink{FG: ink & 0x07, BG: ink >> 4}}
		}
		at += 2 * n
	}
	return r, nil
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
func (r *Raster) Links(row int) []Link {
	if row >= len(r.Rows) {
		return nil
	}
	var out []Link
	cells := r.Rows[row][:]
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
