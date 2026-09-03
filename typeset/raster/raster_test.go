package raster

import (
	"bytes"
	"strings"
	"testing"
)

// tessera's geometry, the first instantiation; the tests that fix
// the cell model run on it, and TestGeometryIsAParameter on others.
var tess = Geometry{Cols: 34, Rows: 28, Panels: 4}

func compile(t *testing.T, src string) *Page {
	t.Helper()
	p, err := Compile(tess, src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return p
}

func nonzero(p *Page) int {
	n := 0
	for _, b := range p.Cells {
		if b != 0 {
			n++
		}
	}
	return n
}

func TestGeometry(t *testing.T) {
	if tess.PanelLen() != 952 || tess.Len() != 3808 || tess.Offset(2, 3, 5) != 2011 {
		t.Fatalf("geometry: panel %d page %d offset %d", tess.PanelLen(), tess.Len(), tess.Offset(2, 3, 5))
	}
	g := Geometry{Cols: 40, Rows: 10, Panels: 3}
	if g.Len() != 1200 || g.Offset(1, 2, 3) != 483 {
		t.Fatalf("40x10x3: len %d offset %d", g.Len(), g.Offset(1, 2, 3))
	}
	p := Of(g, make([]byte, 1200))
	if len(p.Row(2, 9)) != 40 || &p.Row(2, 9)[0] != &p.Cells[1160] {
		t.Fatal("Row does not alias the cells")
	}
}

func TestGeometryIsAParameter(t *testing.T) {
	// The same source compiles on any geometry; bounds and errors
	// follow the geometry, and a wide row holds what a narrow one
	// refuses.
	g := Geometry{Cols: 40, Rows: 3, Panels: 2}
	p, err := Compile(g, ".panel 1\n.at 2 30\n.ink red\nABCDEFGHI\n")
	if err != nil {
		t.Fatal(err)
	}
	if r := p.Row(1, 2); r[30] != InkFG+1 || r[31] != 'A' || r[39] != 'I' {
		t.Fatalf("row = % X", r[28:])
	}
	if _, err := Compile(tess, ".panel 1\n.at 2 30\n.ink red\nABCDEFGHI\n"); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("34 columns accepted a 10-cell run at 30: %v", err)
	}
	for _, tc := range []struct{ src, want string }{
		{".panel 2\n", "panel 2 out of range 0..1"},
		{".panel 0\n.at 3 0\n", "outside rows 0..2, cols 0..39"},
		{".panel 0\n\n\n\nx\n", "below row 2"},
		{".panel 0\n.fill 0 0 41 1\n", "outside the panel"},
	} {
		if _, err := Compile(g, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.src, err, tc.want)
		}
	}
	// Single-column, single-row, single-panel is a page too.
	if p, err := Compile(Geometry{1, 1, 1}, ".panel 0\nX\n"); err != nil || p.Cells[0] != 'X' {
		t.Fatalf("1x1x1: %v %v", p, err)
	}
}

// "TESSERA" in yellow at panel 2, row 3, column 5: the ink code at
// column 5 and the T at 6 (tessera's frozen vector, on its geometry).
func TestFrozenVector(t *testing.T) {
	p := compile(t, ".panel 2\n.at 3 5\n.ink yellow\nTESSERA\n")
	want := []byte{0x83, 0x54, 0x45, 0x53, 0x53, 0x45, 0x52, 0x41}
	o := tess.Offset(2, 3, 5)
	if got := p.Cells[o : o+8]; !bytes.Equal(got, want) {
		t.Fatalf("cells = % X, want % X", got, want)
	}
	if n := nonzero(p); n != 8 {
		t.Fatalf("%d nonzero bytes, want 8", n)
	}
}

