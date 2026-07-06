package typeset

import (
	"strings"
	"testing"
)

// mustTable is a test helper: NewTable that fails the test on error.
func mustTable(t *testing.T, spec string, width int) *Table {
	t.Helper()
	tbl, err := NewTable(spec, width)
	if err != nil {
		t.Fatalf("NewTable(%q, %d): %v", spec, width, err)
	}
	return tbl
}

func TestTable_Basic(t *testing.T) {
	tbl := mustTable(t, "3L 5L 4R", 40)
	tbl.Header("Day", "Time", "Temp")
	tbl.Row("Mon", "09:00", "25")
	tbl.Row("Tue", "14:30", "22")

	got := tbl.Render()
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
	tbl := mustTable(t, "3L *L 4R", 40)
	tbl.Header("Day", "Forecast", "Temp")
	tbl.Row("Mon", "Sunny", "25")

	lines := strings.Split(tbl.Render(), "\n")
	for _, ln := range lines {
		if len([]rune(ln)) != 40 {
			t.Errorf("line not 40 chars: %q (len=%d)", ln, len([]rune(ln)))
		}
	}
}

func TestTable_AutoSpanMiddle(t *testing.T) {
	// 5 + 1 + auto + 1 + 4 + 1 + 6 = 40 -> auto = 22
	tbl := mustTable(t, "5L *L 4R 6R", 40)
	tbl.Header("Date", "Title", "Temp", "Wind")
	tbl.Row("Mon", "Sunny day in Limassol", "25", "12 NW")

	lines := strings.Split(tbl.Render(), "\n")
	for _, ln := range lines {
		if len([]rune(ln)) != 40 {
			t.Errorf("line not 40 chars: %q (len=%d)", ln, len([]rune(ln)))
		}
	}
}

func TestTable_Truncation(t *testing.T) {
	tbl := mustTable(t, "5L", 5)
	tbl.Row("This is too long")
	got := tbl.Render()
	if got != "This " {
		t.Errorf("got %q, want %q", got, "This ")
	}
}

func TestTable_Alignment(t *testing.T) {
	tbl := mustTable(t, "5L 5R 5C", 17)
	tbl.Row("L", "R", "C")
	got := tbl.Render()
	want := "L         R   C  "
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTable_NoHeader(t *testing.T) {
	tbl := mustTable(t, "3L 4R", 8)
	tbl.Row("Mon", "25")
	tbl.Row("Tue", "22")
	got := tbl.Render()
	want := "Mon   25\nTue   22"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTable_InvalidSpec(t *testing.T) {
	cases := []string{
		"",
		"3X",       // bad align
		"abc",      // bad width
		"3L *L *R", // two auto-span
		"50L 50L",  // doesn't fit in 40
	}
	for _, spec := range cases {
		t.Run(spec, func(t *testing.T) {
			if _, err := NewTable(spec, 40); err == nil {
				t.Errorf("expected error for spec %q", spec)
			}
		})
	}
}

func TestTable_RuneAware(t *testing.T) {
	// "λεμεσός" is 7 runes.
	tbl := mustTable(t, "7L", 7)
	tbl.Row("λεμεσός")
	got := tbl.Render()
	if got != "λεμεσός" {
		t.Errorf("got %q, want %q", got, "λεμεσός")
	}

	// Truncate to 4 runes.
	tbl = mustTable(t, "4L", 4)
	tbl.Row("λεμεσός")
	got = tbl.Render()
	if got != "λεμε" {
		t.Errorf("got %q, want %q", got, "λεμε")
	}
}
