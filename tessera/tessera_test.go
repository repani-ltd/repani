package tessera

import (
	"strings"
	"testing"
)

// The cell model, ink and language are tested in typeset/raster;
// here only what tessera adds: the numbers and the view.
func TestGeometry(t *testing.T) {
	if PageLen != 7616 || PanelLen != 952 {
		t.Fatalf("geometry: page %d panel %d", PageLen, PanelLen)
	}
	if Geometry.Size() != PageLen {
		t.Fatalf("raster geometry size %d", Geometry.Size())
	}
}

// "TESSERA" in yellow at panel 2, row 3, column 6: cell 2012, bytes
// 4024 and 4025 the T and its ink, and nothing else on the page.
func TestVector(t *testing.T) {
	p, err := Compile(".panel 2\n.at 3 6\n.fg yellow\nTESSERA\n")
	if err != nil {
		t.Fatal(err)
	}
	o := 2 * Geometry.Offset(2, 3, 6)
	if o != 4024 || p[o] != 'T' || p[o+1] != 0x03 || p[o+12] != 'A' || p[o+13] != 0x03 {
		t.Fatalf("bytes at %d: % X", o, p[o:o+14])
	}
	n := 0
	for _, b := range p {
		if b != 0 {
			n++
		}
	}
	if n != 14 {
		t.Fatalf("%d nonzero bytes, want 14", n)
	}
}

// The raster view reads the page and renders it; the spec states
// the geometry and leaves the rest to RASTER.t.
func TestRasterView(t *testing.T) {
	p, err := Compile(".panel 1\n.fg yellow\nΚΑΙΡΟΣ\n")
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Raster()
	if err != nil || r.Geometry != Geometry {
		t.Fatalf("Raster: %v %+v", err, r.Geometry)
	}
	if rows := r.Text(1); rows[0] != "ΚΑΙΡΟΣ" || len(rows) != Rows {
		t.Fatalf("text = %q", rows[:2])
	}
	if s := Spec(); !strings.Contains(s, "# The page") || strings.Contains(s, "0x80+n") {
		t.Fatal("Spec should state the page and leave ink to RASTER.t")
	}
}
