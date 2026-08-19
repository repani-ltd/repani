package ttf

import (
	"bytes"
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
	// (DESIGN.t §6). Cross-weight equality means bold totals align
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

// Accented Latin in Fira Sans is built from compound glyphs; the
// subset must carry the components (base letter and accent) even
// when no used codepoint maps to them directly.
func TestSubsetKeepsCompoundComponents(t *testing.T) {
	f := loadFont(t, "FiraSans-Regular.ttf")
	tables, err := parseTables(f.Data)
	if err != nil {
		t.Fatal(err)
	}
	glyf, _ := tableGet(f.Data, tables, "glyf")
	loca, _ := tableGet(f.Data, tables, "loca")
	head, _ := tableGet(f.Data, tables, "head")
	maxp, _ := tableGet(f.Data, tables, "maxp")
	numGlyphs := int(readU16(maxp, 4))
	offsets := readLoca(loca, readI16(head, 50), numGlyphs)

	used := map[rune]bool{'é': true, 'ü': true}
	keep := map[uint16]bool{0: true}
	for r := range used {
		keep[f.CharToGID[int(r)]] = true
	}
	direct := len(keep)
	addCompounds(glyf, offsets, keep, numGlyphs)
	if len(keep) <= direct {
		t.Fatalf("é/ü are not compound glyphs in this face (keep=%d); test premise broken", len(keep))
	}

	s, err := f.Subset(used)
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	st, err := parseTables(s.Data)
	if err != nil {
		t.Fatal(err)
	}
	sglyf, _ := tableGet(s.Data, st, "glyf")
	sloca, _ := tableGet(s.Data, st, "loca")
	soffs := readLoca(sloca, readI16(head, 50), numGlyphs)
	for gid := range keep {
		if soffs[gid] >= soffs[gid+1] {
			t.Errorf("glyph %d (kept component or base) is empty in the subset", gid)
		}
	}
	// Every glyph not in keep is empty, so the components are kept
	// by design, not because subsetting is a no-op.
	for gid := uint16(0); int(gid) < numGlyphs; gid++ {
		if !keep[gid] && soffs[gid] != soffs[gid+1] {
			t.Errorf("glyph %d not in keep set but present in subset", gid)
		}
	}
	if len(sglyf) >= len(glyf) {
		t.Errorf("subset glyf %d not smaller than original %d", len(sglyf), len(glyf))
	}
}

// Parse recovers the panics of its offset-chained reads into an
// error: truncated and garbage input must not crash.
func TestParseMalformed(t *testing.T) {
	f := loadFiraMono(t)
	// Clone: a reslice of f.Data keeps the full capacity, and slicing
	// within capacity does not panic.
	inputs := map[string][]byte{
		"empty":   {},
		"garbage": []byte("this is not a font at all, just some bytes"),
		"header":  bytes.Clone(f.Data[:12]),
		"clipped": bytes.Clone(f.Data[:4096]), // directory intact, table data cut
	}
	for name, raw := range inputs {
		if font, err := Parse(raw); err == nil || font != nil {
			t.Errorf("%s: Parse returned font=%v err=%v, want error", name, font != nil, err)
		}
	}
	// A prefix that still holds every table Parse reads (glyf comes
	// last) parses, and Subset then recovers the same way.
	third, err := Parse(bytes.Clone(f.Data[:len(f.Data)/3]))
	if err != nil {
		t.Fatalf("Parse(prefix): %v", err)
	}
	if res, err := third.Subset(map[rune]bool{'a': true}); err == nil || res != nil {
		t.Errorf("Subset on truncated glyf returned res=%v err=%v, want error", res != nil, err)
	}
}
