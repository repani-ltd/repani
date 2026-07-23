package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pavlos/typeset"
	"github.com/pavlos/typeset/pdf"
)

// mkSegs builds n single-line segments labeled prefix1..prefixN.
func mkSegs(prefix string, n int) []seg {
	out := make([]seg, n)
	for i := range out {
		out[i] = seg{lines: []sline{{text: fmt.Sprintf("%s%d", prefix, i+1)}}}
	}
	return out
}

func fixedCap(n int) func(int) int { return func(int) int { return n } }

// checkCols verifies no column exceeds its capacity and returns the
// flattened non-blank texts.
func checkCols(t *testing.T, cols [][]sline, capacity func(int) int) []string {
	t.Helper()
	var flat []string
	for i, col := range cols {
		if len(col) > capacity(i) {
			t.Fatalf("column %d holds %d lines, capacity %d", i, len(col), capacity(i))
		}
		for _, ln := range col {
			if strings.TrimSpace(ln.text) != "" {
				flat = append(flat, ln.text)
			}
		}
	}
	return flat
}

func TestFlow_OrphanNeverSingleSegAtBottom(t *testing.T) {
	blocks := []fblock{
		{segs: mkSegs("fill", 2)},
		{segs: mkSegs("para", 6)},
	}
	cols := flow(blocks, fixedCap(4))
	checkCols(t, cols, fixedCap(4))
	if len(cols[0]) != 2 {
		t.Fatalf("column 0 = %v, want just the filler", cols[0])
	}
	if len(cols[1]) != 4 || cols[1][0].text != "para1" {
		t.Fatalf("column 1 wrong: %v", cols[1])
	}
	if len(cols[2]) != 2 || cols[2][0].text != "para5" {
		t.Fatalf("column 2 wrong: %v", cols[2])
	}
}

func TestFlow_WidowNeverSingleSegAtTop(t *testing.T) {
	blocks := []fblock{{segs: mkSegs("p", 5)}}
	cols := flow(blocks, fixedCap(4))
	checkCols(t, cols, fixedCap(4))
	if len(cols) != 2 || len(cols[0]) != 3 || len(cols[1]) != 2 {
		t.Fatalf("split %d cols, want 3/2 lines", len(cols))
	}
}

func TestFlow_AtomicNeverSplits(t *testing.T) {
	blocks := []fblock{
		{segs: mkSegs("a", 3)},
		{segs: []seg{{lines: toSlines([]string{"pre1", "pre2", "pre3"})}}, atomic: true},
	}
	cols := flow(blocks, fixedCap(5))
	checkCols(t, cols, fixedCap(5))
	if len(cols) != 2 || len(cols[1]) != 3 {
		t.Fatalf("cols = %v", cols)
	}
}

func TestFlow_HeadingKeepsWithNext(t *testing.T) {
	blocks := []fblock{
		{segs: mkSegs("fill", 4)},
		{segs: []seg{{lines: []sline{{text: "Heading", style: styleBold}}}}, keepNext: true, atomic: true},
		{segs: mkSegs("body", 4)},
	}
	cols := flow(blocks, fixedCap(6))
	checkCols(t, cols, fixedCap(6))
	if len(cols[0]) != 4 {
		t.Fatalf("column 0 = %v, want only filler", cols[0])
	}
	if cols[1][0].text != "Heading" || cols[1][2].text != "body1" {
		t.Fatalf("column 1 = %v, want heading atop its body", cols[1])
	}
}

func TestFlow_TableSplitRepeatsHeader(t *testing.T) {
	table := fblock{
		segs:   append([]seg{{lines: toSlines([]string{"Hour  Level", "----  -----"})}}, mkSegs("row", 10)...),
		repeat: 1,
	}
	cols := flow([]fblock{table}, fixedCap(6))
	checkCols(t, cols, fixedCap(6))
	if len(cols) < 2 {
		t.Fatalf("expected a split, got %d column(s)", len(cols))
	}
	var rows []string
	for i, col := range cols {
		if col[0].text != "Hour  Level" || col[1].text != "----  -----" {
			t.Fatalf("column %d does not start with the header: %v", i, col)
		}
		if len(col)-2 < minKeep {
			t.Fatalf("column %d has %d data rows", i, len(col)-2)
		}
		for _, ln := range col[2:] {
			rows = append(rows, ln.text)
		}
	}
	for i, r := range rows {
		if want := fmt.Sprintf("row%d", i+1); r != want {
			t.Fatalf("data row %d = %q, want %q", i, r, want)
		}
	}
	if len(rows) != 10 {
		t.Fatalf("data rows = %d, want 10", len(rows))
	}
}

func TestFlow_MultiLineRowsAreAtomic(t *testing.T) {
	// Rows of 2 lines each; capacity 5 fits header(1)+2 rows(4).
	rows := []seg{
		{lines: toSlines([]string{"hdr"})},
		{lines: toSlines([]string{"r1a", "r1b"})},
		{lines: toSlines([]string{"r2a", "r2b"})},
		{lines: toSlines([]string{"r3a", "r3b"})},
		{lines: toSlines([]string{"r4a", "r4b"})},
	}
	blocks := []fblock{{segs: rows, repeat: 1}}
	cols := flow(blocks, fixedCap(5))
	checkCols(t, cols, fixedCap(5))
	for i, col := range cols {
		for _, ln := range col {
			if strings.HasSuffix(ln.text, "a") {
				// Row start: its second line must be adjacent.
				continue
			}
		}
		if len(col) == 0 {
			t.Fatalf("empty column %d", i)
		}
	}
	// No column may contain an "a" line without its "b" line.
	for i, col := range cols {
		for j, ln := range col {
			if strings.HasSuffix(ln.text, "a") {
				if j+1 >= len(col) || col[j+1].text != strings.TrimSuffix(ln.text, "a")+"b" {
					t.Fatalf("column %d split row %q mid-way", i, ln.text)
				}
			}
		}
	}
}

