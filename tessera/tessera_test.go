package tessera

import (
	"bytes"
	"strings"
	"testing"
)

func compile(t *testing.T, src string) *Page {
	t.Helper()
	p, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return p
}

func nonzero(p *Page) int {
	n := 0
	for _, b := range p {
		if b != 0 {
			n++
		}
	}
	return n
}

func TestGeometry(t *testing.T) {
	if PageLen != 3808 || PanelLen != 952 || TileLen != 238 || Tiles != 16 || TileRows != 7 {
		t.Fatalf("geometry: page %d panel %d tile %d tiles %d rows/tile %d", PageLen, PanelLen, TileLen, Tiles, TileRows)
	}
	if o := Offset(2, 3, 5); o != 2011 || o/TileLen != 8 || o%TileLen != 107 {
		t.Fatalf("offset(2,3,5) = %d", o)
	}
}

// The frozen vector of TESSERA.t: "TESSERA" in yellow at panel 2, row
// 3, column 5 is tile 8, bytes 107..114, with the ink code at column 5.
func TestFrozenVector(t *testing.T) {
	p := compile(t, ".panel 2\n.at 3 5\n.ink yellow\nTESSERA\n")
	want := []byte{0x83, 0x54, 0x45, 0x53, 0x53, 0x45, 0x52, 0x41}
	if got := p.Tile(8)[107:115]; !bytes.Equal(got, want) {
		t.Fatalf("tile 8 [107:115] = % X, want % X", got, want)
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
		_, err := Compile(tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.src, err, tc.want)
		}
	}
}

func TestReproducibleAndText(t *testing.T) {
	src := ".panel 1\n.ink yellow\nΚΑΙΡΟΣ ─── 12°\n.ink default\n\nΑθήνα   21°\n. dotted\n"
	a := compile(t, src)
	b := compile(t, src)
	if *a != *b {
		t.Fatal("compilation is not reproducible")
	}
	rows := a.Text(1)
	if rows[0] != " ΚΑΙΡΟΣ ─── 12°" || rows[1] != "" || rows[2] != "Αθήνα   21°" || rows[3] != ". dotted" {
		t.Fatalf("text = %q", rows[:4])
	}
	if len(rows) != Rows || a.Text(0)[0] != "" {
		t.Fatalf("text shape: %d rows, panel 0 row 0 %q", len(rows), a.Text(0)[0])
	}
}

func TestCellTable(t *testing.T) {
	// Round trip for every glyph value; codes and unassigned render blank.
	glyphs := 0
	for b := 0; b < 256; b++ {
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
