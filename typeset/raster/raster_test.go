package raster

import (
	"bytes"
	"strings"
	"testing"
)

// A 40 by 24 by 4 geometry; the tests that fix the cell model run on
// it, and TestGeometryIsAParameter on others.
var g40 = Geometry{Cols: 40, Rows: 24, Panels: 4}

func compile(t *testing.T, src string) *Page {
	t.Helper()
	p, err := Compile(g40, src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return p
}

func written(p *Page) int {
	n := 0
	for _, c := range p.Cells {
		if c != (Cell{}) {
			n++
		}
	}
	return n
}

func TestGeometry(t *testing.T) {
	if g40.PanelLen() != 960 || g40.Len() != 3840 || g40.Size() != 7680 || g40.Offset(2, 3, 5) != 2045 {
		t.Fatalf("geometry: panel %d page %d size %d offset %d", g40.PanelLen(), g40.Len(), g40.Size(), g40.Offset(2, 3, 5))
	}
	g := Geometry{Cols: 40, Rows: 10, Panels: 3}
	if g.Len() != 1200 || g.Offset(1, 2, 3) != 483 {
		t.Fatalf("40x10x3: len %d offset %d", g.Len(), g.Offset(1, 2, 3))
	}
	p := New(g)
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
	if r := p.Row(1, 2); r[31] != (Cell{'A', Ink{FG: 1}}) || r[39] != (Cell{'I', Ink{FG: 1}}) {
		t.Fatalf("row = %+v", r[28:])
	}
	if _, err := Compile(Geometry{34, 28, 4}, ".panel 1\n.at 2 31\n.fg red\nABCDEFGHI\n"); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("34 columns accepted a 9-cell run at 31: %v", err)
	}
	for _, tc := range []struct{ src, want string }{
		{".panel 2\n", "panel 2 out of range 0..1"},
		{".at 3\n", "outside rows 0..2, cols 0..39"},
		{"\n\n\nx\n", "below row 2"},
		{".fill 0 0 1 41\n", "outside the panel"},
		{".margin 2\n", "unknown command .margin"},
	} {
		if _, err := Compile(g, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.src, err, tc.want)
		}
	}
	// Single-column, single-row, single-panel is a page too, and the
	// page starts in panel 0.
	if p, err := Compile(Geometry{1, 1, 1}, "X\n"); err != nil || p.Cells[0].Glyph != 'X' {
		t.Fatalf("1x1x1: %v %v", p, err)
	}
}

// "RASTER" in yellow at panel 2, row 3, column 6: six cells, each the
// glyph and the ink, and nothing else on the page.
func TestVector(t *testing.T) {
	p := compile(t, ".panel 2\n.at 3 6\n.fg yellow\nRASTER\n")
	o := g40.Offset(2, 3, 6)
	for i, g := range []byte("RASTER") {
		if p.Cells[o+i] != (Cell{g, Ink{FG: 3}}) {
			t.Fatalf("cell %d = %+v", i, p.Cells[o+i])
		}
	}
	if n := written(p); n != 6 {
		t.Fatalf("%d written cells, want 6", n)
	}
	// The same page from a leading space at column 5.
	q := compile(t, ".panel 2\n.at 3 5\n.fg yellow\n RASTER\n")
	if !bytes.Equal(p.Bytes(), q.Bytes()) {
		t.Fatal("a leading space and .at one column right differ")
	}
	// The bytes: glyph then ink, background high, foreground low.
	b := p.Bytes()
	if len(b) != g40.Size() || b[2*o] != 'R' || b[2*o+1] != 0x03 {
		t.Fatalf("bytes at %d: % X", 2*o, b[2*o:2*o+2])
	}
}

func TestBytesRoundTrip(t *testing.T) {
	src := ".fg red\nALERT\n.fg default\n+ now\n.bg blue\n.fill 1\n.fg white\n.at 1 2\nTITLE\n.fg default\n.bg default\n.at 3 4\nx\n"
	p := compile(t, src)
	b := p.Bytes()
	q, err := Of(g40, b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, q.Bytes()) || q.Cells[g40.Offset(0, 1, 2)] != (Cell{'T', Ink{FG: 7, BG: 4}}) {
		t.Fatal("bytes do not round trip")
	}
	if _, err := Of(g40, b[:len(b)-1]); err == nil || !strings.Contains(err.Error(), "bytes for geometry") {
		t.Fatalf("short bytes: %v", err)
	}
	b[1] = 0x80
	if _, err := Of(g40, b); err == nil || !strings.Contains(err.Error(), "not two palette indices") {
		t.Fatalf("reserved bit: %v", err)
	}
	if _, err := Of(g40, make([]byte, g40.Size())); err != nil {
		t.Fatalf("blank page: %v", err)
	}
}

