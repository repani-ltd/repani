package raster

import (
	"bytes"
	"strings"
	"testing"
)

func compile(t *testing.T, src string) *Raster {
	t.Helper()
	r, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return r
}

func written(r *Raster) int {
	n := 0
	for i := range r.Rows {
		for _, c := range r.Rows[i] {
			if c != (Cell{}) {
				n++
			}
		}
	}
	return n
}

func TestRows(t *testing.T) {
	r := New()
	if r.Height() != 0 || len(r.Bytes()) != 0 {
		t.Fatal("a new raster is not blank")
	}
	r.Row(5)[3] = Cell{'x', Ink{}}
	if r.Height() != 6 || r.Rows[5][3].Glyph != 'x' || r.Rows[2].end() != 0 {
		t.Fatalf("Row grows: height %d", r.Height())
	}
	if r.Rows[5].end() != 4 {
		t.Fatalf("end = %d, want 4", r.Rows[5].end())
	}
	// A blank cell in ink counts as written; a written space in
	// default ink does not.
	r.Row(6)[9] = Cell{' ', Ink{BG: 4}}
	r.Row(7)[9] = Cell{' ', Ink{}}
	if r.Rows[6].end() != 10 || r.Rows[7].end() != 0 {
		t.Fatalf("ends %d %d", r.Rows[6].end(), r.Rows[7].end())
	}
	// Rows past the slice are blank for every reader.
	if r.Links(50) != nil || string(r.AppendText(nil, 50)) != "" || len(r.AppendANSI(nil, 50)) != 8+40 {
		t.Fatal("rows past the slice")
	}
}

// "RASTER" in yellow at row 3, column 6: six cells, each the glyph
// and the ink, and nothing else written.
func TestVector(t *testing.T) {
	r := compile(t, ".at 3 6\n.fg yellow\nRASTER\n")
	for i, g := range []byte("RASTER") {
		if r.Rows[3][6+i] != (Cell{g, Ink{FG: 3}}) {
			t.Fatalf("cell %d = %+v", i, r.Rows[3][6+i])
		}
	}
	if n := written(r); n != 6 || r.Height() != 4 {
		t.Fatalf("%d written cells, height %d", n, r.Height())
	}
	// The same raster from a leading space at column 5.
	q := compile(t, ".at 3 5\n.fg yellow\n RASTER\n")
	if !bytes.Equal(r.Bytes(), q.Bytes()) {
		t.Fatal("a leading space and .at one column right differ")
	}
	// The bytes: one record, header little-endian with row 3 and
	// length 12, twelve glyphs (the unwritten six are 0x00), twelve
	// inks.
	b := r.Bytes()
	want := append([]byte{byte(3<<6 | 12), byte(3 >> 2)}, 0, 0, 0, 0, 0, 0)
	want = append(want, []byte("RASTER")...)
	want = append(want, 0, 0, 0, 0, 0, 0, 3, 3, 3, 3, 3, 3)
	if !bytes.Equal(b, want) {
		t.Fatalf("bytes = % X\nwant    % X", b, want)
	}
}

