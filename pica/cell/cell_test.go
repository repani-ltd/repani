package cell

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"

	"repani.com/pica"
	"repani.com/typeset/raster"
)

var update = flag.Bool("update", false, "rewrite the known-answer fixtures")

func parse(t *testing.T, src string) *pica.Doc {
	t.Helper()
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// text is one row of the page as runes, blanks as spaces.
func text(c *raster.Canvas, panel, row int) string {
	var b strings.Builder
	for _, cl := range c.Row(panel, row) {
		g := raster.CellRune(cl.Glyph)
		if g == 0 {
			g = ' '
		}
		b.WriteRune(g)
	}
	return strings.TrimRight(b.String(), " ")
}

// find returns the panel and row whose text begins with prefix.
func find(t *testing.T, c *raster.Canvas, prefix string) (int, int) {
	t.Helper()
	for p := 0; p < c.Panels; p++ {
		for r := 0; r < c.Rows; r++ {
			if strings.HasPrefix(text(c, p, r), prefix) {
				return p, r
			}
		}
	}
	t.Fatalf("no row begins %q", prefix)
	return 0, 0
}

// The harbour example (a tessera notice as a pica document) on a
// four-panel page: the known answer is the page's bytes, and the
// inks land where the vocabulary says.
func TestHarbour(t *testing.T) {
	src, err := os.ReadFile("../example/harbour.t")
	if err != nil {
		t.Fatal(err)
	}
	doc := parse(t, string(src))
	// The document's tables are sized for .width 34; the margin
	// cell makes the measure Cols-1.
	l := Layout{Geometry: raster.Geometry{Cols: 35, Rows: 28, Panels: 4}}
	r, err := Render(doc, l, "")
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		os.WriteFile("testdata/harbour.bin", r.Page.Cells, 0o644)
	}
	want, err := os.ReadFile("testdata/harbour.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r.Page.Cells, want) {
		t.Error("page differs from testdata/harbour.bin (run with -update to accept)")
	}
	c := raster.Decode(r.Page)
	ink := func(panel, row, col int) raster.Ink { return c.Row(panel, row)[col].Ink }

	// The title bar: white on blue the whole row, the title centered.
	if got := strings.TrimSpace(text(c, 0, 0)); got != doc.Title {
		t.Errorf("title row %q", got)
	}
	first := strings.IndexFunc(text(c, 0, 0), func(r rune) bool { return r != ' ' })
	if ink(0, 0, first) != (raster.Ink{FG: 7, BG: 4}) || ink(0, 0, 0).BG != 4 || ink(0, 0, 34).BG != 4 {
		t.Errorf("title ink %v %v %v", ink(0, 0, 0), ink(0, 0, first), ink(0, 0, 34))
	}
	// Byline on row 1, blank, a rule across the columns, blank, then
	// the first heading at the margin, yellow; a subheading cyan.
	if !strings.HasPrefix(strings.TrimSpace(text(c, 0, 1)), "by Kea Port Authority") || text(c, 0, 2) != "" ||
		text(c, 0, 3) != " "+strings.Repeat("─", 34) || text(c, 0, 4) != "" || !strings.HasPrefix(text(c, 0, 5), " Today") {
		t.Errorf("masthead rows %q / %q / %q / %q / %q", text(c, 0, 1), text(c, 0, 2), text(c, 0, 3), text(c, 0, 4), text(c, 0, 5))
	}
	p, row := find(t, c, " Today")
	if ink(p, row, 1) != (raster.Ink{FG: 3}) {
		t.Errorf("heading ink %v", ink(p, row, 1))
	}
	p, row = find(t, c, " Meltemi tonight")
	if ink(p, row, 1) != (raster.Ink{FG: 6}) {
		t.Errorf("subheading ink %v", ink(p, row, 1))
	}
	// A term: the label cyan, the text default after the gap.
	p, row = find(t, c, " Fuel  06:00")
	if ink(p, row, 1) != (raster.Ink{FG: 6}) || ink(p, row, 7) != (raster.Ink{}) {
		t.Errorf("term inks %v %v", ink(p, row, 1), ink(p, row, 7))
	}
	// Emphasis: _not_ is white, its markers blank, the text around
	// it default.
	p, row = find(t, c, " Vourkari bay is")
	line := text(c, p, row)
	i := strings.Index(line, "not")
	if i < 0 || line[i-1] != ' ' || line[i+3] != ' ' {
		t.Fatalf("emphasis row %q", line)
	}
	if ink(p, row, i) != (raster.Ink{FG: 7}) || ink(p, row, i-2) != (raster.Ink{}) || ink(p, row, i+5) != (raster.Ink{}) {
		t.Errorf("emphasis inks %v %v %v", ink(p, row, i-2), ink(p, row, i), ink(p, row, i+5))
	}
	// A table: the header cyan, the separator in the rule glyph, a
	// row default; the heading above it kept with it.
	p, row = find(t, c, " Dep    To")
	if ink(p, row, 1) != (raster.Ink{FG: 6}) || !strings.HasPrefix(text(c, p, row+1), " ──────") || ink(p, row+2, 1) != (raster.Ink{}) {
		t.Errorf("table rows %q %q", text(c, p, row+1), text(c, p, row+2))
	}
	if text(c, p, row-2) != " Ferries · Δρομολόγια" {
		t.Errorf("heading not kept with its table: %q", text(c, p, row-2))
	}
	// The rights notice ends the flow.
	find(t, c, " Λιμεναρχείο Κέας · hourly")
	if len(r.Links) != 0 {
		t.Errorf("links %v", r.Links)
	}
	// Reproducible.
	again, _ := Render(doc, l, "")
	if again.Source != r.Source || !bytes.Equal(again.Page.Cells, r.Page.Cells) {
		t.Error("render is not reproducible")
	}
}

