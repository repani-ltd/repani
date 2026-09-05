package raster

import (
	"bytes"
	"strings"
	"testing"
)

// A 34 by 28 by 4 geometry; the tests that fix the cell model run on
// it, and TestGeometryIsAParameter on others.
var g34 = Geometry{Cols: 34, Rows: 28, Panels: 4}

func compile(t *testing.T, src string) *Page {
	t.Helper()
	p, err := Compile(g34, src)
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

// row compiles src and returns row 0 of panel 0 as its body (trailing
// zeros trimmed) and its tail (the codes at its end).
func row(t *testing.T, src string) ([]byte, []byte) {
	t.Helper()
	r := compile(t, src).Row(0, 0)
	ts := tailStart(r)
	return bytes.TrimRight(r[:ts], "\x00"), r[ts:]
}

func TestGeometry(t *testing.T) {
	if g34.PanelLen() != 952 || g34.Len() != 3808 || g34.Offset(2, 3, 5) != 2011 {
		t.Fatalf("geometry: panel %d page %d offset %d", g34.PanelLen(), g34.Len(), g34.Offset(2, 3, 5))
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
	g := Geometry{Cols: 40, Rows: 3, Panels: 2}
	p, err := Compile(g, ".panel 1\n.at 2 31\n.fg red\nABCDEFGHI\n")
	if err != nil {
		t.Fatal(err)
	}
	if r := p.Row(1, 2); r[30] != InkFG+1 || r[31] != 'A' || r[39] != 'I' {
		t.Fatalf("row = % X", r[28:])
	}
	if _, err := Compile(g34, ".panel 1\n.at 2 31\n.fg red\nABCDEFGHI\n"); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("34 columns accepted a 9-cell run at 31: %v", err)
	}
	for _, tc := range []struct{ src, want string }{
		{".panel 2\n", "panel 2 out of range 0..1"},
		{".at 3\n", "outside rows 0..2, cols 0..39"},
		{"\n\n\nx\n", "below row 2"},
		{".fill 0 0 1 41\n", "outside the panel"},
		{".margin 40\n", "outside columns 0..39"},
	} {
		if _, err := Compile(g, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.src, err, tc.want)
		}
	}
	// Single-column, single-row, single-panel is a page too, and the
	// page starts in panel 0.
	if p, err := Compile(Geometry{1, 1, 1}, "X\n"); err != nil || p.Cells[0] != 'X' {
		t.Fatalf("1x1x1: %v %v", p, err)
	}
}

// "RASTER" in yellow at panel 2, row 3, column 6: the ink code in the
// gap at column 5 and the R at 6.
func TestVector(t *testing.T) {
	p := compile(t, ".panel 2\n.at 3 6\n.fg yellow\nRASTER\n")
	want := []byte{0x83, 0x52, 0x41, 0x53, 0x54, 0x45, 0x52}
	o := g34.Offset(2, 3, 5)
	if got := p.Cells[o : o+7]; !bytes.Equal(got, want) {
		t.Fatalf("cells = % X, want % X", got, want)
	}
	if n := nonzero(p); n != 7 {
		t.Fatalf("%d nonzero bytes, want 7", n)
	}
	// The same bytes from a leading space at column 5.
	q := compile(t, ".panel 2\n.at 3 5\n.fg yellow\n RASTER\n")
	if !bytes.Equal(p.Cells, q.Cells) {
		t.Fatal("a leading space and .at one column right differ")
	}
}

