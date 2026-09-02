package tessera

import "fmt"

// Geometry (TESSERA.t, "The page").
const (
	Cols     = 34                // columns per row
	Rows     = 28                // rows per panel
	Panels   = 4                 // panels per page, read in order
	PanelLen = Cols * Rows       // 952 bytes: one panel, row-major
	PageLen  = Panels * PanelLen // 3,808 bytes: the page, panels back to back
	TileLen  = 238               // one carousel slot: seven whole rows
	TileRows = TileLen / Cols    // 7
	Tiles    = PageLen / TileLen // 16
)

// Ink codes (TESSERA.t, "Ink"): 0x80+n sets the foreground to palette
// entry n, 0x88+n the background, each for the rest of its row. A code
// renders as a blank in the state it establishes.
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

// A Page is the raster: byte i is panel i/952, row (i%952)/34,
// column i%34. The zero Page is blank.
type Page [PageLen]byte

// Offset returns the page offset of a cell.
func Offset(panel, row, col int) int {
	return panel*PanelLen + row*Cols + col
}

// Tile returns tile k (0..15), the value carousel slot k carries:
// bytes 238k through 238k+237 of the page. The slice aliases p.
func (p *Page) Tile(k int) []byte {
	if k < 0 || k >= Tiles {
		panic(fmt.Sprintf("tessera: tile %d out of range 0..%d", k, Tiles-1))
	}
	return p[k*TileLen : (k+1)*TileLen]
}

// Row returns the 34 cells of one row. The slice aliases p.
func (p *Page) Row(panel, row int) []byte {
	o := Offset(panel, row, 0)
	return p[o : o+Cols]
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

// Text renders one panel as 28 rows of 34 runes, plain: ink codes and
// blanks render as spaces. Rows are trimmed on the right.
func (p *Page) Text(panel int) []string {
	out := make([]string, Rows)
	for r := range Rows {
		runes := make([]rune, Cols)
		for c, b := range p.Row(panel, r) {
			runes[c] = CellRune(b)
		}
		end := Cols
		for end > 0 && runes[end-1] == ' ' {
			end--
		}
		out[r] = string(runes[:end])
	}
	return out
}
