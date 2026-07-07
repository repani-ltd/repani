package ttf

import (
	"os"
	"testing"
)

func loadFiraMono(t *testing.T) *TTFont {
	t.Helper()
	raw, err := os.ReadFile("../fonts/FiraMono-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

func TestParseFiraMono(t *testing.T) {
	f := loadFiraMono(t)
	if f.PostScriptName != "FiraMono-Regular" {
		t.Errorf("PostScriptName = %q", f.PostScriptName)
	}
	if f.UnitsPerEm == 0 || f.Ascent <= 0 || f.Descent >= 0 {
		t.Errorf("bad metrics: upem=%d ascent=%d descent=%d", f.UnitsPerEm, f.Ascent, f.Descent)
	}
	// Monospace: ASCII and Greek all advance 600/1000 em.
	for _, r := range "AzM09 αβγΩλ·" {
		if r == ' ' {
			continue
		}
		if w := f.CIDWidths[int(r)]; w != 600 {
			t.Errorf("width of %q = %d, want 600", r, w)
		}
	}
	if f.DefaultWidth != 600 {
		t.Errorf("DefaultWidth = %d, want 600", f.DefaultWidth)
	}
}

func TestSubsetKeepsUsedGlyphs(t *testing.T) {
	f := loadFiraMono(t)
	used := map[rune]bool{'H': true, 'i': true, 'λ': true}
	s, err := f.Subset(used)
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	if len(s.Data) == 0 || len(s.Data) >= len(f.Data) {
		t.Errorf("subset size %d not smaller than original %d", len(s.Data), len(f.Data))
	}
	// The subset must itself be a parseable TTF whose cmap-independent
	// structure is intact (we re-parse tables directly).
	if _, err := parseTables(s.Data); err != nil {
		t.Fatalf("subset does not parse: %v", err)
	}
	// CIDToGID map: 2 bytes per CID up to MaxCID, non-zero at used CIDs.
	for r := range used {
		off := int(r) * 2
		if off+1 >= len(s.CIDToGID) {
			t.Fatalf("CIDToGID too short for %q", r)
		}
		gid := uint16(s.CIDToGID[off])<<8 | uint16(s.CIDToGID[off+1])
		if gid == 0 {
			t.Errorf("used rune %q maps to GID 0 in subset", r)
		}
	}
	for r := range used {
		if w := s.Widths[int(r)]; w != 600 {
			t.Errorf("subset width of %q = %d, want 600", r, w)
		}
	}
}