func TestFlow_BlockTallerThanColumnForceSplits(t *testing.T) {
	blocks := []fblock{{segs: mkSegs("x", 20)}}
	cols := flow(blocks, fixedCap(6))
	flat := checkCols(t, cols, fixedCap(6))
	if len(flat) != 20 {
		t.Fatalf("lines preserved = %d, want 20", len(flat))
	}
}

func TestBroadsheetEndToEnd(t *testing.T) {
	var b strings.Builder
	b.WriteString("E2E TEST SHEET\n\n")
	for i := 0; i < 48; i++ {
		fmt.Fprintf(&b, "# Section %d\n\n", i)
		b.WriteString(strings.Repeat("The quick brown fox jumps over the lazy dog and keeps running through the sunlit meadow. ", 3))
		b.WriteString("\n\n")
	}
	b.WriteString(".table 6L *L 4R\nDay | Conditions | Temp\nMon | Sunny with a strengthening westerly breeze | 25\nTue | Cloudy | 22\n.end\n")

	doc, err := typeset.Parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	out, err := broadsheet(doc)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "%PDF-1.3") {
		t.Fatal("not a PDF")
	}
	if pages := strings.Count(s, "/Type /Page\n"); pages < 2 {
		t.Fatalf("pages = %d, want multi-page", pages)
	}

	// Deterministic bytes: rendering again is identical.
	out2, err := broadsheet(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(out2) != s {
		t.Fatal("PDF bytes are not deterministic")
	}
}

func TestBroadsheet_DerivedSizeFloor(t *testing.T) {
	src := "T\n\nbody text here\n\n.paper a5\n.cols 4\n"
	doc, err := typeset.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broadsheet(doc); err == nil {
		t.Fatal("expected readability-floor error for a5/4col/width40")
	}
}

// TestFlow_RepeatTallerThanColumnTerminates is the regression for a
// non-progress loop: a repeated lead-in as tall as the column used to
// make rest() reconstruct the identical block forever.
func TestFlow_RepeatTallerThanColumnTerminates(t *testing.T) {
	header := seg{lines: toSlines([]string{"h1", "h2", "h3", "h4", "h5", "h6"})}
	blocks := []fblock{{
		segs:   append([]seg{header}, mkSegs("row", 4)...),
		repeat: 1,
	}}
	// Capacity smaller than the header alone: must terminate and
	// keep every data row exactly once.
	cols := flow(blocks, fixedCap(4))
	rows := 0
	for _, col := range cols {
		for _, ln := range col {
			if strings.HasPrefix(ln.text, "row") {
				rows++
			}
		}
	}
	if rows != 4 {
		t.Fatalf("data rows survived = %d, want 4", rows)
	}
	if len(cols) > 12 {
		t.Fatalf("suspiciously many columns (%d): non-progress split?", len(cols))
	}
}

func TestSpread_NegativeSlackCompressesGaps(t *testing.T) {
	// A shrunk line (Width > units) must compress its gaps so the
	// drawn line fills units exactly: gap sum = units - word widths.
	m := pdf.Measure(pdf.Sans)
	words := []string{"one", "two", "three"}
	wsum := 0
	for _, w := range words {
		wsum += m.Width(w)
	}
	ln := typeset.Line{Words: words, Width: wsum + 2*m.Space()}
	units := ln.Width - 100 // wrap width 100 units narrower than natural
	gaps := spread(ln, units, m, false)
	total := wsum
	for _, g := range gaps {
		total += g
	}
	if total != units {
		t.Errorf("compressed line fills %d, want %d (gaps %v)", total, units, gaps)
	}
	for _, g := range gaps {
		if g >= m.Space() {
			t.Errorf("gap %d not compressed below natural %d", g, m.Space())
		}
	}
	// Final lines stay at natural spacing regardless of slack.
	for _, g := range spread(ln, units, m, true) {
		if g != m.Space() {
			t.Errorf("final line gap %d, want natural %d", g, m.Space())
		}
	}
}

func TestBroadsheet_Sans(t *testing.T) {
	src := "The Daily Fable\n\n# Weather\n\n" +
		strings.Repeat("The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow. ", 6) +
		"\n\n.table 10L 6R\nCity | Temp\nAthens | 31\nNicosia | 34\n.end\n\n.link https://example.com Example\n\n.width 40\n.cols 2\n.font sans\n"
	doc, err := typeset.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	b1, err := broadsheet(doc)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := broadsheet(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b1), "FiraSans-Regular") {
		t.Error("sans document does not embed Fira Sans")
	}
	if !strings.Contains(string(b1), "FiraMono-Regular") {
		t.Error("sans document with a table should still embed Fira Mono")
	}
	if string(b1) != string(b2) {
		t.Error("sans broadsheet is not deterministic")
	}
}