// Two columns per panel: the second column starts after the first
// and a gutter, its headings are inked there, and the documents
// that cannot fit are errors.
func TestColumns(t *testing.T) {
	src, _ := os.ReadFile("../example/harbour.t")
	doc := parse(t, string(src))
	l := Layout{Geometry: raster.Geometry{Cols: 72, Rows: 26, Panels: 2}, Columns: 2}
	r, err := Render(doc, l, "")
	if err != nil {
		t.Fatal(err)
	}
	c := raster.Decode(r.Page)
	if !strings.HasPrefix(text(c, 0, 5), " Today") {
		t.Errorf("row 5: %q", text(c, 0, 5))
	}
	// Column 2 begins at 1+35+1 = 37, after a blank gutter cell
	// that carries its code; a subheading there is cyan while the
	// row's column-1 text keeps its own ink.
	found := false
	for row := 0; row < 26; row++ {
		cells := c.Row(0, row)
		if line := text(c, 0, row); len(line) > 37 && strings.HasPrefix(line[37:], "Fees per night") {
			found = true
			if cells[36].Glyph != 0 || cells[37].Ink != (raster.Ink{FG: 6}) {
				t.Errorf("row %d: gutter %v, subheading ink %v", row, cells[36], cells[37].Ink)
			}
			if strings.HasPrefix(line, " Today") && cells[1].Ink != (raster.Ink{FG: 3}) {
				t.Errorf("row %d: column-1 heading ink %v", row, cells[1].Ink)
			}
		}
	}
	if !found {
		t.Error("no heading at column 37")
	}
	if _, err := Render(doc, Layout{Geometry: raster.Geometry{Cols: 16, Rows: 24, Panels: 1}, Columns: 2}, ""); err == nil || !strings.Contains(err.Error(), "measure") {
		t.Errorf("narrow: %v", err)
	}
	if _, err := Render(doc, Layout{Geometry: raster.Geometry{Cols: 35, Rows: 28, Panels: 1}}, ""); err == nil || !strings.Contains(err.Error(), "do not fit") {
		t.Errorf("overflow: %v", err)
	}
}

// Content that would lex as a raster command or continuation is
// painted as written; a .link is bracketed and reported; a rule is
// the rule glyph across the measure; a custom vocabulary inks.
func TestEscapesLinksVocabulary(t *testing.T) {
	doc := parse(t, `A test
.pre
.item not a command
+ not a continuation
..dots
.
plain
.end

.link https://repani.com Repani

---

# Head
.width 40
`)
	l := Layout{Geometry: raster.Geometry{Cols: 24, Rows: 16, Panels: 1}}
	r, err := Render(doc, l, "")
	if err != nil {
		t.Fatal(err)
	}
	c := raster.Decode(r.Page)
	// No byline: the title, a blank, the rule, a blank, then
	// content from row 4.
	for i, want := range []string{" .item not a command", " + not a continuation", " ..dots", " .", " plain", "", " [Repani]", "", " " + strings.Repeat("─", 23), "", " Head"} {
		if got := text(c, 0, 4+i); got != want {
			t.Errorf("row %d: %q, want %q", 4+i, got, want)
		}
	}
	if len(r.Links) != 1 || r.Links[0] != (Link{Text: "Repani", URL: "https://repani.com"}) {
		t.Errorf("links %v", r.Links)
	}
	if links := c.Links(0, 10); len(links) != 1 || links[0].Target != "Repani" {
		t.Errorf("page links %v", links)
	}
	vocab := strings.Replace(Vocabulary, ".def heading TEXT\n.fg yellow", ".def heading TEXT\n.fg red", 1)
	r2, err := Render(doc, l, vocab)
	if err != nil {
		t.Fatal(err)
	}
	c2 := raster.Decode(r2.Page)
	if ink := c2.Row(0, 14)[1].Ink; ink != (raster.Ink{FG: 1}) {
		t.Errorf("custom heading ink %v", ink)
	}
	// A rune outside the repertoire is an error, never a substitution.
	if _, err := Render(parse(t, "T\n\nsection § 3\n"), l, ""); err == nil {
		t.Error("rune outside the repertoire accepted")
	}
}
