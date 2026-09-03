package stylebook

import (
	"strings"
	"testing"
	"text/template"
)

func render(t *testing.T, tmplText string, data any) string {
	t.Helper()
	tmpl, err := template.New("t").Funcs(Funcs()).Parse(tmplText)
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestFuncMap_Render(t *testing.T) {
	got := render(t,
		`Temp: {{round .Temp}}, Wind: {{decimal .Wind 1}}, Time: {{shortTime .Time}}, Date: {{shortDate .Date}}`,
		map[string]any{
			"Temp": 25.7,
			"Wind": 12.345,
			"Time": "14:30:25",
			"Date": "2026-04-10",
		})
	want := "Temp: 26, Wind: 12.3, Time: 14:30, Date: Fri 10"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFuncMap_AllFunctions(t *testing.T) {
	fm := Funcs()
	expected := []string{"round", "decimal", "trunc", "pad", "join", "shortTime", "shortDate", "dur", "cells"}
	for _, name := range expected {
		if _, ok := fm[name]; !ok {
			t.Errorf("missing function %q in Funcs", name)
		}
	}
}

func TestRound(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{25.7, "26"},
		{25.4, "25"},
		{-1.5, "-2"},
		{0.0, "0"},
		{100.0, "100"},
		{7, "7"},        // YAML whole numbers arrive as int
		{int64(8), "8"}, // and sometimes int64
	}
	for _, c := range cases {
		got, err := round(c.in)
		if err != nil || got != c.want {
			t.Errorf("round(%v) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := round("nope"); err == nil {
		t.Error("round of a string should error")
	}
}

func TestDecimal(t *testing.T) {
	cases := []struct {
		in   any
		n    int
		want string
	}{
		{25.726, 1, "25.7"},
		{25.0, 2, "25.00"},
		{0.0, 3, "0.000"},
		{27, 1, "27.0"}, // int widens
	}
	for _, c := range cases {
		got, err := decimal(c.in, c.n)
		if err != nil || got != c.want {
			t.Errorf("decimal(%v, %d) = %q, %v; want %q", c.in, c.n, got, err, c.want)
		}
	}
}

func TestTrunc(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"Hello world", 5, "Hello"},
		{"hi", 5, "hi"},
		{"", 5, ""},
		{"λεμεσός", 4, "λεμε"},
	}
	for _, c := range cases {
		if got := trunc(c.in, c.n); got != c.want {
			t.Errorf("trunc(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestShortTime(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"2026-04-10T14:30:00Z", "14:30"},
		{"14:30:25", "14:30"},
		{"14:30", "14:30"},
		{"not a time", "not a time"},
	}
	for _, c := range cases {
		if got := shortTime(c.in); got != c.want {
			t.Errorf("shortTime(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShortDate(t *testing.T) {
	if got := shortDate("2026-04-10"); got != "Fri 10" {
		t.Errorf("shortDate(2026-04-10) = %q, want %q", got, "Fri 10")
	}
	if got := shortDate("garbage"); got != "garbage" {
		t.Errorf("shortDate(garbage) = %q, want passthrough", got)
	}
}

func TestDur(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"30s", "30s"},
		{"5m", "5m"},
		{"90m", "1h"},
		{"2h", "2h"},
		{"36h", "1d"},
		{"1d", "1d"}, // unparseable by Go, passes through verbatim
	}
	for _, c := range cases {
		if got := dur(c.in); got != c.want {
			t.Errorf("dur(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPad(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"Mon", 5, "Mon  "},
		{"Hello world", 5, "Hello"},
		{"", 3, "   "},
	}
	for _, c := range cases {
		if got := pad(c.in, c.n); got != c.want {
			t.Errorf("pad(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestRows(t *testing.T) {
	rows := []any{
		map[string]any{"Spot": "Akrotiri", "Pts": 25},
		map[string]any{"Spot": "Kourion", "Pts": float64(1)},
	}
	got, err := Rows(rows, "Spot", "Pts")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0][0] != "Akrotiri" || got[0][1] != "25" || got[1][1] != "1" {
		t.Errorf("Rows = %v", got)
	}
	if _, err := Rows(rows, "Nope"); err == nil {
		t.Error("missing field should error")
	}
	if _, err := Rows("not-a-slice", "F"); err == nil {
		t.Error("non-slice rows should error")
	}
	if _, err := Rows(rows); err == nil {
		t.Error("no fields should error")
	}
	if _, err := Rows([]any{"x"}, "F"); err == nil {
		t.Error("non-object row should error")
	}
}

func TestCells(t *testing.T) {
	ferries := []any{
		map[string]any{"dep": "06:00", "to": "Lavrio", "vessel": "Marmari", "status": "on time"},
		map[string]any{"dep": "19:30", "to": "Kythnos", "vessel": "Makedon", "status": "cancelled"},
	}
	got, err := cells("6L 8L 8L *L", 34, ferries, "dep", "to", "vessel", "status")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"06:00 ", "Lavrio  ", "Marmari ", "on time  "},
		{"19:30 ", "Kythnos ", "Makedon ", "cancelled"},
	}
	for i := range want {
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("cell %d,%d = %q, want %q", i, j, got[i][j], want[i][j])
			}
		}
	}
	// Joined with a space, the row is a 34-column line.
	if ln := strings.Join(got[1], " "); len([]rune(ln)) != 34 {
		t.Errorf("joined row %q is %d wide", ln, len([]rune(ln)))
	}
	// Numeric columns align across the whole set; width 0 takes the
	// fixed widths.
	fees := []any{map[string]any{"n": "14"}, map[string]any{"n": "2.5"}}
	num, err := cells("6N", 0, fees, "n")
	if err != nil {
		t.Fatal(err)
	}
	if num[0][0] != "  14  " || num[1][0] != "   2.5" {
		t.Errorf("numeric cells = %q", num)
	}
	// Errors are loud: a bad spec, an auto column with no width,
	// a missing field.
	if _, err := cells("6X", 0, fees, "n"); err == nil {
		t.Error("bad spec should error")
	}
	if _, err := cells("*L", 0, fees, "n"); err == nil {
		t.Error("auto column without a width should error")
	}
	if _, err := cells("6L", 0, fees, "nope"); err == nil {
		t.Error("missing field should error")
	}
	// Through a template, with join.
	got2 := render(t, `{{range cells "3L 3R" 0 .R "a" "b"}}{{join . " "}}|{{end}}`,
		map[string]any{"R": []any{map[string]any{"a": "x", "b": "y"}}})
	if got2 != "x     y|" {
		t.Errorf("template cells = %q", got2)
	}
}