func TestInk(t *testing.T) {
	// Every cell carries its own ink: colored text can be glued to
	// text, can start at column 0, and can fill the whole row.
	p := compile(t, "AB\n.fg cyan\n+ CD\n.fg white\n.bg blue\n+ EF\n.fg red\n"+strings.Repeat("x", 40)+"\n")
	r := p.Row(0, 0)
	if r[1] != (Cell{'B', Ink{}}) || r[3] != (Cell{'C', Ink{FG: 6}}) || r[6] != (Cell{'E', Ink{FG: 7, BG: 4}}) {
		t.Fatalf("row 0 = %+v", r[:8])
	}
	r = p.Row(0, 1)
	if r[0] != (Cell{'x', Ink{FG: 1, BG: 4}}) || r[39] != (Cell{'x', Ink{FG: 1, BG: 4}}) {
		t.Fatalf("full red row: %+v %+v", r[0], r[39])
	}
	// Bare .fg and .bg are the default.
	p = compile(t, ".fg red\n.bg blue\nA\n.fg\n.bg\nB\n")
	if p.Row(0, 0)[0].Ink != (Ink{FG: 1, BG: 4}) || p.Row(0, 1)[0].Ink != (Ink{}) {
		t.Fatal("bare .fg/.bg")
	}
	// Painting order does not matter: a red word placed before a
	// default one to its right recolors nothing.
	a := compile(t, ".at 0 10\nX\n.fg red\n.at 0\nALERT\n")
	b := compile(t, ".fg red\nALERT\n.fg default\n.at 0 10\nX\n")
	if !bytes.Equal(a.Bytes(), b.Bytes()) || a.Row(0, 0)[10].FG != 0 || a.Row(0, 0)[0].FG != 1 {
		t.Fatal("order changed the page")
	}
}

func TestFill(t *testing.T) {
	// A bar: spaces in the ink to the edge; text over it inherits the
	// background it is painted in.
	p := compile(t, ".bg blue\n.fill 0\n.at 0 2\nHI\n")
	r := p.Row(0, 0)
	if r[0] != (Cell{' ', Ink{BG: 4}}) || r[2] != (Cell{'H', Ink{BG: 4}}) || r[39] != (Cell{' ', Ink{BG: 4}}) {
		t.Fatalf("bar row = %+v", r[:4])
	}
	// A partial fill covers exactly its cells.
	p = compile(t, ".bg red\n.fill 5 10 1 4\n")
	if r := p.Row(0, 5); r[9] != (Cell{}) || r[10] != (Cell{' ', Ink{BG: 1}}) || r[13] != (Cell{' ', Ink{BG: 1}}) || r[14] != (Cell{}) {
		t.Fatalf("partial fill = %+v", r[8:16])
	}
	// Two rows, column defaults, rows given.
	p = compile(t, ".bg green\n.fill 3 0 2\n")
	if p.Row(0, 3)[0].BG != 2 || p.Row(0, 4)[0].BG != 2 || p.Row(0, 5)[0] != (Cell{}) {
		t.Fatal("two-row fill")
	}
	// A default fill over content clears it.
	p = compile(t, ".fg red\nABC\n.fg default\n.fill 0\n")
	if n := written(p); n != 40 || p.Row(0, 0)[0] != (Cell{' ', Ink{}}) {
		t.Fatalf("clearing fill: %d written, first %+v", n, p.Row(0, 0)[0])
	}
}

func TestAt(t *testing.T) {
	p := compile(t, ".fg yellow\n  HEAD\n.fg default\nbody\n.at 5 10\nfar\nback\n")
	if p.Row(0, 0)[2].Glyph != 'H' || p.Row(0, 0)[2].FG != 3 || p.Row(0, 1)[0].Glyph != 'b' {
		t.Fatal("leading spaces or the flow to column 0 not honoured")
	}
	if p.Row(0, 5)[10].Glyph != 'f' || p.Row(0, 6)[0].Glyph != 'b' {
		t.Fatal(".at is not one-shot, or does not return to column 0")
	}
	// .panel moves only the cursor: the pen persists.
	p = compile(t, ".fg red\n.at 3 1\n.panel 1\nX\n")
	if x := p.Row(1, 0)[0]; x.Glyph != 'X' || x.FG != 1 {
		t.Fatalf("after .panel: %+v", x)
	}
	// The blank line flows a row and returns to column 0.
	p = compile(t, ".at 0 5\n\nY\n")
	if p.Row(0, 1)[0].Glyph != 'Y' {
		t.Fatal("blank line after .at")
	}
	// .col places on the row of the last run and leaves the cursor
	// alone: a label at column 0, its value at column 6, the next
	// line below both.
	p = compile(t, ".fg cyan\nWIND\n.fg\n.col 6\nNW 6 kt\nnext\n.col 10\nmore\n")
	if r := p.Row(0, 0); r[0].Glyph != 'W' || r[0].FG != 6 || r[6].Glyph != 'N' || r[6].FG != 0 || r[4].Glyph != 0 {
		t.Fatalf(".col row 0 = %q", string(p.AppendText(nil, 0, 0)))
	}
	if r := p.Row(0, 1); r[0].Glyph != 'n' || r[10].Glyph != 'm' || p.Row(0, 2)[0].Glyph != 0 {
		t.Fatalf(".col row 1 = %q", string(p.AppendText(nil, 0, 1)))
	}
	// A lone + and a +5 are content.
	p = compile(t, "+\n+5\n")
	if p.Row(0, 0)[0].Glyph != '+' || p.Row(0, 1)[1].Glyph != '5' {
		t.Fatal("+ as content")
	}
}

func TestErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{".panel 4\n", "panel 4 out of range"},
		{".at 24 0\n", "outside rows"},
		{".at 0 0\n+ X\n", "nothing to continue"},
		{".col 6\nX\n", "no run to attach to"},
		{"A\n.at 1\n.col 6\nX\n", "no run to attach to"},
		{"A\n.col 40\nX\n", "outside columns 0..39"},
		{".bogus\n", "unknown command"},
		{".fg puce\n", "unknown color"},
		{".fg red blue\n", "one color name, or none"},
		{".ink red\n", "unknown command"},
		{"日本\n", "outside the cell repertoire"},
		{".at 0 36\nHELLO\n", "line 2: raster: row 0: 5 cells at column 36 overflow the row"},
		{".fill 0 0 1 41\n", "outside the panel"},
		{".fill\n", "want 1 to 4 arguments"},
		{"+ \n", "nothing to continue"},
		{strings.Repeat("x\n", 25), "below row 23"},
	} {
		_, err := Compile(g40, tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.src, err, tc.want)
		}
	}
}

func TestReproducibleAndText(t *testing.T) {
	src := ".panel 1\n.fg yellow\nΚΑΙΡΟΣ ─── 12°\n.fg default\n\nΑθήνα   21°\n. dotted\n"
	a := compile(t, src)
	b := compile(t, src)
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("compilation is not reproducible")
	}
	rows := a.Text(1)
	if rows[0] != "ΚΑΙΡΟΣ ─── 12°" || rows[1] != "" || rows[2] != "Αθήνα   21°" || rows[3] != ". dotted" {
		t.Fatalf("text = %q", rows[:4])
	}
	if len(rows) != g40.Rows || a.Text(0)[0] != "" {
		t.Fatalf("text shape: %d rows, panel 0 row 0 %q", len(rows), a.Text(0)[0])
	}
}

