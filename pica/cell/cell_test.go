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

// text is one row of a raster as runes, blanks as spaces, trimmed.
func text(r *raster.Raster, row int) string {
	if row >= r.Height() {
		return ""
	}
	var b strings.Builder
	for _, cl := range r.Rows[row] {
		b.WriteRune(raster.CellRune(cl.Glyph))
	}
	return strings.TrimRight(b.String(), " ")
}

// find returns the screen and row whose text begins with prefix.
func find(t *testing.T, res *Result, prefix string) (int, int) {
	t.Helper()
	for s, r := range res.Rasters {
		for row := range r.Height() {
			if strings.HasPrefix(text(r, row), prefix) {
				return s, row
			}
		}
	}
	t.Fatalf("no row begins %q", prefix)
	return 0, 0
}

// allBytes is every screen's bytes back to back.
func allBytes(res *Result) []byte {
	var out []byte
	for _, r := range res.Rasters {
		out = append(out, r.Bytes()...)
	}
	return out
}

// The harbour example (a notice board as a pica document) on screens
// of 28 rows: the known answer is the screens' bytes, and the inks
// land where the vocabulary says.
func TestHarbour(t *testing.T) {
	src, err := os.ReadFile("../example/harbour.t")
	if err != nil {
		t.Fatal(err)
	}
	doc := parse(t, string(src))
	l := Layout{Rows: 28, Screens: 4}
	res, err := Render(doc, l, "")
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		os.WriteFile("testdata/harbour.bin", allBytes(res), 0o644)
	}
	want, err := os.ReadFile("testdata/harbour.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(allBytes(res), want) {
		t.Error("screens differ from testdata/harbour.bin (run with -update to accept)")
	}
	first := res.Rasters[0]
	ink := func(s, row, col int) raster.Ink { return res.Rasters[s].Rows[row][col].Ink }

	// The title bar: white on blue the whole row, the title centered.
	if got := strings.TrimSpace(text(first, 0)); got != doc.Title {
		t.Errorf("title row %q", got)
	}
	at := strings.IndexFunc(text(first, 0), func(r rune) bool { return r != ' ' })
	if ink(0, 0, at) != (raster.Ink{FG: 7, BG: 4}) || ink(0, 0, 0).BG != 4 || ink(0, 0, 39).BG != 4 {
		t.Errorf("title ink %v %v %v", ink(0, 0, 0), ink(0, 0, at), ink(0, 0, 39))
	}
	// Byline on row 1, blank, a rule across the columns, blank, then
	// the first heading at column 0, yellow; a subheading cyan.
	if !strings.HasPrefix(strings.TrimSpace(text(first, 1)), "by Kea Port Authority") || text(first, 2) != "" ||
		text(first, 3) != strings.Repeat("─", 40) || text(first, 4) != "" || !strings.HasPrefix(text(first, 5), "Today") {
		t.Errorf("masthead rows %q / %q / %q / %q / %q", text(first, 1), text(first, 2), text(first, 3), text(first, 4), text(first, 5))
	}
	s, row := find(t, res, "Today")
	if ink(s, row, 0) != (raster.Ink{FG: 3}) {
		t.Errorf("heading ink %v", ink(s, row, 0))
	}
	s, row = find(t, res, "Meltemi tonight")
	if ink(s, row, 0) != (raster.Ink{FG: 6}) {
		t.Errorf("subheading ink %v", ink(s, row, 0))
	}
	// A term: the label cyan, the text default after the gap.
	s, row = find(t, res, "Fuel  06:00")
	if ink(s, row, 0) != (raster.Ink{FG: 6}) || ink(s, row, 6) != (raster.Ink{}) {
		t.Errorf("term inks %v %v", ink(s, row, 0), ink(s, row, 6))
	}
	// Emphasis: _not_ is white, its markers blank, the text around
	// it default. The wrapped line it lands on is the breaker's.
	s, row, i := -1, -1, -1
	for si, r := range res.Rasters {
		for ri := range r.Height() {
			line := []rune(text(r, ri))
			for k := 1; k+4 < len(line); k++ {
				if string(line[k:k+3]) == "not" && line[k-1] == ' ' && line[k+3] == ' ' && r.Rows[ri][k].Ink == (raster.Ink{FG: 7}) {
					s, row, i = si, ri, k
				}
			}
		}
	}
	if i < 0 {
		t.Fatal("no emphasized 'not'")
	}
	if ink(s, row, i+2) != (raster.Ink{FG: 7}) || ink(s, row, i-1) != (raster.Ink{}) || ink(s, row, i+4) != (raster.Ink{}) {
		t.Errorf("emphasis inks %v %v %v", ink(s, row, i-1), ink(s, row, i+2), ink(s, row, i+4))
	}
	// A table: the header cyan, the separator in the rule glyph, a
	// row default; the heading above it kept with it.
	s, row = find(t, res, "Dep    To")
	if ink(s, row, 0) != (raster.Ink{FG: 6}) || !strings.HasPrefix(text(res.Rasters[s], row+1), "──────") || ink(s, row+2, 0) != (raster.Ink{}) {
		t.Errorf("table rows %q %q", text(res.Rasters[s], row+1), text(res.Rasters[s], row+2))
	}
	if text(res.Rasters[s], row-2) != "Ferries · Δρομολόγια" {
		t.Errorf("heading not kept with its table: %q", text(res.Rasters[s], row-2))
	}
	// The rights notice ends the flow; every screen fits its rows.
	find(t, res, "Λιμεναρχείο Κέας · hourly")
	for s, r := range res.Rasters {
		if r.Height() > 28 {
			t.Errorf("screen %d has %d rows", s, r.Height())
		}
	}
	if len(res.Links) != 0 {
		t.Errorf("links %v", res.Links)
	}
	// Reproducible.
	again, _ := Render(doc, l, "")
	if strings.Join(again.Sources, "") != strings.Join(res.Sources, "") || !bytes.Equal(allBytes(again), allBytes(res)) {
		t.Error("render is not reproducible")
	}
}

