package tessera

import (
	"fmt"

	"repani.com/typeset/raster"
)

// Geometry (TESSERA.t, "The page"): tessera is a raster of 34 by 28
// by 4, and everything about cells, ink and authoring is raster's
// (repani.com/typeset/raster). What tessera adds is the tile.
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

// Geometry is the page's shape as raster sees it.
var Geometry = raster.Geometry{Cols: Cols, Rows: Rows, Panels: Panels}

// Ink codes and the palette are raster's; the names are kept here for
// callers that read a tessera page byte by byte.
const (
	InkFG = raster.InkFG
	InkBG = raster.InkBG
)

// ColorNames is the palette: the renderer's default and teletext's
// seven hues.
var ColorNames = raster.ColorNames

// IsInk reports whether b is an ink code.
func IsInk(b byte) bool { return raster.IsInk(b) }

// CellRune returns the display rune of a cell byte.
func CellRune(b byte) rune { return raster.CellRune(b) }

// Transcode maps content text (UTF-8) to cell bytes.
func Transcode(text string) ([]byte, error) { return raster.Transcode(text) }

// A Page is the raster: byte i is panel i/952, row (i%952)/34,
// column i%34. The zero Page is blank.
type Page [PageLen]byte

// Offset returns the page offset of a cell.
func Offset(panel, row, col int) int {
	return Geometry.Offset(panel, row, col)
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

// Raster views the page as a raster page of tessera's geometry; the
// view aliases p, so it is the page for every raster operation.
func (p *Page) Raster() *raster.Page { return raster.Of(Geometry, p[:]) }

// Compile turns source into a page: raster's compiler on tessera's
// geometry. Errors carry the 1-based source line, and compilation is
// reproducible: the same source yields the same 3,808 bytes.
func Compile(src string) (*Page, error) {
	rp, err := raster.Compile(Geometry, src)
	if err != nil {
		return nil, err
	}
	p := new(Page)
	copy(p[:], rp.Cells)
	return p, nil
}

// Text renders one panel as 28 rows of 34 runes, plain.
func (p *Page) Text(panel int) []string { return p.Raster().Text(panel) }

// ANSI renders one panel as 28 rows of exactly 34 cells with ANSI
// colors.
func (p *Page) ANSI(panel int) []string { return p.Raster().ANSI(panel) }

// HTMLRows renders one panel as 28 lines of HTML for a <pre>.
func (p *Page) HTMLRows(panel int) []string { return p.Raster().HTMLRows(panel) }

// Layout arranges the four panels' rendered rows in reading order,
// across panels per row of panels.
func Layout(panels [][]string, across int) []string {
	return raster.Layout(panels, Cols, across)
}

// HTMLDocument renders the page as one self-contained HTML document,
// the four panels across to a row.
func HTMLDocument(p *Page, across int, title string) string {
	return raster.HTMLDocument(p.Raster(), across, title)
}