func TestInkPlacement(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      []byte // the row's body, trailing zeros trimmed
		tail      []byte // the codes at its end
	}{
		{"plain", "AB\n", []byte("AB"), nil},
		{"gap takes the code", "AB\n.fg cyan\n+ CD\n", []byte{'A', 'B', InkFG + 6, 'C', 'D'}, nil},
		{"two attributes, two gaps, bar closed after", "AB\n.fg white\n.bg blue\n+  CD\n", []byte{'A', 'B', InkBG + 4, InkFG + 7, 'C', 'D', InkBG}, nil},
		{"column 0 goes to the tail", ".fg red\nALERT\n", []byte("ALERT"), []byte{InkFG + 1}},
		{"leading space, gap at 0", ".fg red\n ALERT\n", []byte{InkFG + 1, 'A', 'L', 'E', 'R', 'T'}, nil},
		{"back to default costs the gap", ".fg red\n ABC\n.fg default\n+ D\n", []byte{InkFG + 1, 'A', 'B', 'C', InkFG, 'D'}, nil},
		{"bare .fg is default", ".fg red\n ABC\n.fg\n+ D\n", []byte{InkFG + 1, 'A', 'B', 'C', InkFG, 'D'}, nil},
		{"title at column 0 in a bar: both codes in the tail", ".bg blue\n.fill 0\n.fg white\nTITLE\n", append([]byte("TITLE"), bytes.Repeat([]byte{' '}, 27)...), []byte{InkBG + 4, InkFG + 7}},
		{"fg only leaves bg alone", ".bg blue\n.fill 0\n.fg yellow\n.at 0 3\nQ\n", append([]byte{InkBG + 4, ' ', InkFG + 3, 'Q'}, bytes.Repeat([]byte{' '}, 30)...), nil},
	} {
		got, tail := row(t, tc.src)
		if !bytes.Equal(got, tc.want) || !bytes.Equal(tail, tc.tail) {
			t.Errorf("%s: row = % X tail % X, want % X tail % X", tc.name, got, tail, tc.want, tc.tail)
		}
	}
	// Opening ink with both attributes, and a tail of two.
	r := compile(t, ".fg white\n.bg blue\nX\n").Row(0, 0)
	if r[0] != 'X' || r[32] != InkBG+4 || r[33] != InkFG+7 {
		t.Fatalf("two-code tail = % X … % X", r[:2], r[32:])
	}
}

func TestOrderDoesNotMatter(t *testing.T) {
	// Painting the same cells in any order yields the same bytes: a
	// red word placed before a default one to its right recolors
	// nothing (the fix for in-band recoloring to the right).
	a := compile(t, ".at 0 10\nX\n.fg red\n.at 0\nALERT\n")
	b := compile(t, ".fg red\nALERT\n.fg default\n.at 0 10\nX\n")
	if !bytes.Equal(a.Cells, b.Cells) {
		t.Fatalf("order changed the bytes:\n% X\n% X", a.Row(0, 0), b.Row(0, 0))
	}
	c := Decode(a)
	if x := c.Row(0, 0)[10]; x.Glyph != 'X' || x.FG != 0 {
		t.Fatalf("X = %+v, want default ink", x)
	}
	if c.Row(0, 0)[0].FG != 1 {
		t.Fatal("ALERT is not red")
	}
}

func TestFill(t *testing.T) {
	// A bar: code in the first cell, spaces to the edge, no closing code;
	// text over it inherits the background without a new code.
	p := compile(t, ".bg blue\n.fill 0\n.at 0 2\nHI\n")
	r := p.Row(0, 0)
	if r[0] != InkBG+4 || r[1] != ' ' || r[2] != 'H' || r[3] != 'I' || r[4] != ' ' || r[33] != ' ' {
		t.Fatalf("bar row = % X", r)
	}
	// A partial fill closes at its right edge in the cell after it.
	p = compile(t, ".bg red\n.fill 5 10 1 4\n")
	if r := p.Row(0, 5); r[10] != InkBG+1 || r[13] != ' ' || r[14] != InkBG || r[15] != 0 {
		t.Fatalf("partial fill = % X", r[8:16])
	}
	// Two rows, column defaults, rows given.
	p = compile(t, ".bg green\n.fill 3 0 2\n")
	if p.Row(0, 3)[0] != InkBG+2 || p.Row(0, 4)[0] != InkBG+2 || p.Row(0, 5)[0] != 0 {
		t.Fatal("two-row fill")
	}
	// A default fill over content clears it.
	p = compile(t, ".fg red\nABC\n.fg default\n.fill 0\n")
	if n := nonzero(p); n != 34 || p.Row(0, 0)[0] != ' ' {
		t.Fatalf("clearing fill: %d nonzero, first %X", n, p.Row(0, 0)[0])
	}
	// A bar one short of the edge: the last cell keeps the bar's
	// background, since a code there would read as opening ink.
	p = compile(t, ".bg blue\n.fill 0 0 1 33\n")
	if r := p.Row(0, 0); r[33] != 0 || Decode(p).Row(0, 0)[33].BG != 4 {
		t.Fatalf("bar to 32: last cell %X", r[33])
	}
}