func TestBytesAndRead(t *testing.T) {
	src := ".fg red\nALERT\n.fg default\n+ now\n.bg blue\n.fill 1\n.fg white\n.at 1 2\nTITLE\n.fg default\n.bg default\n.at 3 4\nx\n.at 1000\nfar\n"
	r := compile(t, src)
	b := r.Bytes()
	// Rows 0, 1, 3, 1000: row 2 is blank and absent; the bar row is
	// full length; row 1000 is the last record.
	if len(b) != (2+2*9)+(2+2*40)+(2+2*5)+(2+2*3) {
		t.Fatalf("%d bytes", len(b))
	}
	if h := uint16(b[len(b)-8]) | uint16(b[len(b)-7])<<8; h>>6 != 1000 || h&0x3F != 3 {
		t.Fatalf("last header %#04x", h)
	}
	q, err := Read(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, q.Bytes()) || q.Height() != 1001 || q.Rows[1][2] != (Cell{'T', Ink{FG: 7, BG: 4}}) {
		t.Fatal("bytes do not round trip")
	}
	// A stream: any order, a repeat replaces, length 0 clears.
	stream := append([]byte{}, b...)
	two, _ := Compile(".at 1\nTWO\n")
	stream = append(stream, two.Bytes()...)
	stream = append(stream, byte(3<<6), byte(3>>2)) // clear row 3
	s, err := Read(stream)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Text()[1]; got != "TWO" {
		t.Fatalf("replaced row 1 = %q", got)
	}
	if s.Rows[3].end() != 0 || s.Rows[1][10] != (Cell{}) {
		t.Fatal("clear or replace left residue")
	}
	// Errors: cut short, length past 40, a reserved ink bit.
	for _, tc := range []struct {
		name string
		b    []byte
		want string
	}{
		{"header cut", []byte{0x01}, "header cut short"},
		{"row cut", []byte{0x02, 0x00, 'a'}, "cut short"},
		{"length 41", []byte{41, 0x00}, "length 41"},
		{"ink bit", []byte{0x01, 0x00, 'a', 0x80}, "not two palette indices"},
	} {
		if _, err := Read(tc.b); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if r, err := Read(nil); err != nil || r.Height() != 0 {
		t.Fatalf("empty stream: %v", err)
	}
}

func TestInk(t *testing.T) {
	// Every cell carries its own ink: colored text can be glued to
	// text, can start at column 0, and can fill the whole row.
	r := compile(t, "AB\n.fg cyan\n+ CD\n.fg white\n.bg blue\n+ EF\n.fg red\n"+strings.Repeat("x", 40)+"\n")
	row := r.Rows[0]
	if row[1] != (Cell{'B', Ink{}}) || row[3] != (Cell{'C', Ink{FG: 6}}) || row[6] != (Cell{'E', Ink{FG: 7, BG: 4}}) {
		t.Fatalf("row 0 = %+v", row[:8])
	}
	row = r.Rows[1]
	if row[0] != (Cell{'x', Ink{FG: 1, BG: 4}}) || row[39] != (Cell{'x', Ink{FG: 1, BG: 4}}) {
		t.Fatalf("full red row: %+v %+v", row[0], row[39])
	}
	// Bare .fg and .bg are the default.
	r = compile(t, ".fg red\n.bg blue\nA\n.fg\n.bg\nB\n")
	if r.Rows[0][0].Ink != (Ink{FG: 1, BG: 4}) || r.Rows[1][0].Ink != (Ink{}) {
		t.Fatal("bare .fg/.bg")
	}
	// Painting order does not matter: a red word placed before a
	// default one to its right recolors nothing.
	a := compile(t, ".at 0 10\nX\n.fg red\n.at 0\nALERT\n")
	b := compile(t, ".fg red\nALERT\n.fg default\n.at 0 10\nX\n")
	if !bytes.Equal(a.Bytes(), b.Bytes()) || a.Rows[0][10].FG != 0 || a.Rows[0][0].FG != 1 {
		t.Fatal("order changed the raster")
	}
}

func TestFill(t *testing.T) {
	// A bar: spaces in the ink to the edge; text over it inherits the
	// background it is painted in.
	r := compile(t, ".bg blue\n.fill 0\n.at 0 2\nHI\n")
	row := r.Rows[0]
	if row[0] != (Cell{' ', Ink{BG: 4}}) || row[2] != (Cell{'H', Ink{BG: 4}}) || row[39] != (Cell{' ', Ink{BG: 4}}) {
		t.Fatalf("bar row = %+v", row[:4])
	}
	// A partial fill covers exactly its cells.
	r = compile(t, ".bg red\n.fill 5 10 1 4\n")
	if row := r.Rows[5]; row[9] != (Cell{}) || row[10] != (Cell{' ', Ink{BG: 1}}) || row[13] != (Cell{' ', Ink{BG: 1}}) || row[14] != (Cell{}) {
		t.Fatalf("partial fill = %+v", row[8:16])
	}
	// Two rows, column defaults, rows given.
	r = compile(t, ".bg green\n.fill 3 0 2\n")
	if r.Rows[3][0].BG != 2 || r.Rows[4][0].BG != 2 || r.Height() != 5 {
		t.Fatal("two-row fill")
	}
	// A default fill over content clears it.
	r = compile(t, ".fg red\nABC\n.fg default\n.fill 0\n")
	if n := written(r); n != 40 || r.Rows[0][0] != (Cell{' ', Ink{}}) || len(r.Bytes()) != 0 {
		t.Fatalf("clearing fill: %d written, first %+v, %d bytes", n, r.Rows[0][0], len(r.Bytes()))
	}
}

func TestAt(t *testing.T) {
	r := compile(t, ".fg yellow\n  HEAD\n.fg default\nbody\n.at 5 10\nfar\nback\n")
	if r.Rows[0][2].Glyph != 'H' || r.Rows[0][2].FG != 3 || r.Rows[1][0].Glyph != 'b' {
		t.Fatal("leading spaces or the flow to column 0 not honoured")
	}
	if r.Rows[5][10].Glyph != 'f' || r.Rows[6][0].Glyph != 'b' {
		t.Fatal(".at is not one-shot, or does not return to column 0")
	}
	// The blank line flows a row and returns to column 0.
	r = compile(t, ".at 0 5\n\nY\n")
	if r.Rows[1][0].Glyph != 'Y' {
		t.Fatal("blank line after .at")
	}
	// .col places on the row of the last run and leaves the cursor
	// alone: a label at column 0, its value at column 6, the next
	// line below both.
	r = compile(t, ".fg cyan\nWIND\n.fg\n.col 6\nNW 6 kt\nnext\n.col 10\nmore\n")
	if row := r.Rows[0]; row[0].Glyph != 'W' || row[0].FG != 6 || row[6].Glyph != 'N' || row[6].FG != 0 || row[4].Glyph != 0 {
		t.Fatalf(".col row 0 = %q", string(r.AppendText(nil, 0)))
	}
	if row := r.Rows[1]; row[0].Glyph != 'n' || row[10].Glyph != 'm' || r.Height() != 2 {
		t.Fatalf(".col row 1 = %q", string(r.AppendText(nil, 1)))
	}
	// A lone + and a +5 are content.
	r = compile(t, "+\n+5\n")
	if r.Rows[0][0].Glyph != '+' || r.Rows[1][1].Glyph != '5' {
		t.Fatal("+ as content")
	}
	// Rows reach 1023 and no further.
	r = compile(t, ".at 1023\nlast\n")
	if r.Height() != 1024 {
		t.Fatalf("height %d", r.Height())
	}
}

func TestErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{".at 1024 0\n", "outside rows 0..1023"},
		{".at 0 0\n+ X\n", "nothing to continue"},
		{".col 6\nX\n", "no run to attach to"},
		{"A\n.at 1\n.col 6\nX\n", "no run to attach to"},
		{"A\n.col 40\nX\n", "outside columns 0..39"},
		{".bogus\n", "unknown command"},
		{".panel 1\n", "unknown command .panel"},
		{".margin 2\n", "unknown command .margin"},
		{".fg puce\n", "unknown color"},
		{".fg red blue\n", "one color name, or none"},
		{"日本\n", "outside the cell repertoire"},
		{".at 0 36\nHELLO\n", "line 2: raster: row 0: 5 cells at column 36 overflow the row"},
		{".fill 0 0 1 41\n", "outside the raster"},
		{".fill 1023 0 2\n", "outside the raster"},
		{".fill 1 2 3 4 5\n", "too many arguments"},
		{"+ \n", "nothing to continue"},
		{".at 1023\nx\ny\n", "below row 1023"},
	} {
		_, err := Compile(tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.src, err, tc.want)
		}
	}
}