// Two columns per screen: the second column starts after the first
// and a gutter, its headings are inked there, and the documents that
// cannot fit are errors.
func TestColumns(t *testing.T) {
	doc := parse(t, `Two columns
.by the cell writer

# Today

The harbour is open. Berthing on the south quay only, since the north quay is reserved for the evening ferry. Stern-to, with lazy lines in place.

# Tonight

North seven to eight from six, gusting nine in the channel. Double up mooring lines and fenders, and do not anchor in Vourkari bay.

## Fees

Up to ten metres fourteen euro, ten to fifteen twenty-two, fifteen to twenty thirty-six, over twenty ask.
`)
	l := Layout{Rows: 14, Columns: 2, Gutter: 2}
	res, err := Render(doc, l, "")
	if err != nil {
		t.Fatal(err)
	}
	first := res.Rasters[0]
	if !strings.HasPrefix(text(first, 5), "Today") {
		t.Errorf("row 5: %q", text(first, 5))
	}
	// Column 2 begins at 19+2 = 21, after a blank gutter; a heading
	// there is yellow while the row's column-1 text keeps its own ink.
	found := false
	for _, r := range res.Rasters {
		for row := range r.Height() {
			cells := r.Rows[row]
			if line := text(r, row); len([]rune(line)) > 21 && strings.HasPrefix(string([]rune(line)[21:]), "Tonight") || strings.HasPrefix(text(r, row), strings.Repeat(" ", 21)+"Tonight") {
				found = true
				if cells[19].Glyph != 0 || cells[20].Glyph != 0 || cells[21].Ink != (raster.Ink{FG: 3}) {
					t.Errorf("row %d: gutter %v %v, heading ink %v", row, cells[19], cells[20], cells[21].Ink)
				}
			}
		}
	}
	if !found {
		t.Error("no heading at column 21")
	}
	if _, err := Render(doc, Layout{Rows: 24, Columns: 5}, ""); err == nil || !strings.Contains(err.Error(), "measure") {
		t.Errorf("narrow: %v", err)
	}
	if _, err := Render(doc, Layout{Rows: 8, Screens: 1}, ""); err == nil || !strings.Contains(err.Error(), "do not fit") {
		t.Errorf("overflow: %v", err)
	}
	// One screen of any height: a scroll.
	scroll, err := Render(doc, Layout{}, "")
	if err != nil || len(scroll.Rasters) != 1 || scroll.Rasters[0].Height() < 14 {
		t.Fatalf("scroll: %v, %d rasters", err, len(scroll.Rasters))
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
	l := Layout{Rows: 16}
	res, err := Render(doc, l, "")
	if err != nil {
		t.Fatal(err)
	}
	c := res.Rasters[0]
	// No byline: the title, a blank, the rule, a blank, then
	// content from row 4.
	for i, want := range []string{".item not a command", "+ not a continuation", "..dots", ".", "plain", "", "[Repani]", "", strings.Repeat("─", 40), "", "Head"} {
		if got := text(c, 4+i); got != want {
			t.Errorf("row %d: %q, want %q", 4+i, got, want)
		}
	}
	if len(res.Links) != 1 || res.Links[0] != (Link{Text: "Repani", URL: "https://repani.com"}) {
		t.Errorf("links %v", res.Links)
	}
	if links := c.Links(10); len(links) != 1 || links[0].Target != "Repani" {
		t.Errorf("raster links %v", links)
	}
	vocab := strings.Replace(Vocabulary, ".def heading\n.fg yellow", ".def heading\n.fg red", 1)
	res2, err := Render(doc, l, vocab)
	if err != nil {
		t.Fatal(err)
	}
	if ink := res2.Rasters[0].Rows[14][0].Ink; ink != (raster.Ink{FG: 1}) {
		t.Errorf("custom heading ink %v", ink)
	}
	// A rune outside the repertoire is an error, never a substitution.
	if _, err := Render(parse(t, "T\n\nsection § 3\n"), l, ""); err == nil {
		t.Error("rune outside the repertoire accepted")
	}
}
