package tessera

import (
	"bytes"
	"strings"
	"testing"
)

// The cell model, ink and language are tested in typeset/raster on
// this geometry; here only what tessera adds: the numbers, the tile,
// and the view.
func TestGeometry(t *testing.T) {
	if PageLen != 3808 || PanelLen != 952 || TileLen != 238 || Tiles != 16 || TileRows != 7 {
		t.Fatalf("geometry: page %d panel %d tile %d tiles %d rows/tile %d", PageLen, PanelLen, TileLen, Tiles, TileRows)
	}
	if Geometry.Len() != PageLen {
		t.Fatalf("raster geometry length %d", Geometry.Len())
	}
	if o := Geometry.Offset(2, 3, 5); o != 2011 || o/TileLen != 8 || o%TileLen != 107 {
		t.Fatalf("offset(2,3,5) = %d", o)
	}
}

// The frozen vector of TESSERA.t: "TESSERA" in yellow at panel 2, row
// 3, column 6 is tile 8, bytes 107..114, with the ink code at column 5.
func TestFrozenVector(t *testing.T) {
	p, err := Compile(".panel 2\n.at 3 6\n.fg yellow\nTESSERA\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x83, 0x54, 0x45, 0x53, 0x53, 0x45, 0x52, 0x41}
	if got := p.Tile(8)[107:115]; !bytes.Equal(got, want) {
		t.Fatalf("tile 8 [107:115] = % X, want % X", got, want)
	}
	if n := PageLen - bytes.Count(p[:], []byte{0}); n != 8 {
		t.Fatalf("%d nonzero bytes, want 8", n)
	}
}

// The raster view aliases the page and renders it; the spec states
// the tile and leaves the rest to RASTER.t.
func TestRasterView(t *testing.T) {
	p, err := Compile(".panel 1\n.fg yellow\nΚΑΙΡΟΣ\n")
	if err != nil {
		t.Fatal(err)
	}
	r := p.Raster()
	if &r.Cells[0] != &p[0] || r.Geometry != Geometry {
		t.Fatal("Raster does not alias the page on tessera's geometry")
	}
	if rows := r.Text(1); rows[0] != "ΚΑΙΡΟΣ" || len(rows) != Rows {
		t.Fatalf("text = %q", rows[:2])
	}
	if s := Spec(); !strings.Contains(s, "# The tile") || strings.Contains(s, "0x80+n") {
		t.Fatal("Spec should state the tile and leave ink to RASTER.t")
	}
}
