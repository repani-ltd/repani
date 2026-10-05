package pica

import (
	"errors"
	"strings"
	"testing"

	"repani.com/typeset/tbl"
)

// table parses a .table block of spec and rows on a page wide enough
// for any test table, failing the test on error.
func table(t *testing.T, spec string, rows ...string) *Table {
	t.Helper()
	d, err := Parse("T\n\n.table " + spec + "\n" + strings.Join(rows, "\n") + "\n.end\n\n.width 200\n")
	if err != nil {
		t.Fatalf("table %q: %v", spec, err)
	}
	return d.Blocks[0].Table
}

// layout lays tb out at width, failing the test on error.
func layout(t *testing.T, tb *Table, width int) *TableLayout {
	t.Helper()
	tl, err := tb.Layout(width)
	if err != nil {
		t.Fatalf("Layout(%d): %v", width, err)
	}
	return tl
}

// render is Layout joined to text.
func render(t *testing.T, tb *Table, width int) string {
	t.Helper()
	return strings.Join(layout(t, tb, width).Lines(), "\n")
}

func TestTable_Basic(t *testing.T) {
	tb := table(t, "3L 5L 4R", "^Day | Time | Temp", "Mon | 09:00 | 25", "Tue | 14:30 | 22")
	want := strings.Join([]string{
		"Day Time  Temp",
		"--- ----- ----",
		"Mon 09:00   25",
		"Tue 14:30   22",
	}, "\n")
	if got := render(t, tb, 40); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTable_AutoSpan(t *testing.T) {
	// 3 + 1 + auto + 1 + 4 = 40 -> auto = 31; the same table lays out
	// at another width.
	tb := table(t, "3L *L 4R", "^Day | Forecast | Temp", "Mon | Sunny | 25")
	for _, w := range []int{40, 28} {
		for ln := range strings.SplitSeq(render(t, tb, w), "\n") {
			if len([]rune(ln)) > w {
				t.Errorf("line exceeds %d: %q", w, ln)
			}
		}
	}
}

func TestTable_CellsWrapByDefault(t *testing.T) {
	tb := table(t, "6L *L 4R", "^Day | Conditions | Temp",
		"Sat 11 | High cloud thickening late in the day | 30", "Sun 12 | Clear | 29")
	tl := layout(t, tb, 30)
	// Row 0 wraps: several lines, the other columns blank on the
	// continuations; the short row stays one line; every line fits.
	if len(tl.Rows[0].Lines) < 2 {
		t.Fatalf("expected a wrapped row, got %q", tl.Rows[0].Lines)
	}
	if cont := tl.Rows[0].Lines[1]; !strings.HasPrefix(cont, strings.Repeat(" ", 7)) {
		t.Errorf("continuation does not blank the first column: %q", cont)
	}
	if len(tl.Rows[1].Lines) != 1 {
		t.Errorf("short row wrapped: %q", tl.Rows[1].Lines)
	}
	for _, ln := range tl.Lines() {
		if len([]rune(ln)) > 30 {
			t.Errorf("line exceeds width: %q", ln)
		}
	}
}

func TestTable_ClipModifier(t *testing.T) {
	if got := render(t, table(t, "6L! 4R", "This is far too long | 25"), 12); got != "This i   25" {
		t.Errorf("got %q", got)
	}
}

func TestTable_LongWordHardCut(t *testing.T) {
	tl := layout(t, table(t, "5L", "abcdefghij"), 5)
	if got := tl.Rows[0].Lines; len(got) != 2 || got[0] != "abcde" || got[1] != "fghij" {
		t.Errorf("hard cut wrong: %q", got)
	}
}

func TestTable_Alignment(t *testing.T) {
	// Trailing pad is trimmed.
	if got := render(t, table(t, "5L 5R 5C", "L | R | C"), 17); got != "L         R   C" {
		t.Errorf("got %q", got)
	}
}

func TestTable_NumericColumn(t *testing.T) {
	tb := table(t, "*L 10N", "^Client | Amount", "Alpha | 1,234.56", "Beta | 12.5", "Gamma | (2.00)", "Delta | n/a")
	got := render(t, tb, 20)
	want := strings.Join([]string{
		"Client    Amount",
		"--------- ----------",
		"Alpha      1,234.56",
		"Beta          12.5",
		"Gamma         (2.00)",
		"Delta        n/a",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTable_Numbers(t *testing.T) {
	// A writer that sets numbers itself gets each single N box's
	// number split at the point and the column its point occupies --
	// the cell every formatted line puts it in -- and nothing for a
	// cell that is not a number or a box that spans.
	tb := table(t, "*L 10N", "^Client | Amount", "Alpha | 1,234.56", "Gamma | (2.00)", "Delta | n/a", "a cell across both")
	tl := layout(t, tb, 20)
	for i, want := range []NumCell{
		{Box: Span{Start: 10, End: 20}, Sep: 16, Int: "1,234", Tail: ".56"},
		{Box: Span{Start: 10, End: 20}, Sep: 16, Int: "(2", Tail: ".00)"},
	} {
		r := tl.Rows[i]
		if len(r.Nums) != 1 || r.Nums[0] != want {
			t.Errorf("row %d: %+v, want %+v", i, r.Nums, want)
		}
		if at := strings.LastIndex(r.Lines[0], "."); at != want.Sep {
			t.Errorf("row %d: point at %d in %q, Sep %d", i, at, r.Lines[0], want.Sep)
		}
	}
	if n := tl.Rows[2].Nums; n != nil {
		t.Errorf("n/a: %+v", n)
	}
	// "a cell across both" spans the N column: not a number, and its
	// text stays in the line, whatever a writer does with numbers.
	if n := tl.Rows[3].Nums; n != nil {
		t.Errorf("spanning cell: %+v", n)
	}
	tl = layout(t, table(t, "6L 6N", "Q1 | 10.5", "See 2024"), 13)
	if n := tl.Rows[1].Nums; n != nil {
		t.Errorf("a short row's digits read as a number: %+v", n)
	}
}

func TestTable_NumericColumnIntegers(t *testing.T) {
	// No fractions, no parens: N degrades to plain right-align.
	if got := render(t, table(t, "3L 5N", "a | 100", "b | 7"), 9); got != "a     100\nb       7" {
		t.Errorf("got %q", got)
	}
}

func TestTable_NumericHeaderLongerFraction(t *testing.T) {
	// A header that reads as a number with a longer fraction than any
	// data row right-aligns flush rather than panicking on a negative
	// pad.
	if got := render(t, table(t, "6N", "^1.50", "2", "(3)"), 6); got != "  1.50\n------\n    2\n   (3)" {
		t.Errorf("got %q", got)
	}
}

func TestTable_NoteRows(t *testing.T) {
	tb := table(t, "6L 5N", "^Client | Amt", ".. | eur", "Alpha | 12.50", ".. prime broker |", "Beta | 3.00")
	tl := layout(t, tb, 12)
	// Half-grid notes: widths and the column gap double, cells
	// left-align in their boxes.
	if got := tl.Header.Notes; !equalLines(got, []string{"              eur"}) {
		t.Errorf("header notes %q", got)
	}
	if got := tl.Rows[0].Notes; !equalLines(got, []string{"prime broker"}) {
		t.Errorf("row notes %q", got)
	}
	if tl.Rows[1].Notes != nil {
		t.Errorf("row 1 notes %q, want none", tl.Rows[1].Notes)
	}
	// Plain text: notes render as ordinary full-size rows in order.
	want := strings.Join([]string{
		"Client Amt",
		"       eur",
		"------ -----",
		"Alpha  12.50",
		"prime",
		"broker",
		"Beta    3.00",
	}, "\n")
	if got := strings.Join(tl.Lines(), "\n"); got != want {
		t.Errorf("Lines():\n%s\nwant:\n%s", got, want)
	}
}

func equalLines(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestTable_TotalRows(t *testing.T) {
	tb := table(t, "6L 8N", "^Client | Amt", "Alpha | 100.00", "Beta | 25.50", "= Total | 125.50")
	want := strings.Join([]string{
		"Client   Amt",
		"------ --------",
		"Alpha    100.00",
		"Beta      25.50",
		"------ --------",
		"Total    125.50",
	}, "\n")
	if got := render(t, tb, 15); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	tl := layout(t, tb, 15)
	if tl.Rows[0].Total || tl.Rows[1].Total || !tl.Rows[2].Total {
		t.Errorf("totals %v %v %v", tl.Rows[0].Total, tl.Rows[1].Total, tl.Rows[2].Total)
	}
}

func TestTable_ProseColumn(t *testing.T) {
	tb := table(t, "4L *P", "^key | meaning", "em | the point size squared, the unit of measure", "box | a rectangle")

	// Mono path: P lays out exactly as L, nothing measured.
	tlm := layout(t, tb, 20)
	if tlm.Rows[0].Prose != nil {
		t.Error("mono Layout must not measure prose cells")
	}
	if !strings.Contains(tlm.Rows[0].Lines[0], "the point size") {
		t.Errorf("mono P cell not laid out as L: %q", tlm.Rows[0].Lines)
	}

	// Measured path: the formatted rows reserve the cell blank at the
	// measured height.
	tl, err := tb.LayoutMeasured(20, Mono, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	pc := tl.Rows[0].Prose[0]
	if pc.Box != (Span{Start: 5, End: 20}) || pc.Align != 'P' || len(pc.Lines) == 0 {
		t.Fatalf("prose cell %+v", pc)
	}
	if got := len(tl.Rows[0].Lines); got != len(pc.Lines) {
		t.Errorf("row height %d, measured lines %d", got, len(pc.Lines))
	}
	for _, physical := range tl.Rows[0].Lines {
		if strings.Contains(physical, "point") {
			t.Errorf("measured P cell not blanked: %q", physical)
		}
	}
	if !strings.HasPrefix(tl.Rows[0].Lines[0], "em") {
		t.Errorf("mono cell missing: %q", tl.Rows[0].Lines[0])
	}
}

func TestTable_ClippedProseIsOneLine(t *testing.T) {
	// "!" holds for a measured P cell as for a mono one: one line.
	tl, err := table(t, "3L 8P!", "a | one two three four five").LayoutMeasured(12, Mono, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Rows[0].Lines) != 1 || len(tl.Rows[0].Prose[0].Lines) != 1 {
		t.Errorf("rows %q, measured lines %d", tl.Rows[0].Lines, len(tl.Rows[0].Prose[0].Lines))
	}
}

func TestTable_LayoutMeasuredHeader(t *testing.T) {
	tb := table(t, "6L 5N", "^Client name | Amt", "Alpha | 12.50")
	plain := layout(t, tb, 12)
	for _, tc := range []struct {
		name       string
		mHead      Measurer
		wantHeight int // header lines
	}{
		{"nil header measurer", nil, 0},
		{"mono header measurer", Mono, 2},
		{"wide header measurer at ten units per rune", wideMeasurer{}, 2},
	} {
		runeUnits := 1
		if tc.mHead != nil {
			runeUnits = tc.mHead.Width("x")
		}
		tl, err := tb.LayoutMeasured(12, Mono, tc.mHead, runeUnits)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.mHead == nil {
			if tl.Header.Prose != nil || !equalLines(tl.Header.Lines, plain.Header.Lines) {
				t.Errorf("%s: header %+v, want Layout's", tc.name, tl.Header)
			}
			continue
		}
		h := tl.Header
		if len(h.Prose) != 2 || len(h.Lines) != tc.wantHeight || len(h.Prose[0].Lines) != tc.wantHeight {
			t.Errorf("%s: header %d lines, %d cells, first %d lines", tc.name, len(h.Lines), len(h.Prose), len(h.Prose[0].Lines))
		}
		for _, ln := range h.Lines {
			if strings.TrimSpace(ln) != "" {
				t.Errorf("%s: measured header not blanked: %q", tc.name, ln)
			}
		}
		// Everything else is exactly Layout.
		if !equalLines(tl.Rows[0].Lines, plain.Rows[0].Lines) {
			t.Errorf("%s: rows differ from Layout", tc.name)
		}
	}
}

func TestTable_InvalidSpec(t *testing.T) {
	for _, spec := range []string{"", "3X", "abc", "3L *L *R", "r 4L", "4L/b"} {
		if _, err := newTable(spec, 1, 1); err == nil {
			t.Errorf("spec %q accepted", spec)
		}
	}
	// Fit errors surface at layout time.
	if _, err := table(t, "50L 50L", "a | b").Layout(40); !errors.Is(err, tbl.ErrFit) {
		t.Errorf("50L 50L in 40: %v", err)
	}
}

func TestTable_RuneAware(t *testing.T) {
	// "λεμεσός" is 7 runes.
	if got := render(t, table(t, "7L", "λεμεσός"), 7); got != "λεμεσός" {
		t.Errorf("got %q", got)
	}
	if got := render(t, table(t, "4L!", "λεμεσός"), 4); got != "λεμε" {
		t.Errorf("got %q", got)
	}
}

func TestTable_CellsHyphenate(t *testing.T) {
	// At 17 runes, greedy wrapping needed 3 lines (Isolated /
	// thunderstorms / inland); Knuth-Plass with hyphenation fits 2.
	lines := layout(t, table(t, "17L", "Isolated thunderstorms inland"), 17).Rows[0].Lines
	if len(lines) != 2 || !strings.Contains(lines[0], "-") {
		t.Errorf("cell set in %q, want 2 lines, hyphenated", lines)
	}
}

func TestTable_NoBreakSpace(t *testing.T) {
	got := render(t, table(t, "9L", "Open sig Page.Row body"), 9)
	if !strings.Contains(got, "Open sig") || strings.Contains(got, "Open\n") {
		t.Errorf("no-break space broke:\n%s", got)
	}
}
