package main

import (
	"fmt"
	"strings"
	"testing"

	"repani.com/pica"
	"repani.com/pica/pdf"
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

// checkCols verifies no column exceeds its capacity (measured in
// half-line units) and returns the flattened non-blank texts.
func checkCols(t *testing.T, cols [][]sline, capacity func(int) int) []string {
	t.Helper()
	var flat []string
	for i, col := range cols {
		units := 0
		for _, ln := range col {
			units += roleUnits(ln.role)
		}
		if units > 2*capacity(i) {
			t.Fatalf("column %d holds %d half-line units, capacity %d lines", i, units, capacity(i))
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

func TestExtractNums(t *testing.T) {
	// Grid: "Alpha      1,234.56 " — N column at [10,20), frac 3,
	// paren slot. The cell content lifts into a span anchored at
	// SepIndex and the mono text is blanked behind it.
	cols := []pica.NumCol{{Span: pica.Span{Start: 10, End: 20}, Frac: 3, Paren: true}}

	ln := extractNums(sline{text: "Alpha       1,234.56"}, cols)
	if len(ln.nums) != 1 {
		t.Fatalf("nums = %+v, want one span", ln.nums)
	}
	sp := ln.nums[0]
	if sp.intPart != "1,234" || sp.tail != ".56" || sp.sep != cols[0].SepIndex() {
		t.Errorf("span = %+v", sp)
	}
	if ln.text != "Alpha" {
		t.Errorf("blanked text = %q, want %q", ln.text, "Alpha")
	}

	// Non-numeric content stays on the mono grid, untouched.
	for _, s := range []string{"Client         Amount", "-------------- -----", "Epsilon    n/a"} {
		if got := extractNums(sline{text: s}, cols); len(got.nums) != 0 || got.text != s {
			t.Errorf("extractNums(%q) = %+v", s, got)
		}
	}

	// A line trimmed short of the cell is left alone.
	if got := extractNums(sline{text: "Alpha"}, cols); len(got.nums) != 0 || got.text != "Alpha" {
		t.Errorf("short line = %+v", got)
	}
}

func TestCompose_EvenRowPitch(t *testing.T) {
	// All notes fit one half-line: every data row is padded to the
	// same 3-unit pitch; the total row stays unpadded under its
	// rule.
	src := "T\n\n.table 6L 5N\nClient | Amt\nAlpha | 1.00\n.. custody |\nBeta | 2.00\n= Total | 3.00\n.end\n\n.width 30\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := compose(doc, typo{ps: 9, psMono: 9, lineH: 11})
	if err != nil {
		t.Fatal(err)
	}
	segs := blocks[0].segs
	// header(2 lines)=4, Alpha+note=3, Beta+pad=3, rule+Total=4.
	want := []int{4, 3, 3, 4}
	for i, s := range segs {
		if s.height() != want[i] {
			t.Errorf("seg %d height = %d, want %d (%+v)", i, s.height(), want[i], s.lines)
		}
	}

	// A wrapping note switches the table back to variable heights:
	// no padding anywhere.
	src = "T\n\n.table 6L 5N\nClient | Amt\nAlpha | 1.00\n.. a very long custody annotation that wraps |\nBeta | 2.00\n.end\n\n.width 30\n"
	doc, err = pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err = compose(doc, typo{ps: 9, psMono: 9, lineH: 11})
	if err != nil {
		t.Fatal(err)
	}
	segs = blocks[0].segs
	if n := segs[1].height(); n <= 4 {
		t.Errorf("wrapped note seg height = %d, want > 4", n)
	}
	if n := segs[2].height(); n != 2 {
		t.Errorf("plain row seg height = %d, want 2 (no pad)", n)
	}
}

func TestFlow_HeadingChainKeeps(t *testing.T) {
	// A section heading directly over a subsection heading must
	// bind through it to the subsection's first content: the keep
	// walks the chain, so neither heading strands at a column foot.
	filler := fblock{segs: mkSegs("fill", 5)}
	h1 := fblock{segs: []seg{{lines: []sline{{text: "h1", style: styleBold}}}}, keepNext: true, atomic: true}
	h2 := fblock{segs: []seg{{lines: []sline{{text: "h2", style: styleBold}}}}, keepNext: true, atomic: true}
	para := fblock{segs: mkSegs("para", 4)}

	// Capacity 10 lines: h1 alone plus h2's opening would fit after
	// the filler (the one-level keep), but the chain through h2 to
	// para's first minKeep lines does not — both headings must move.
	cols := flow([]fblock{filler, h1, h2, para}, fixedCap(10))
	checkCols(t, cols, fixedCap(10))
	if len(cols) < 2 {
		t.Fatalf("expected a column break, got %d column(s)", len(cols))
	}
	for _, ln := range cols[0] {
		if ln.text == "h1" || ln.text == "h2" {
			t.Fatalf("heading %q stranded in column 0: %v", ln.text, cols[0])
		}
	}
	if cols[1][0].text != "h1" || cols[1][2].text != "h2" {
		t.Fatalf("column 1 does not open with the heading chain: %v", cols[1])
	}
}

func TestCompose_ProseCells(t *testing.T) {
	// In a sans document a P cell's measured lines attach to the
	// row's slines as positioned spans at the column's grid offset,
	// and the mono text reserves the space blank.
	src := "T\n\n.table 6L *P\nkey | meaning\nem | the point size squared, the unit of horizontal measure\n.end\n\n.width 30\n.font sans\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	units := 30 * pdf.AvgAdvance(pdf.Sans)
	blocks, err := compose(doc, typo{sans: true, ps: 9, psMono: 9, lineH: 11, units: units})
	if err != nil {
		t.Fatal(err)
	}
	segs := blocks[0].segs
	row := segs[1].lines // header seg first, then the row
	found := 0
	for _, ln := range row {
		for _, sp := range ln.prose {
			if sp.off != 7*runeUnits {
				t.Errorf("prose span off = %d, want %d", sp.off, 7*runeUnits)
			}
			if len(sp.words) == 0 {
				t.Error("empty prose span")
			}
			found++
		}
		if strings.Contains(ln.text, "point") {
			t.Errorf("prose content leaked into mono text: %q", ln.text)
		}
	}
	// The header row is labels: bold, blanked on the grid, drawn as
	// spans, over a per-column segmented rule.
	head := segs[0].lines
	if head[0].style != styleBold || len(head[0].prose) != 2 {
		t.Errorf("header line: style=%v spans=%d, want bold with 2 spans", head[0].style, len(head[0].prose))
	}
	last := head[len(head)-1]
	if last.style != styleRule || len(last.ruleSegs) != 2 {
		t.Errorf("header rule: style=%v segs=%v, want rule with 2 segments", last.style, last.ruleSegs)
	}
	if found == 0 {
		t.Fatal("no prose spans attached")
	}
}

func TestCompose_ItemRunGaps(t *testing.T) {
	// A tight run containing a turnover gets a half-line gap after
	// every item but the last, glued into each item's final seg; an
	// all single-line run stays tight.
	src := "T\n\n.item alpha\n.item a rather long item that certainly wraps here\n.item omega\n\n.item one\n.item two\n\n.width 20\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := compose(doc, typo{ps: 9, psMono: 9, lineH: 11})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 5 {
		t.Fatalf("blocks = %d, want 5", len(blocks))
	}
	endsHalf := func(fb fblock) bool {
		s := fb.segs[len(fb.segs)-1]
		return s.lines[len(s.lines)-1].role == roleHalf
	}
	for i, want := range []bool{true, true, false, false, false} {
		if got := endsHalf(blocks[i]); got != want {
			t.Errorf("block %d trailing half spacer = %v, want %v", i, got, want)
		}
	}
	if blocks[1].segs[0].height() != 2 || blocks[0].height() != 3 {
		t.Errorf("unexpected heights: first item %d units, wrapped item first seg %d",
			blocks[0].height(), blocks[1].segs[0].height())
	}
}

func TestCompose_HeadingRoles(t *testing.T) {
	// "#" composes at the display role (4 units), "##" at the
	// heading role (3 units), in both font modes.
	src := "T\n\n# Section\n\nprose\n\n## Subsection\n\nmore\n\n.width 30\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []typo{
		{ps: 9, psMono: 9, lineH: 11},
		{sans: true, ps: 9, psMono: 9, lineH: 11, units: 30 * pdf.AvgAdvance(pdf.Sans)},
	} {
		blocks, err := compose(doc, typ)
		if err != nil {
			t.Fatal(err)
		}
		sec, sub := blocks[0].segs[0], blocks[2].segs[0]
		if r := sec.lines[0].role; r != roleDisplay || sec.height() != 4 {
			t.Errorf("sans=%v: section role=%v height=%d, want display/4", typ.sans, r, sec.height())
		}
		if r := sub.lines[0].role; r != roleHeading || sub.height() != 3 {
			t.Errorf("sans=%v: subsection role=%v height=%d, want heading/3", typ.sans, r, sub.height())
		}
	}
}

func TestFlow_HalfLineSnap(t *testing.T) {
	// A table whose noted row leaves the column at an odd half-line
	// count: the next block snaps back to a whole body line via a
	// blank half-line before its separator.
	table := fblock{segs: []seg{
		{lines: []sline{{text: "h"}, {text: "--"}}},
		{lines: []sline{{text: "r1"}, {text: "n1", role: roleHalf}}},
		{lines: []sline{{text: "r2"}}},
	}, repeat: 1}
	para := fblock{segs: []seg{{lines: []sline{{text: "p1"}}}}}

	cols := flow([]fblock{table, para}, fixedCap(8))
	checkCols(t, cols, fixedCap(8))
	if len(cols) != 1 {
		t.Fatalf("expected one column, got %d", len(cols))
	}
	got := cols[0]
	wantTexts := []string{"h", "--", "r1", "n1", "r2", "", "", "p1"}
	wantHalf := []bool{false, false, false, true, false, true, false, false}
	if len(got) != len(wantTexts) {
		t.Fatalf("column = %v, want %d lines", got, len(wantTexts))
	}
	for i := range got {
		half := got[i].role == roleHalf
		if got[i].text != wantTexts[i] || half != wantHalf[i] {
			t.Fatalf("line %d = {%q half=%v}, want {%q half=%v}",
				i, got[i].text, half, wantTexts[i], wantHalf[i])
		}
	}
}

func TestFlow_NoteStaysWithRow(t *testing.T) {
	// Row segs carry their note lines, so a split can never orphan
	// a note from its row, and the header repeats above it.
	segs := []seg{{lines: []sline{{text: "h"}}}}
	for i := 1; i <= 5; i++ {
		lines := []sline{{text: fmt.Sprintf("r%d", i)}}
		if i == 2 {
			lines = append(lines, sline{text: "n2", role: roleHalf})
		}
		segs = append(segs, seg{lines: lines})
	}
	table := fblock{segs: segs, repeat: 1}

	cols := flow([]fblock{table}, fixedCap(3))
	checkCols(t, cols, fixedCap(3))
	for i, col := range cols {
		if col[0].text != "h" {
			t.Fatalf("column %d does not start with the header: %v", i, col)
		}
		for j, ln := range col {
			if ln.text != "r2" {
				continue
			}
			if j+1 >= len(col) || col[j+1].text != "n2" || col[j+1].role != roleHalf {
				t.Fatalf("n2 does not follow r2 in column %d: %v", i, col)
			}
		}
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
			if rest, ok := strings.CutSuffix(ln.text, "a"); ok {
				if j+1 >= len(col) || col[j+1].text != rest+"b" {
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
	for i := range 48 {
		fmt.Fprintf(&b, "# Section %d\n\n", i)
		b.WriteString(strings.Repeat("The quick brown fox jumps over the lazy dog and keeps running through the sunlit meadow. ", 3))
		b.WriteString("\n\n")
	}
	b.WriteString(".table 6L *L 4R\nDay | Conditions | Temp\nMon | Sunny with a strengthening westerly breeze | 25\nTue | Cloudy | 22\n.end\n")

	doc, err := pica.Parse(b.String())
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
	doc, err := pica.Parse(src)
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
	ln := pica.Line{Words: words, Width: wsum + 2*m.Space()}
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

func TestSpread_DashFinalLineHangsHyphen(t *testing.T) {
	// A justified line ending in "-" targets units plus the hyphen
	// hang: the drawn line runs hang units past the wrap width, so
	// the hyphen protrudes into the margin.
	m := pdf.Measure(pdf.Sans)
	words := []string{"aaa", "bbb", "ccc-"}
	wsum := 0
	for _, w := range words {
		wsum += m.Width(w)
	}
	ln := pica.Line{Words: words, Width: wsum + 2*m.Space()}
	units := ln.Width // pretend the line naturally fills the width
	hang := pica.HangHyphen(m)
	if hang <= 0 {
		t.Fatal("sans hyphen hang should be positive")
	}
	gaps := spread(ln, units, m, false)
	total := wsum
	for _, g := range gaps {
		total += g
	}
	if total != units+hang {
		t.Errorf("dash-final line fills %d, want %d (units %d + hang %d)",
			total, units+hang, units, hang)
	}
	// The same line as a paragraph's last line stays natural.
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
	doc, err := pica.Parse(src)
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

func TestBroadsheet_NewBlocks(t *testing.T) {
	body := "\n\n.by A. Writer\n.date Today\n\n" +
		".quote\n" + strings.Repeat("The quick brown fox jumps over the lazy dog. ", 3) + "\n.attrib Aesop\n.end\n\n" +
		".item first item that runs long enough to wrap onto another line for sure\n.item second item\n\n" +
		strings.Repeat("Plain prose follows the list and fills the columns evenly. ", 4) + "\n"
	for _, trailer := range []string{"\n.width 40\n.cols 2\n", "\n.width 40\n.cols 2\n.font sans\n"} {
		doc, err := pica.Parse("The Daily Fable" + body + trailer)
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
		if string(b1) != string(b2) {
			t.Errorf("broadsheet with new blocks is not deterministic (%s)", strings.TrimSpace(trailer))
		}
	}
}

// TestDeriveTypo_FloorGuardsMonoSize: in a sans document the mono
// size (tables, verbatim) is the smaller of the two derived sizes;
// the readability floor must catch it even when the body size passes.
func TestDeriveTypo_FloorGuardsMonoSize(t *testing.T) {
	doc, err := pica.Parse("T\n\nbody\n\n.width 40\n.font sans\n")
	if err != nil {
		t.Fatal(err)
	}
	// A column where the mono size lands just under the floor.
	colW := emWidth * 40 * (minPs - 0.1)
	sansPs := colW * 1000 / float64(40*pdf.AvgAdvance(pdf.Sans))
	if sansPs < minPs {
		t.Fatalf("premise: sans body size %.2f must clear the floor %.1f", sansPs, minPs)
	}
	if _, err := deriveTypo(doc, colW); err == nil {
		t.Fatal("expected floor error for the mono size")
	}
	doc.Layout.Font = "mono"
	if _, err := deriveTypo(doc, colW); err == nil {
		t.Fatal("expected floor error in mono mode too")
	}
}

// TestCompose_HeadingWraps: a heading longer than its shrunken
// measure wraps in both modes (mono used to truncate silently).
func TestCompose_HeadingWraps(t *testing.T) {
	long := "A very long section heading that cannot fit on one line"
	doc, err := pica.Parse("T\n\n# " + long + "\n\nprose\n\n.width 30\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []typo{
		{ps: 9, psMono: 9, lineH: 11},
		{sans: true, ps: 9, psMono: 9, lineH: 11, units: 30 * pdf.AvgAdvance(pdf.Sans)},
	} {
		blocks, err := compose(doc, typ)
		if err != nil {
			t.Fatal(err)
		}
		h := blocks[0]
		if len(h.segs) < 2 {
			t.Fatalf("sans=%v: heading composed as %d line(s), want a wrap", typ.sans, len(h.segs))
		}
		var words []string
		for _, sg := range h.segs {
			ln := sg.lines[0]
			if ln.role != roleDisplay || ln.style != styleBold {
				t.Errorf("sans=%v: wrapped heading line lost role/style", typ.sans)
			}
			if typ.sans {
				words = append(words, ln.words...)
			} else {
				if n := len([]rune(ln.text)); n > 30*2/3 {
					t.Errorf("mono heading line %q is %d runes, budget %d", ln.text, n, 30*2/3)
				}
				words = append(words, strings.Fields(ln.text)...)
			}
		}
		if got := strings.Join(words, " "); got != long {
			t.Errorf("sans=%v: heading text %q, want %q (nothing truncated)", typ.sans, got, long)
		}
	}
}
