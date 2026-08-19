package typeset

import (
	"slices"
	"strings"
	"testing"
)

// render is a test helper: Layout joined to text, failing the test
// on error.
func render(t *testing.T, tbl *Table, width int) string {
	t.Helper()
	tl, err := tbl.Layout(width)
	if err != nil {
		t.Fatalf("Layout(%d): %v", width, err)
	}
	return strings.Join(tl.Lines(), "\n")
}

func mustTable(t *testing.T, spec string) *Table {
	t.Helper()
	tbl, err := NewTable(spec)
	if err != nil {
		t.Fatalf("NewTable(%q): %v", spec, err)
	}
	return tbl
}

func TestTable_Basic(t *testing.T) {
	tbl := mustTable(t, "3L 5L 4R")
	tbl.Header("Day", "Time", "Temp")
	tbl.Row("Mon", "09:00", "25")
	tbl.Row("Tue", "14:30", "22")

	got := render(t, tbl, 40)
	want := strings.Join([]string{
		"Day Time  Temp",
		"--- ----- ----",
		"Mon 09:00   25",
		"Tue 14:30   22",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTable_AutoSpan(t *testing.T) {
	// 3 + 1 + auto + 1 + 4 = 40 -> auto = 31
	tbl := mustTable(t, "3L *L 4R")
	tbl.Header("Day", "Forecast", "Temp")
	tbl.Row("Mon", "Sunny", "25")

	for ln := range strings.SplitSeq(render(t, tbl, 40), "\n") {
		if len([]rune(ln)) > 40 {
			t.Errorf("line exceeds 40 chars: %q", ln)
		}
	}
	// The same table lays out at another width.
	for ln := range strings.SplitSeq(render(t, tbl, 28), "\n") {
		if len([]rune(ln)) > 28 {
			t.Errorf("line exceeds 28 chars: %q", ln)
		}
	}
}

func TestTable_CellsWrapByDefault(t *testing.T) {
	tbl := mustTable(t, "6L *L 4R")
	tbl.Header("Day", "Conditions", "Temp")
	tbl.Row("Sat 11", "High cloud thickening late in the day", "30")
	tbl.Row("Sun 12", "Clear", "29")

	tl, err := tbl.Layout(30)
	if err != nil {
		t.Fatal(err)
	}
	// Row 0 wraps: multiple lines, continuation cells blank, no
	// content lost.
	if len(tl.Rows[0]) < 2 {
		t.Fatalf("expected wrapped row, got %v", tl.Rows[0])
	}
	// Content preservation, checked at the cell level (row lines
	// interleave other columns' cells): rejoining the wrapped cell
	// and undoing hyphen breaks recovers the original text.
	cell := strings.Join(wrapCell("High cloud thickening late in the day", 18), " ")
	cell = strings.ReplaceAll(cell, "- ", "")
	if cell != "High cloud thickening late in the day" {
		t.Errorf("wrapped cell does not rejoin to original: %q", cell)
	}
	// Continuation lines leave the other columns blank.
	cont := tl.Rows[0][1]
	if !strings.HasPrefix(cont, strings.Repeat(" ", 7)) {
		t.Errorf("continuation does not blank the first column: %q", cont)
	}
	// Unwrapped row stays single-line.
	if len(tl.Rows[1]) != 1 {
		t.Errorf("short row wrapped: %v", tl.Rows[1])
	}
	// Every line fits.
	for _, ln := range tl.Lines() {
		if len([]rune(ln)) > 30 {
			t.Errorf("line exceeds width: %q", ln)
		}
	}
}

func TestTable_ClipModifier(t *testing.T) {
	tbl := mustTable(t, "6L! 4R")
	tbl.Row("This is far too long", "25")
	got := render(t, tbl, 12)
	if got != "This i   25" {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Error("clipped cell should not wrap")
	}
}

func TestTable_LongWordHardCut(t *testing.T) {
	tbl := mustTable(t, "5L")
	tbl.Row("abcdefghij")
	tl, err := tbl.Layout(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Rows[0]) != 2 || strings.TrimSpace(tl.Rows[0][0]) != "abcde" || strings.TrimSpace(tl.Rows[0][1]) != "fghij" {
		t.Errorf("hard cut wrong: %v", tl.Rows[0])
	}
}

func TestTable_Alignment(t *testing.T) {
	tbl := mustTable(t, "5L 5R 5C")
	tbl.Row("L", "R", "C")
	got := render(t, tbl, 17)
	// Trailing pad is trimmed.
	want := "L         R   C"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTable_NumericColumn(t *testing.T) {
	tbl := mustTable(t, "*L 10N")
	tbl.Header("Client", "Amount")
	tbl.Row("Alpha", "1,234.56")
	tbl.Row("Beta", "12.5")
	tbl.Row("Gamma", "(2.00)")
	tbl.Row("Delta", "n/a")

	got := render(t, tbl, 20)
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

	// Every decimal point sits in the same rune column.
	var dots []int
	for _, ln := range strings.Split(got, "\n")[2:5] {
		dots = append(dots, strings.LastIndex(ln, "."))
	}
	if dots[0] != dots[1] || dots[1] != dots[2] {
		t.Errorf("decimal points misaligned: %v", dots)
	}
}

func TestTable_NoteRows(t *testing.T) {
	tbl := mustTable(t, "6L 5N")
	tbl.Header("Client", "Amt")
	tbl.Note("", "eur")
	tbl.Row("Alpha", "12.50")
	tbl.Note("prime broker", "")
	tbl.Row("Beta", "3.00")

	tl, err := tbl.Layout(12)
	if err != nil {
		t.Fatal(err)
	}

	// Half-grid notes: widths and the column gap double, cells
	// left-align under their columns.
	if want := []string{"              eur"}; !equalLines(tl.HeaderNotes, want) {
		t.Errorf("HeaderNotes = %q, want %q", tl.HeaderNotes, want)
	}
	if want := []string{"prime broker"}; !equalLines(tl.RowNotes[0], want) {
		t.Errorf("RowNotes[0] = %q, want %q", tl.RowNotes[0], want)
	}
	if tl.RowNotes[1] != nil {
		t.Errorf("RowNotes[1] = %q, want none", tl.RowNotes[1])
	}

	// Plain-text form: notes render as ordinary full-size rows in
	// document order.
	got := strings.Join(tl.Lines(), "\n")
	want := strings.Join([]string{
		"Client Amt",
		"       eur",
		"------ -----",
		"Alpha  12.50",
		"prime",
		"broker",
		"Beta    3.00",
	}, "\n")
	if got != want {
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
	tbl := mustTable(t, "6L 8N")
	tbl.Header("Client", "Amt")
	tbl.Row("Alpha", "100.00")
	tbl.Row("Beta", "25.50")
	tbl.Total("Total", "125.50")

	got := render(t, tbl, 15)
	want := strings.Join([]string{
		"Client   Amt",
		"------ --------",
		"Alpha    100.00",
		"Beta      25.50",
		"------ --------",
		"Total    125.50",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	tl, err := tbl.Layout(15)
	if err != nil {
		t.Fatal(err)
	}
	if want := []bool{false, false, true}; !slices.Equal(tl.Totals, want) {
		t.Errorf("Totals = %v, want %v", tl.Totals, want)
	}
}

func TestTable_ProseColumn(t *testing.T) {
	tbl := mustTable(t, "4L *P")
	tbl.Header("key", "meaning")
	tbl.Row("em", "the point size squared, the unit of measure")
	tbl.Row("box", "a rectangle")

	// Mono path: P lays out exactly as L.
	tlm, err := tbl.Layout(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(tlm.ProseCols) != 1 || tlm.ProseCols[0] != (ProseCol{Start: 5, End: 20}) {
		t.Errorf("ProseCols = %+v", tlm.ProseCols)
	}
	if tlm.RowProse[0] != nil {
		t.Error("mono Layout must not measure prose cells")
	}
	if !strings.Contains(tlm.Rows[0][0], "the point size") {
		t.Errorf("mono P cell not laid out as L: %q", tlm.Rows[0])
	}

	// Measured path: a wider measurer than mono (600 units/rune vs
	// Mono's 1) exercises real measuring; the formatted rows
	// reserve the cell blank at the measured height.
	tl, err := tbl.LayoutMeasured(20, Mono, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	lines := tl.RowProse[0][0]
	if len(lines) == 0 {
		t.Fatal("no measured prose lines")
	}
	if got := len(tl.Rows[0]); got != max(1, len(lines)) {
		t.Errorf("row height %d, measured lines %d", got, len(lines))
	}
	for _, physical := range tl.Rows[0] {
		if strings.Contains(physical, "point") {
			t.Errorf("measured P cell not blanked: %q", physical)
		}
	}
	// The mono cells still render on the grid.
	if !strings.HasPrefix(tl.Rows[0][0], "em") {
		t.Errorf("mono cell missing: %q", tl.Rows[0][0])
	}
}

func TestTable_NumColGeometry(t *testing.T) {
	tbl := mustTable(t, "*L 10N")
	tbl.Header("Client", "Amount")
	tbl.Row("Alpha", "1,234.56")
	tbl.Row("Gamma", "(2.00)")

	tl, err := tbl.Layout(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.NumCols) != 1 {
		t.Fatalf("NumCols = %+v, want one", tl.NumCols)
	}
	c := tl.NumCols[0]
	want := typesetNumCol()
	if c != want {
		t.Errorf("NumCol = %+v, want %+v", c, want)
	}
	// SepIndex names the rune cell the decimal points occupy in the
	// formatted lines.
	for _, row := range tl.Rows {
		if i := strings.LastIndex(row[0], "."); i != c.SepIndex() {
			t.Errorf("decimal at %d in %q, SepIndex = %d", i, row[0], c.SepIndex())
		}
	}
}

// typesetNumCol is the expected geometry for the table above: auto
// column 9 wide, N column at [10,20), frac ".56" = 3, paren present.
func typesetNumCol() NumCol {
	return NumCol{Start: 10, End: 20, Frac: 3, Paren: true}
}

func TestSplitNumeric(t *testing.T) {
	cases := []struct {
		in, intPart, tail string
		ok                bool
	}{
		{"1,234.56", "1,234", ".56", true},
		{"(2,340.10)", "(2,340", ".10)", true},
		{"(500)", "(500", ")", true},
		{"315", "315", "", true},
		{"n/a", "", "", false},
		{"Amount", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		intPart, tail, ok := SplitNumeric(c.in)
		if intPart != c.intPart || tail != c.tail || ok != c.ok {
			t.Errorf("SplitNumeric(%q) = %q, %q, %v; want %q, %q, %v",
				c.in, intPart, tail, ok, c.intPart, c.tail, c.ok)
		}
	}
}

func TestTable_NumericColumnIntegers(t *testing.T) {
	// No fractions, no parens: N degrades to plain right-align.
	tbl := mustTable(t, "3L 5N")
	tbl.Row("a", "100")
	tbl.Row("b", "7")
	got := render(t, tbl, 9)
	want := "a     100\nb       7"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTable_NoHeader(t *testing.T) {
	tbl := mustTable(t, "3L 4R")
	tbl.Row("Mon", "25")
	tbl.Row("Tue", "22")
	got := render(t, tbl, 8)
	want := "Mon   25\nTue   22"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTable_InvalidSpec(t *testing.T) {
	for _, spec := range []string{
		"",
		"3X",       // bad align
		"abc",      // bad width
		"3L *L *R", // two auto-span
	} {
		t.Run(spec, func(t *testing.T) {
			if _, err := NewTable(spec); err == nil {
				t.Errorf("expected error for spec %q", spec)
			}
		})
	}
	// Fit errors surface at layout time.
	tbl := mustTable(t, "50L 50L")
	if _, err := tbl.Layout(40); err == nil {
		t.Error("expected overflow error at Layout")
	}
}

func TestTable_RuneAware(t *testing.T) {
	// "λεμεσός" is 7 runes.
	tbl := mustTable(t, "7L")
	tbl.Row("λεμεσός")
	if got := render(t, tbl, 7); got != "λεμεσός" {
		t.Errorf("got %q", got)
	}
	tbl2 := mustTable(t, "4L!")
	tbl2.Row("λεμεσός")
	if got := render(t, tbl2, 4); got != "λεμε" {
		t.Errorf("got %q", got)
	}
}

func TestTable_CellsHyphenate(t *testing.T) {
	// At 17 runes, greedy wrapping needed 3 lines (Isolated /
	// thunderstorms / inland); Knuth-Plass with hyphenation fits 2.
	tbl := mustTable(t, "17L")
	tbl.Row("Isolated thunderstorms inland")
	tl, err := tbl.Layout(17)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Rows[0]) != 2 {
		t.Fatalf("cell set in %d lines, want 2 (hyphenated):\n%s",
			len(tl.Rows[0]), strings.Join(tl.Rows[0], "\n"))
	}
	if !strings.Contains(tl.Rows[0][0], "-") {
		t.Errorf("expected a hyphen break: %q", tl.Rows[0][0])
	}
	for _, ln := range tl.Rows[0] {
		if len([]rune(ln)) > 17 {
			t.Errorf("line exceeds cell width: %q", ln)
		}
	}
}