func TestInkCosts(t *testing.T) {
	// No code when the pen matches the row's state.
	p := compile(t, ".panel 0\nAB\n")
	if got := p.Row(0, 0)[:3]; !bytes.Equal(got, []byte{'A', 'B', 0}) {
		t.Fatalf("plain row = % X", got)
	}
	// A continuation in a new foreground spends one cell, the gap.
	p = compile(t, ".panel 0\nAB\n.ink cyan\n+ CD\n")
	if got := p.Row(0, 0)[:5]; !bytes.Equal(got, []byte{'A', 'B', InkFG + 6, 'C', 'D'}) {
		t.Fatalf("fg change = % X", got)
	}
	// Foreground and background together spend two.
	p = compile(t, ".panel 0\n.ink white on blue\nX\n")
	if got := p.Row(0, 0)[:3]; !bytes.Equal(got, []byte{InkBG + 4, InkFG + 7, 'X'}) {
		t.Fatalf("fg+bg change = % X", got)
	}
	// .ink FG alone means FG on the default background: inside a bar
	// that is two codes, and the bar is cut.
	p = compile(t, ".panel 0\n.ink white on blue\nX\n.ink red\n+ Y\n")
	if got := p.Row(0, 0)[:6]; !bytes.Equal(got, []byte{InkBG + 4, InkFG + 7, 'X', InkBG, InkFG + 1, 'Y'}) {
		t.Fatalf("fg-only after bar = % X", got)
	}
	// Back to default costs the same cell again.
	p = compile(t, ".panel 0\n.ink red\nA\n.ink default\n+ B\n")
	if got := p.Row(0, 0)[:4]; !bytes.Equal(got, []byte{InkFG + 1, 'A', InkFG, 'B'}) {
		t.Fatalf("reset = % X", got)
	}
}

func TestFill(t *testing.T) {
	// A bar: code at the left edge, spaces, closing code at the right
	// edge; text over it inherits the background without a new code.
	p := compile(t, ".panel 0\n.ink default on blue\n.fill 0 0 10 2\n.at 0 2\nHI\n")
	row := p.Row(0, 0)
	want := append([]byte{InkBG + 4, ' ', 'H', 'I'}, bytes.Repeat([]byte{' '}, 6)...)
	want = append(want, InkBG)
	if !bytes.Equal(row[:11], want) {
		t.Fatalf("bar row = % X, want % X", row[:11], want)
	}
	if row[11] != 0 {
		t.Fatalf("cell past the closing code = %X", row[11])
	}
	if r1 := p.Row(0, 1); r1[0] != InkBG+4 || r1[10] != InkBG {
		t.Fatalf("second bar row = % X", r1[:11])
	}
	// A fill to the row's end has no closing code.
	p = compile(t, ".panel 0\n.ink default on red\n.fill 5 30 4 1\n")
	if r := p.Row(0, 5); r[30] != InkBG+1 || r[33] != ' ' {
		t.Fatalf("edge fill = % X", r[28:])
	}
	// A default fill over content clears it, codes included.
	p = compile(t, ".panel 0\n.ink red\nABC\n.ink default\n.fill 0 0 34 1\n")
	if n := nonzero(p); n != 34 || p.Row(0, 0)[0] != ' ' {
		t.Fatalf("clearing fill: %d nonzero, first %X", n, p.Row(0, 0)[0])
	}
	// Text in a different foreground over a bar spends its code inside
	// the bar and keeps the bar's background.
	p = compile(t, ".panel 0\n.ink default on blue\n.fill 0 0 10 1\n.ink yellow on blue\n.at 0 3\nQ\n")
	if r := p.Row(0, 0); r[3] != InkFG+3 || r[4] != 'Q' || r[10] != InkBG {
		t.Fatalf("text over bar = % X", r[:11])
	}
}

func TestErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"HELLO\n", "content before .panel"},
		{".panel 4\n", "panel 4 out of range"},
		{".panel 0\n.at 28 0\n", "outside rows"},
		{".panel 0\n.at 0 0\n+ X\n", "nothing to continue"},
		{".panel 0\n.bogus\n", "unknown command"},
		{".panel 0\n.ink puce\n", "unknown color"},
		{".panel 0\n日本\n", "outside the cell repertoire"},
		{".panel 0\n.at 0 30\nHELLO\n", "overflow the row"},
		{".panel 0\n.ink red\n.at 0 33\nA\n", "overflow the row"},
		{".panel 0\n.ink red\nA\n.ink default\n.at 0 0\nB\n", "content over an ink code"},
		{".panel 0\nABCDEFGHIJKL\n.ink default on blue\n.fill 0 0 5 1\n", "closing ink code at column 5 lands on content"},
		{".panel 0\n.ink red on blue\n.fill 0 0 1 1\n", "cannot hold its 2 ink codes"},
		{".panel 0\n.fill 0 0 35 1\n", "outside the panel"},
		{".panel 0\n+\n", "empty continuation"},
		{".panel 0\n" + strings.Repeat("x\n", 29), "below row 27"},
	} {
		_, err := Compile(tess, tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.src, err, tc.want)
		}
	}
}