func TestReproducibleAndText(t *testing.T) {
	src := ".fg yellow\nΚΑΙΡΟΣ ─── 12°\n.fg default\n\nΑθήνα   21°\n. dotted\n"
	a := compile(t, src)
	b := compile(t, src)
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("compilation is not reproducible")
	}
	rows := a.Text()
	if len(rows) != 4 || rows[0] != "ΚΑΙΡΟΣ ─── 12°" || rows[1] != "" || rows[2] != "Αθήνα   21°" || rows[3] != ". dotted" {
		t.Fatalf("text = %q", rows)
	}
}

func TestCellTable(t *testing.T) {
	// Round trip for every glyph value; unassigned values render blank.
	glyphs := 0
	for b := range 256 {
		r := CellRune(byte(b))
		if b == 0 || (b >= 0x1B && b <= 0x1F) || (b >= 0x80 && b <= 0x96) || (b >= 0xB2 && b <= 0xBF) {
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
	if glyphs != 213 { // 26 symbols + 95 ASCII + € + 6 typographic + 9 marks + 12 Italian + 64 Greek
		t.Fatalf("%d glyph values, want 213", glyphs)
	}
}

func TestANSI(t *testing.T) {
	r := compile(t, ".fg red\n AB\n.fg default\n\nEF\n")
	a := r.ANSI()
	if len(a) != 3 || a[0] != "\x1b[0m \x1b[31mAB\x1b[39m"+strings.Repeat(" ", 37)+"\x1b[0m" || a[1] != "\x1b[0m"+strings.Repeat(" ", 40)+"\x1b[0m" {
		t.Fatalf("ANSI = %q", a)
	}
}

func TestHTML(t *testing.T) {
	r := compile(t, ".bg blue\n.fill 0 0 1 5\n.fg white\n.at 0 2\n<>\n.fg default\n.bg default\n.at 1\nplain\n")
	rows := r.HTMLRows()
	if want := `<span class="f0 b4">  </span><span class="f7 b4">&lt;&gt; </span>` + strings.Repeat(" ", 35); rows[0] != want {
		t.Fatalf("row 0 = %q, want %q", rows[0], want)
	}
	if want := "plain" + strings.Repeat(" ", 35); rows[1] != want {
		t.Fatalf("row 1 = %q", rows[1])
	}
	doc := HTMLDocument([]*Raster{r, r, r}, 2, "t<t", Teletext)
	if !strings.Contains(doc, "<title>t&lt;t</title>") || strings.Count(doc, "<pre>") != 3 || !strings.Contains(doc, "repeat(2, max-content)") {
		t.Fatal("document shape")
	}
}

func TestSpec(t *testing.T) {
	s := Spec()
	for _, want := range []string{"# Rows", "# Cells", "# Ink", "# Authoring", "# Aliases"} {
		if !strings.Contains(s, want) {
			t.Errorf("spec missing %q", want)
		}
	}
	// The Authoring example is a raster: it compiles, with its red
	// ALERT at column 0 of its row.
	a := strings.Index(s, "    .rem A notice:")
	b := strings.Index(s[a:], ".end")
	var src strings.Builder
	for _, l := range strings.Split(s[a:a+b], "\n") {
		src.WriteString(strings.TrimPrefix(l, "    ") + "\n")
	}
	r, err := Compile(src.String())
	if err != nil {
		t.Fatalf("spec example: %v", err)
	}
	if row := r.Rows[7]; row[0] != (Cell{'A', Ink{FG: 1}}) || row[6] != (Cell{'n', Ink{}}) {
		t.Fatalf("ALERT row = %+v", row[:8])
	}
	if rows := r.Text(); rows[0] != "HARBOUR NOTICE · 02 SEP" || rows[6] != "FUEL    06:00-14:00, south quay" {
		t.Fatalf("spec example text = %q", rows[:8])
	}
	if l := r.Links(10); len(l) != 1 || l[0] != (Link{Col: 4, Len: 7, Target: "tides"}) {
		t.Fatalf("spec example links = %+v", l)
	}
}

func TestLinks(t *testing.T) {
	r := compile(t, "Tap [close] or [tide tables]\n[] [x\n.fg red\n[ALERT] now\nno]link[\n")
	want := []Link{{4, 7, "close"}, {15, 13, "tide tables"}}
	got := r.Links(0)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("links = %+v, want %+v", got, want)
	}
	// An empty pair is no link; an unclosed bracket ends the search.
	if l := r.Links(1); len(l) != 0 {
		t.Fatalf("row 1 links = %+v", l)
	}
	// A link in ink at column 0.
	if l := r.Links(2); len(l) != 1 || l[0].Target != "ALERT" || r.Rows[2][0].FG != 1 {
		t.Fatalf("row 2 links = %+v", l)
	}
	if l := r.Links(3); len(l) != 0 {
		t.Fatalf("row 3 links = %+v", l)
	}
	// Links survive the text renderer as typed and become anchors in
	// HTML, wrapping the span with its ink inside.
	if rows := r.Text(); rows[0] != "Tap [close] or [tide tables]" {
		t.Fatalf("text = %q", rows[0])
	}
	h := r.HTMLRows()
	if !strings.HasPrefix(h[0], `Tap <a href="#close">[close]</a> or <a href="#tide tables">[tide tables]</a>`) {
		t.Fatalf("html row 0 = %q", h[0])
	}
	if !strings.HasPrefix(h[2], `<a href="#ALERT"><span class="f1 b0">[ALERT]</span></a> <span class="f1 b0">now`) {
		t.Fatalf("html row 2 = %q", h[2])
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
	if s := JS(); !strings.Contains(s, "export function read(") || !strings.Contains(s, "export function paint(") {
		t.Fatal("JS() is not the reader")
	}
}

func TestAliases(t *testing.T) {
	vocab := ".def bar\n.fg white\n.bg blue\n.fill\n$bar\n.enddef\n" +
		".def label\n.fg cyan\n$label\n.enddef\n" +
		".def rule\n────────\n.enddef\n" +
		".def t\n$t\n.enddef\n" +
		".def twice\n$twice $twice! $twicer $\n.enddef\n"
	r := compile(t, vocab+".bar HARBOUR · 02 SEP\n.at 2\n.label TEMP\n.col 6\n31°C  dew 11°C\n.rule\n.twice ab\nplain $x\n.bar\n")
	rows := r.Text()
	want := []string{"HARBOUR · 02 SEP", "", "TEMP  31°C  dew 11°C", "────────", "ab ab! $twicer $", "plain $x", ""}
	if strings.Join(rows, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %q, want %q", rows, want)
	}
	if r.Rows[0][0].FG != 7 || r.Rows[0][0].BG != 4 || r.Rows[2][0].FG != 6 || r.Rows[2][6].FG != 0 || r.Rows[6][39].BG != 4 {
		t.Fatal("alias ink")
	}
	// Hygiene: a text that looks like a command or a continuation is
	// painted, not obeyed; a slot inside a continuation too.
	r = compile(t, vocab+".def c\nA\n+ $c\n.enddef\n.t .fg red\n.t + not a continuation\n.c .bg blue\n.t   two leading spaces\n")
	if rows := r.Text(); rows[0] != ".fg red" || rows[1] != "+ not a continuation" || rows[2] != "A .bg blue" || rows[3] != "  two leading spaces" || r.Rows[0][1].FG != 0 || r.Rows[2][3].BG != 0 {
		t.Fatalf("hygiene: %q", rows)
	}
	// The pen is the caller's: a body sets it, the use restores it, and
	// text after a use is in the caller's ink. A body is relative: the
	// same use at another row is the same rows shifted.
	r = compile(t, vocab+".fg green\n.label X\nafter\n.at 5\n.bar B\nnext\n")
	if r.Rows[0][0].FG != 6 || r.Rows[1][0].FG != 2 || r.Rows[5][0].BG != 4 || r.Rows[6][0].Glyph != 'n' || r.Rows[6][0].BG != 0 {
		t.Fatal("pen not restored, or body not relative")
	}
	for _, tc := range []struct{ src, want string }{
		{".def at\n.enddef\n", "a command's name"},
		{".def a-b\n.enddef\n", "letters, digits"},
		{".def a b\n.enddef\n", "a name and nothing else"},
		{".def a\n.def b\n.enddef\n.enddef\n", ".def inside .def"},
		{".def a\n$a\n", ".def a without .enddef"},
		{".enddef\n", ".enddef without .def"},
		{".def a\n$a\n.enddef\n.def a\n.enddef\n", "already defined"},
		{".def a\nx\n.enddef\n.a text\n", ".a takes no text"},
		{".def a\n.at 3\n.enddef\n", ".at inside an alias"},
		{".def a\n.fill 3\n.enddef\n", ".fill takes no arguments inside an alias"},
		{".def a\n$a\n.enddef\n.def b\n.a $b\n.enddef\n", "an alias inside an alias"},
		{".def a\n.fg $a\n.enddef\n", "$a in a command"},
		{".def a\n.bogus\n.enddef\n", "unknown command .bogus"},
		{".use marine\n", "unknown command"},
		{".def f\n.fg puce\n.enddef\n.f\n", `unknown color "puce" (default red green yellow blue magenta cyan white) (.f line 1)`},
		{".def f\n$f\n.enddef\n.at 0 38\n.f abc\n", "overflow the row (.f line 1)"},
	} {
		if _, err := Compile(tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
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
	r := compile(t, ".fg red\nX\n")
	if doc := HTMLDocument([]*Raster{r}, 1, "t", TeletextLight); !strings.Contains(doc, "--c1: #b3271b") || !strings.Contains(doc, `<span class="f1 b0">X`) {
		t.Fatal("teletext-light document")
	}
	if _, ok := Themes["teletext-light"]; !ok || len(Themes) != 2 {
		t.Fatalf("themes = %v", Themes)
	}
	// Every document carries the embedded face.
	if doc := HTMLDocument([]*Raster{r}, 1, "t", Teletext); !strings.Contains(doc, `@font-face { font-family: "JuliaMono"; font-weight: 400;`) {
		t.Error("document lacks the embedded face")
	}
}