func TestCellTable(t *testing.T) {
	// Round trip for every glyph value; unassigned values render blank.
	glyphs := 0
	for b := range 256 {
		r := CellRune(byte(b))
		if b == 0 || (b >= 0x11 && b <= 0x1F) || (b >= 0x80 && b <= 0x8F) || (b >= 0xA6 && b <= 0xBF) {
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
	if a := p.ANSI(0); a[0] != "\x1b[0m \x1b[31mAB\x1b[39m   \x1b[0m" {
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
	if want := `<span class="f0 b4">  </span><span class="f7 b4">&lt;&gt; </span>` + strings.Repeat(" ", 35); rows[0] != want {
		t.Fatalf("row 0 = %q, want %q", rows[0], want)
	}
	if want := "plain" + strings.Repeat(" ", 35); rows[1] != want {
		t.Fatalf("row 1 = %q", rows[1])
	}
	doc := HTMLDocument(p, 2, "t<t", Teletext)
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
	// The Authoring example is a page: it compiles on the 40-column
	// geometry, with its red ALERT at column 0 of its row.
	a := strings.Index(s, "    .rem A notice:")
	b := strings.Index(s[a:], ".end")
	var src strings.Builder
	for _, l := range strings.Split(s[a:a+b], "\n") {
		src.WriteString(strings.TrimPrefix(l, "    ") + "\n")
	}
	p, err := Compile(g40, src.String())
	if err != nil {
		t.Fatalf("spec example: %v", err)
	}
	if r := p.Row(0, 7); r[0] != (Cell{'A', Ink{FG: 1}}) || r[6] != (Cell{'n', Ink{}}) {
		t.Fatalf("ALERT row = %+v", r[:8])
	}
	if rows := p.Text(0); rows[0] != "HARBOUR NOTICE · 02 SEP" || rows[6] != "FUEL    06:00-14:00, south quay" {
		t.Fatalf("spec example text = %q", rows[:8])
	}
	if l := p.Links(0, 10); len(l) != 1 || l[0] != (Link{Col: 4, Len: 7, Target: "tides"}) {
		t.Fatalf("spec example links = %+v", l)
	}
}

func TestLinks(t *testing.T) {
	p := compile(t, "Tap [close] or [tide tables]\n[] [x\n.fg red\n[ALERT] now\nno]link[\n")
	want := []Link{{4, 7, "close"}, {15, 13, "tide tables"}}
	got := p.Links(0, 0)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("links = %+v, want %+v", got, want)
	}
	// An empty pair is no link; an unclosed bracket ends the search.
	if l := p.Links(0, 1); len(l) != 0 {
		t.Fatalf("row 1 links = %+v", l)
	}
	// A link in ink at column 0.
	if l := p.Links(0, 2); len(l) != 1 || l[0].Target != "ALERT" || p.Row(0, 2)[0].FG != 1 {
		t.Fatalf("row 2 links = %+v", l)
	}
	if l := p.Links(0, 3); len(l) != 0 {
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

func TestPageReuse(t *testing.T) {
	// A page compiled twice is exactly the second page, and the
	// append renderers give the same rows as the allocating ones.
	p := New(g40)
	if err := p.Compile(".fg red\nALERT\n.bg blue\n.fill 3\n"); err != nil {
		t.Fatal(err)
	}
	if err := p.Compile(".at 1 3\nquiet\n"); err != nil {
		t.Fatal(err)
	}
	want := compile(t, ".at 1 3\nquiet\n")
	if !bytes.Equal(p.Bytes(), want.Bytes()) {
		t.Fatal("a reused page kept something")
	}
	if got, want := string(p.AppendANSI(nil, 0, 1)), want.ANSI(0)[1]; got != want {
		t.Fatalf("AppendANSI %q, ANSI %q", got, want)
	}
	if got := string(p.AppendText(nil, 0, 1)); got != "   quiet" {
		t.Fatalf("AppendText %q", got)
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
		t.Fatal("JS() is not the reader")
	}
}

func TestAliases(t *testing.T) {
	vocab := ".def bar TITLE\n.fg white\n.bg blue\n.fill 0\n.at 0\n$TITLE\n.fg\n.bg\n.enddef\n" +
		".def field LABEL VALUE\n.fg cyan\n$LABEL\n.fg\n.col 6\n$VALUE\n.enddef\n" +
		".def wind SPEED\n.field WIND NW $SPEED kt\n.enddef\n"
	p := compile(t, vocab+".bar HARBOUR · 02 SEP\n.at 2\n.field TEMP 31°C  dew 11°C\n.wind 18\nplain $x\n")
	rows := p.Text(0)
	if rows[0] != "HARBOUR · 02 SEP" || rows[2] != "TEMP  31°C  dew 11°C" || rows[3] != "WIND  NW 18 kt" || rows[4] != "plain $x" {
		t.Fatalf("rows = %q", rows[:5])
	}
	if p.Row(0, 0)[0].FG != 7 || p.Row(0, 0)[0].BG != 4 || p.Row(0, 2)[0].FG != 6 || p.Row(0, 2)[6].FG != 0 {
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
		if _, err := Compile(g40, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.src, err, tc.want)
		}
	}
}

func TestThemes(t *testing.T) {
	for name, th := range Themes {
		if th.Name != name {
			t.Errorf("theme %q is named %q", name, th.Name)
		}
		for i := range 8 {
			for _, c := range []string{th.FG[i], th.BG[i]} {
				if len(c) != 7 || c[0] != '#' {
					t.Errorf("%s: colour %q", name, c)
				}
			}
		}
		css := th.CSS()
		for _, want := range []string{"--c0: " + th.FG[0], "--g7: " + th.BG[7], ".f7 { color: var(--c7) }", ".b7 { background: var(--g7); color: var(--ground) }"} {
			if !strings.Contains(css, want) {
				t.Errorf("%s: css lacks %q", name, want)
			}
		}
	}
	p := compile(t, ".fg red\nX\n")
	if doc := HTMLDocument(p, 1, "t", TeletextLight); !strings.Contains(doc, "--c1: #b3271b") || !strings.Contains(doc, `<span class="f1 b0">X`) {
		t.Fatal("teletext-light document")
	}
	if _, ok := Themes["teletext-light"]; !ok || len(Themes) != 2 {
		t.Fatalf("themes = %v", Themes)
	}
	// Every document carries the embedded face.
	if doc := HTMLDocument(p, 1, "t", Teletext); !strings.Contains(doc, `@font-face { font-family: "JuliaMono"; font-weight: 400;`) {
		t.Error("document lacks the embedded face")
	}
}
