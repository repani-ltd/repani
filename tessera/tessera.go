package tessera

import (
	"repani.com/typeset/raster"
)

// Geometry (TESSERA.t, "The page").
const (
	Cols     = 34                    // columns per row
	Rows     = 28                    // rows per panel
	Panels   = 4                     // panels per page, read in order
	PanelLen = Cols * Rows           // 952 cells: one panel, row-major
	PageLen  = 2 * Panels * PanelLen // 7,616 bytes: the page, two bytes a cell
)

// Geometry is the page's shape as raster sees it.
var Geometry = raster.Geometry{Cols: Cols, Rows: Rows, Panels: Panels}

// A Page is the 7,616 bytes (RASTER.t, "The page"). The zero Page is
// blank. Everything about its cells -- rows, rendering -- is the
// raster's, through Raster.
type Page [PageLen]byte

// Raster reads the page as a raster page of tessera's geometry.
func (p *Page) Raster() (*raster.Page, error) { return raster.Of(Geometry, p[:]) }

// Compile is raster.Compile on tessera's geometry, returning the
// page as its bytes.
func Compile(src string) (*Page, error) {
	rp, err := raster.Compile(Geometry, src)
	if err != nil {
		return nil, err
	}
	p := new(Page)
	copy(p[:], rp.Bytes())
	return p, nil
}