func TestMarginAndAt(t *testing.T) {
	p := compile(t, ".margin 2\n.fg yellow\nHEAD\n.fg default\nbody\n.at 5 10\nfar\nback\n")
	c := Decode(p)
	if c.Row(0, 0)[2].Glyph != 'H' || c.Row(0, 0)[2].FG != 3 || c.Row(0, 1)[2].Glyph != 'b' {
		t.Fatal("margin 2 not honoured")
	}
	if c.Row(0, 5)[10].Glyph != 'f' || c.Row(0, 6)[2].Glyph != 'b' {
		t.Fatal(".at is not one-shot, or does not return to the margin")
	}
	// .panel moves only the cursor: pen and margin persist.
	p = compile(t, ".margin 1\n.fg red\n.panel 1\nX\n")
	if x := Decode(p).Row(1, 0)[1]; x.Glyph != 'X' || x.FG != 1 {
		t.Fatalf("after .panel: %+v", x)
	}
	// The blank line flows a row and returns to the margin.
	p = compile(t, ".at 0 5\n\nY\n")
	if Decode(p).Row(0, 1)[0].Glyph != 'Y' {
		t.Fatal("blank line after .at")
	}
	// .col places on the row of the last run and leaves the cursor
	// alone: a label at the margin, its value at column 6, the next
	// line below both.
	p = compile(t, ".fg cyan\nWIND\n.fg\n.col 6\nNW 6 kt\nnext\n.col 10\nmore\n")
	c = Decode(p)
	if r := c.Row(0, 0); r[0].Glyph != 'W' || r[0].FG != 6 || r[6].Glyph != 'N' || r[6].FG != 0 || r[4].Glyph != 0 {
		t.Fatalf(".col row 0 = %q", string(c.AppendText(nil, 0, 0)))
	}
	if r := c.Row(0, 1); r[0].Glyph != 'n' || r[10].Glyph != 'm' || c.Row(0, 2)[0].Glyph != 0 {
		t.Fatalf(".col row 1 = %q", string(c.AppendText(nil, 0, 1)))
	}
	// A lone + and a +5 are content.
	p = compile(t, "+\n+5\n")
	if p.Row(0, 0)[0] != '+' || p.Row(0, 1)[1] != '5' {
		t.Fatal("+ as content")
	}
}

func TestErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{".panel 4\n", "panel 4 out of range"},
		{".at 28 0\n", "outside rows"},
		{".at 0 0\n+ X\n", "nothing to continue"},
		{".col 6\nX\n", "no run to attach to"},
		{"A\n.at 1\n.col 6\nX\n", "no run to attach to"},
		{"A\n.col 34\nX\n", "outside columns 0..33"},
		{".bogus\n", "unknown command"},
		{".fg puce\n", "unknown color"},
		{".fg red blue\n", "one color name, or none"},
		{".ink red\n", "unknown command"},
		{"日本\n", "outside the cell repertoire"},
		{".at 0 30\nHELLO\n", "overflow the row"},
		{".fg red\n" + strings.Repeat("x", 34) + "\n", "line 2: raster: panel 0 row 0: column 0 starts in ink and the row is full"},
		{"ABC\n.fg red\n.at 0 3\nD\n", "column 3 needs 1 blank cells before it"},
		{".fg white\n.bg blue\n.at 0 1\nX\n", "column 1 needs 2 blank cells"},
		{".fill 0 0 1 35\n", "outside the panel"},
		{".fill\n", "want 1 to 4 arguments"},
		{"+ \n", "nothing to continue"},
		{strings.Repeat("x\n", 29), "below row 27"},
	} {
		_, err := Compile(g34, tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.src, err, tc.want)
		}
	}
}

func TestReproducibleAndText(t *testing.T) {
	src := ".panel 1\n.fg yellow\nΚΑΙΡΟΣ ─── 12°\n.fg default\n\nΑθήνα   21°\n. dotted\n"
	a := compile(t, src)
	b := compile(t, src)
	if !bytes.Equal(a.Cells, b.Cells) {
		t.Fatal("compilation is not reproducible")
	}
	rows := a.Text(1)
	if rows[0] != "ΚΑΙΡΟΣ ─── 12°" || rows[1] != "" || rows[2] != "Αθήνα   21°" || rows[3] != ". dotted" {
		t.Fatalf("text = %q", rows[:4])
	}
	if len(rows) != g34.Rows || a.Text(0)[0] != "" {
		t.Fatalf("text shape: %d rows, panel 0 row 0 %q", len(rows), a.Text(0)[0])
	}
}

