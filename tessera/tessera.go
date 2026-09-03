package tessera

import (
	"fmt"

	"repani.com/typeset/raster"
)

// Geometry (TESSERA.t, "The page" and "The tile").
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

// A Page is the 3,808 bytes. The zero Page is blank. Everything about
// its cells -- rows, rendering -- is the raster's, through Raster.
type Page [PageLen]byte

// Tile returns tile k (0..15), the value carousel slot k carries:
// bytes 238k through 238k+237 of the page. The slice aliases p.
func (p *Page) Tile(k int) []byte {
	if k < 0 || k >= Tiles {
		panic(fmt.Sprintf("tessera: tile %d out of range 0..%d", k, Tiles-1))
	}
	return p[k*TileLen : (k+1)*TileLen]
}

// Raster views the page as a raster page of tessera's geometry. The
// view aliases p.
func (p *Page) Raster() *raster.Page { return raster.Of(Geometry, p[:]) }

// Compile is raster.Compile on tessera's geometry, returning the
// page as its bytes.
func Compile(src string) (*Page, error) {
	rp, err := raster.Compile(Geometry, src)
	if err != nil {
		return nil, err
	}
	p := new(Page)
	copy(p[:], rp.Cells)
	return p, nil
}
