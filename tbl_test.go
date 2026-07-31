package typeset

import (
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

	for _, ln := range strings.Split(render(t, tbl, 40), "\n") {
		if len([]rune(ln)) > 40 {
			t.Errorf("line exceeds 40 chars: %q", ln)
		}
	}
	// The same table lays out at another width.
	for _, ln := range strings.Split(render(t, tbl, 28), "\n") {
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