func TestDecodeEncode(t *testing.T) {
	// Encode(Decode(p)) is p for a compiled page, and Decode reads the
	// tail as opening ink.
	src := ".fg red\nALERT\n.fg default\n+ now\n.bg blue\n.fill 1\n.fg white\n.at 1 2\nTITLE\n.fg default\n.bg default\n.at 3 4\nx\n"
	p := compile(t, src)
	q, err := Decode(p).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.Cells, q.Cells) {
		t.Fatalf("round trip:\n% X\n% X", p.Row(0, 0), q.Row(0, 0))
	}
	c := Decode(p)
	if r := c.Row(0, 0); r[0].FG != 1 || r[0].Glyph != 'A' || r[6].FG != 0 || r[6].Glyph != 'n' {
		t.Fatalf("decoded row 0: %+v %+v", r[0], r[6])
	}
}

func TestCellTable(t *testing.T) {
	// Round trip for every glyph value; codes and unassigned render blank.
	glyphs := 0
	for b := range 256 {
		r := CellRune(byte(b))
		if b == 0 || IsInk(byte(b)) || (b >= 0x11 && b <= 0x1F) || (b >= 0xA6 && b <= 0xBF) {
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
	if glyphs != 198 { // 16 symbols + 95 ASCII + € + 7 weather + 6 typographic + 9 marks + 64 Greek
		t.Fatalf("%d glyph values, want 198", glyphs)
	}
}

func TestANSIAndLayout(t *testing.T) {
	g := Geometry{Cols: 6, Rows: 2, Panels: 3}
	p, err := Compile(g, ".fg red\n AB\n.fg default\n.panel 1\nCD\n.panel 2\n.at 1\nEF\n")
	if err != nil {
		t.Fatal(err)
	}
	if a := p.ANSI(0); a[0] != "\x1b[0m\x1b[31m AB   \x1b[0m" {
		t.Fatalf("ANSI = %q", a[0])
	}
	got := Layout(p.Rendered(p.Text), g.Cols, 2)
	want := []string{" AB     CD", "", "", "", "EF"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Layout = %q, want %q", got, want)
	}
}

func TestHTML(t *testing.T) {
	p := compile(t, ".bg blue\n.fill 0 0 1 5\n.fg white\n.at 0 2\n<>\n.fg default\n.bg default\n.at 1\nplain\n")
	rows := p.HTMLRows(0)
	if want := `<span class="f0 b4">  </span><span class="f7 b4">&lt;&gt; </span>` + strings.Repeat(" ", 29); rows[0] != want {
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
	// The Authoring example is a page: it compiles on the 34-column
	// geometry, with its red ALERT in the tail of its row.
	a := strings.Index(s, "    .rem A notice:")
	b := strings.Index(s[a:], ".end")
	var src strings.Builder
	for _, l := range strings.Split(s[a:a+b], "\n") {
		src.WriteString(strings.TrimPrefix(l, "    ") + "\n")
	}
	p, err := Compile(g34, src.String())
	if err != nil {
		t.Fatalf("spec example: %v", err)
	}
	if r := p.Row(0, 7); r[0] != 'A' || r[33] != InkFG+1 || r[5] != InkFG {
		t.Fatalf("ALERT row = % X", r)
	}
	if rows := p.Text(0); rows[0] != "  HARBOUR NOTICE · 02 SEP" || rows[6] != "FUEL    06:00-14:00, south quay" {
		t.Fatalf("spec example text = %q", rows[:8])
	}
	if l := Decode(p).Links(0, 10); len(l) != 1 || l[0] != (Link{Col: 4, Len: 7, Target: "tides"}) {
		t.Fatalf("spec example links = %+v", l)
	}
}

func TestLinks(t *testing.T) {
	p := compile(t, "Tap [close] or [tide tables]\n[] [x\n.fg red\n[ALERT] now\nno]link[\n")
	c := Decode(p)
	want := []Link{{4, 7, "close"}, {15, 13, "tide tables"}}
	got := c.Links(0, 0)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("links = %+v, want %+v", got, want)
	}
	// An empty pair is no link; an unclosed bracket ends the search.
	if l := c.Links(0, 1); len(l) != 0 {
		t.Fatalf("row 1 links = %+v", l)
	}
	// A link in ink at column 0: the bracket is the glyph in the first
	// cell, its ink in the tail, and the link is still derived.
	if l := c.Links(0, 2); len(l) != 1 || l[0].Target != "ALERT" || c.Row(0, 2)[0].FG != 1 {
		t.Fatalf("row 2 links = %+v", l)
	}
	if l := c.Links(0, 3); len(l) != 0 {
		t.Fatalf("row 3 links = %+v", l)
	}
	// Links survive the text renderer as typed and become anchors in
	// HTML, wrapping the span with its ink inside.
	if rows := p.Text(0); rows[0] != "Tap [close] or [tide tables]" {
		t.Fatalf("text = %q", rows[0])
	}
	h := p.HTMLRows(0)
	if !strings.HasPrefix(h[0], `Tap <a href="#close">[close]</a> or <a href="#tide tables">[tide tables]</a>`) {
		t.Fatalf("html row 0 = %q", h[0])
	}
	if !strings.HasPrefix(h[2], `<a href="#ALERT"><span class="f1 b0">[ALERT]</span></a> <span class="f1 b0">now`) {
		t.Fatalf("html row 2 = %q", h[2])
	}
}

func TestCanvasReuse(t *testing.T) {
	// A canvas compiled twice is exactly the second page, and the
	// append renderers give the same rows as the allocating ones.
	c := NewCanvas(g34)
	if err := c.Compile(".fg red\nALERT\n.bg blue\n.fill 3\n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Compile(".at 1 3\nquiet\n"); err != nil {
		t.Fatal(err)
	}
	p := New(g34)
	if err := c.EncodeInto(p); err != nil {
		t.Fatal(err)
	}
	want := compile(t, ".at 1 3\nquiet\n")
	if !bytes.Equal(p.Cells, want.Cells) {
		t.Fatal("a reused canvas kept something")
	}
	if got, want := string(c.AppendANSI(nil, 0, 1)), want.ANSI(0)[1]; got != want {
		t.Fatalf("AppendANSI %q, ANSI %q", got, want)
	}
	if got := string(c.AppendText(nil, 0, 1)); got != "   quiet" {
		t.Fatalf("AppendText %q", got)
	}
	if err := c.EncodeInto(New(Geometry{1, 1, 1})); err == nil {
		t.Fatal("EncodeInto accepted another geometry")
	}
	// Errors from encoding name the line that painted the row.
	if err := c.Compile(".fg red\n" + strings.Repeat("x", 34) + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := c.encodeInto(p); err == nil || !strings.Contains(err.Error(), "line 2:") {
		t.Fatalf("attributed error = %v", err)
	}
}

func TestGreekCapitals(t *testing.T) {
	got, err := Transcode("Άραξος Έβρος Ίος Ϊ")
	want, _ := Transcode("Αραξος Εβρος Ιος Ι")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("accented capitals: % X %v, want % X", got, err, want)
	}
}

func TestJSEmbedded(t *testing.T) {
	if s := JS(); !strings.Contains(s, "export function decode(") || !strings.Contains(s, "export function paint(") {
		t.Fatal("JS() is not the decoder")
	}
}

func TestAliases(t *testing.T) {
	vocab := ".def bar TITLE\n.fg white\n.bg blue\n.fill 0\n.at 0 2\n$TITLE\n.fg\n.bg\n.enddef\n" +
		".def field LABEL VALUE\n.fg cyan\n$LABEL\n.fg\n.col 6\n$VALUE\n.enddef\n" +
		".def wind SPEED\n.field WIND NW $SPEED kt\n.enddef\n"
	p := compile(t, vocab+".bar HARBOUR · 02 SEP\n.at 2\n.field TEMP 31°C  dew 11°C\n.wind 18\nplain $x\n")
	rows := p.Text(0)
	if rows[0] != "  HARBOUR · 02 SEP" || rows[2] != "TEMP  31°C  dew 11°C" || rows[3] != "WIND  NW 18 kt" || rows[4] != "plain $x" {
		t.Fatalf("rows = %q", rows[:5])
	}
	c := Decode(p)
	if c.Row(0, 0)[2].FG != 7 || c.Row(0, 0)[2].BG != 4 || c.Row(0, 2)[0].FG != 6 || c.Row(0, 2)[6].FG != 0 {
		t.Fatal("alias ink")
	}
	for _, tc := range []struct{ src, want string }{
		{".def at X\n.enddef\n", "a command's name"},
		{".def a-b X\n.enddef\n", "letters, digits"},
		{".def a X\n.def b Y\n.enddef\n.end\n", ".def inside .def"},
		{".def a X\n$X\n", ".def a without .enddef"},
		{".enddef\n", ".enddef without .def"},
		{".def f A B\n$A $B\n.enddef\n.f one\n", "wants 2 arguments (A B), has 1"},
		{".use marine\n", "unknown command"},
		{".def f X\n.fg $X\n.enddef\n.f puce\n", `unknown color "puce" (default red green yellow blue magenta cyan white) (.f line 1)`},
	} {
		if _, err := Compile(g34, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.src, err, tc.want)
		}
	}
}
