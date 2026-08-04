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

func TestTabularFigures(t *testing.T) {
	// applyTnum runs at Parse time: in both Fira Sans weights every
	// digit must share one advance, and the figure space (U+2007)
	// must match it, so figure-space padding aligns numbers exactly
	// (DESIGN.md §6). Cross-weight equality means bold totals align
	// with regular body rows.
	widths := map[string]int{}
	for _, name := range []string{"FiraSans-Regular.ttf", "FiraSans-Bold.ttf"} {
		raw, err := os.ReadFile("../fonts/" + name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := Parse(raw)
		if err != nil {
			t.Fatalf("Parse(%s): %v", name, err)
		}
		w0 := f.CIDWidths['0']
		if w0 == 0 {
			t.Fatalf("%s: no width for '0'", name)
		}
		for r := '1'; r <= '9'; r++ {
			if w := f.CIDWidths[int(r)]; w != w0 {
				t.Errorf("%s: width('%c') = %d, want %d", name, r, w, w0)
			}
		}
		if w := f.CIDWidths[0x2007]; w != w0 {
			t.Errorf("%s: figure space = %d, want digit width %d", name, w, w0)
		}
		widths[name] = w0
	}
	if widths["FiraSans-Regular.ttf"] != widths["FiraSans-Bold.ttf"] {
		t.Errorf("tabular width differs across weights: %v", widths)
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