func TestReproducibleAndText(t *testing.T) {
	src := ".panel 1\n.ink yellow\nΚΑΙΡΟΣ ─── 12°\n.ink default\n\nΑθήνα   21°\n. dotted\n"
	a := compile(t, src)
	b := compile(t, src)
	if !bytes.Equal(a.Cells, b.Cells) {
		t.Fatal("compilation is not reproducible")
	}
	rows := a.Text(1)
	if rows[0] != " ΚΑΙΡΟΣ ─── 12°" || rows[1] != "" || rows[2] != "Αθήνα   21°" || rows[3] != ". dotted" {
		t.Fatalf("text = %q", rows[:4])
	}
	if len(rows) != tess.Rows || a.Text(0)[0] != "" {
		t.Fatalf("text shape: %d rows, panel 0 row 0 %q", len(rows), a.Text(0)[0])
	}
}

func TestCellTable(t *testing.T) {
	// Round trip for every glyph value; codes and unassigned render blank.
	glyphs := 0
	for b := range 256 {
		r := CellRune(byte(b))
		if b == 0 || IsInk(byte(b)) || (b >= 0x98 && b <= 0xBF) {
			if r != ' ' {
				t.Errorf("%02X renders %q, want blank", b, r)
			}
			continue
		}
		glyphs++
		cells, err := Transcode(string(r))
		if err != nil || len(cells) != 1 || cells[0] != byte(b) {
			t.Errorf("%02X %q: round trip %X %v", b, r, cells, err)
		}
	}
	if glyphs != 199 { // 31 symbols + 95 ASCII + € + 8 weather + 64 Greek
		t.Fatalf("%d glyph values, want 199", glyphs)
	}
}

func TestANSIAndLayout(t *testing.T) {
	g := Geometry{Cols: 6, Rows: 2, Panels: 3}
	p, err := Compile(g, ".panel 0\n.ink red\nAB\n.panel 1\nCD\n.panel 2\n.at 1 0\nEF\n")
	if err != nil {
		t.Fatal(err)
	}
	if a := p.ANSI(0); a[0] != "\x1b[0m\x1b[31m AB   \x1b[0m" {
		t.Fatalf("ANSI = %q", a[0])
	}
	got := Layout(p.Rendered(p.Text), g.Cols, 2)
	// Panels 0 and 1 side by side (two rows), a blank line, then
	// panel 2 alone (two rows).
	want := []string{" AB     CD", "", "", "", "EF"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Layout = %q, want %q", got, want)
	}
}

func TestHTML(t *testing.T) {
	p := compile(t, ".panel 0\n.ink white on blue\n.fill 0 0 4 1\n.at 0 1\n<>\n.ink default\n.at 1 0\nplain\n")
	rows := p.HTMLRows(0)
	if want := `<span class="f0 b4"> </span><span class="f7 b4"> &lt;&gt;</span><span class="f7 b0"> </span>` + strings.Repeat(" ", 29); rows[0] != want {
		t.Fatalf("row 0 = %q, want %q", rows[0], want)
	}
	if want := "plain" + strings.Repeat(" ", 29); rows[1] != want {
		t.Fatalf("row 1 = %q", rows[1])
	}
	doc := HTMLDocument(p, 2, "t<t")
	if !strings.Contains(doc, "<title>t&lt;t</title>") || strings.Count(doc, "<pre>") != 4 || !strings.Contains(doc, "repeat(2, max-content)") {
		t.Fatal("document shape")
	}
}

func TestSpec(t *testing.T) {
	s := Spec()
	for _, want := range []string{"# The page", "# Cells", "# Ink", "# Authoring"} {
		if !strings.Contains(s, want) {
			t.Errorf("spec missing %q", want)
		}
	}
}
