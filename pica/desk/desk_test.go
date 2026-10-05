package desk

import (
	"strings"
	"testing"

	"repani.com/pica"
)

func TestFuncs(t *testing.T) {
	fm := Funcs()
	for _, name := range []string{"round", "decimal", "trunc", "pad", "join", "shortTime", "shortDate", "dur", "table"} {
		if _, ok := fm[name]; !ok {
			t.Errorf("missing function %q in Funcs", name)
		}
	}
}

// TestRender_Valid: the desk's helpers resolve and the result
// is the generated source, newline terminated.
func TestRender_Valid(t *testing.T) {
	src, err := Render("bulletin", "Weather\n\nTemp {{round .t}} degrees.", map[string]any{"t": 21.6})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "Weather\n\nTemp 22 degrees.\n"; string(src) != want {
		t.Fatalf("Render = %q, want %q", src, want)
	}
}

// TestRender_InvalidDoc is the package's promise: a template that
// generates an invalid document is an error carrying the pica
// parse position, never a result.
func TestRender_InvalidDoc(t *testing.T) {
	src, err := Render("b", "T\n\n.bogus {{.x}}", map[string]any{"x": 1})
	if src != nil || err == nil {
		t.Fatalf("Render = %q, %v; want nil, parse error", src, err)
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("parse error carries no line number: %v", err)
	}
	if !strings.Contains(err.Error(), "b: rendered document:") {
		t.Fatalf("parse error not labelled as the template's output: %v", err)
	}
}

func TestTable_DataDriven(t *testing.T) {
	rows := []any{
		map[string]any{"Spot": "Akrotiri", "When": "Sat 14:00", "Kind": "High"},
		map[string]any{"Spot": "Kourion", "When": "Sun 06:00", "Kind": "Low"},
	}
	got, err := table("9L 9L 5L", "Spot | When | Level", rows, "Spot", "When", "Kind")
	if err != nil {
		t.Fatal(err)
	}
	want := ".table 9L 9L 5L\n" +
		"^Spot | When | Level\n" +
		"Akrotiri | Sat 14:00 | High\n" +
		"Kourion | Sun 06:00 | Low\n" +
		".end"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	// Headerless: an empty header.
	got, err = table("9L 5R", "", rows, "Spot", "Kind")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Level") || !strings.HasPrefix(got, ".table 9L 5R\nAkrotiri") {
		t.Errorf("headerless form wrong:\n%s", got)
	}

	// Numbers format via %v (JSON floats included).
	nrows := []any{map[string]any{"Rank": float64(1), "Pts": 25}}
	got, err = table("2R 3R", "# | Pts", nrows, "Rank", "Pts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "1 | 25") {
		t.Errorf("numeric cells wrong:\n%s", got)
	}

	// Errors are loud.
	if _, err := table("2R", "h", rows, "Nope"); err == nil {
		t.Error("missing field should error")
	}
	if _, err := table("2R", "h", "not-a-slice", "F"); err == nil {
		t.Error("non-slice rows should error")
	}
	if _, err := table("2R", "h", rows); err == nil {
		t.Error("no fields should error")
	}
}

func TestTable_DataIsNeverSyntax(t *testing.T) {
	// A value the language would read as a mark, a role or a cell
	// break is refused, naming the row and field; the same text away
	// from the first cell, or not leading, is fine.
	for _, tc := range []struct {
		a, b string
		ok   bool
	}{
		{"=sum", "x", false},
		{"..x", "x", false},
		{"^top", "x", false},
		{"---", "x", false},
		{"a", ":r", false},
		{"a", "@home", false},
		{"a|b", "x", false},
		{"x", "=sum", true},
		{"x", "a:b@c", true},
		{"x", "-- dash", true},
	} {
		rows := []any{map[string]any{"A": tc.a, "B": tc.b}}
		_, err := table("6L 6L", "", rows, "A", "B")
		if (err == nil) != tc.ok {
			t.Errorf("%q | %q: err = %v", tc.a, tc.b, err)
		}
		if err != nil && !strings.Contains(err.Error(), "row 0 field") {
			t.Errorf("error does not name the place: %v", err)
		}
	}
}

func TestTable_EndToEndThroughLanguage(t *testing.T) {
	// The helper's output must parse as a valid .table block.
	rows := []any{map[string]any{"A": "x", "B": "longer cell value here"}}
	blk, err := table("4L *L", "A | B", rows, "A", "B")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pica.Parse("T\n\n" + blk + "\n"); err != nil {
		t.Fatalf("helper emitted unparseable block: %v", err)
	}
}

// The helpers moved here with the desk: the vocabulary over data,
// missing keys, execution errors, rows and cells.
func TestVocabulary(t *testing.T) {
	src, err := Render("t", "T\n\nTemp {{round .Temp}}, wind {{decimal .Wind 1}}, at {{shortTime .Time}} on {{shortDate .Date}}, {{pad .Spot 8}}| {{trunc .Spot 3}} {{dur .Wait}}",
		map[string]any{"Temp": 25.7, "Wind": 12.345, "Time": "14:30:25", "Date": "2026-04-10", "Spot": "Kourion", "Wait": "90s"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "T\n\nTemp 26, wind 12.3, at 14:30 on Fri 10, Kourion | Kou 1m\n"; string(src) != want {
		t.Fatalf("Render = %q, want %q", src, want)
	}
	if src, err := Render("t", "T\n\nvalue {{.absent}} here.", map[string]any{}); err != nil || !strings.Contains(string(src), "value <no value> here.") {
		t.Fatalf("missing key: %q, %v", src, err)
	}
	if _, err := Render("t", "T\n\n{{round .s}}", map[string]any{"s": "not a number"}); err == nil {
		t.Fatal("Render accepted a helper type error")
	}
	if _, err := Render("broken.tmpl", "T {{if}}", nil); err == nil || !strings.Contains(err.Error(), "broken.tmpl") {
		t.Fatalf("template parse error = %v", err)
	}
	if got, _ := Funcs()["round"].(func(any) (string, error))(int64(8)); got != "8" {
		t.Errorf("round of int64 = %q", got)
	}
}

func TestRows(t *testing.T) {
	rows := []any{map[string]any{"Spot": "Akrotiri", "Pts": 25}, map[string]any{"Spot": "Kourion", "Pts": float64(1)}}
	got, err := Rows(rows, "Spot", "Pts")
	if err != nil || len(got) != 2 || got[0][0] != "Akrotiri" || got[0][1] != "25" || got[1][1] != "1" {
		t.Errorf("Rows = %v, %v", got, err)
	}
	for _, bad := range []func() error{
		func() error { _, err := Rows(rows, "Nope"); return err },
		func() error { _, err := Rows("not-a-slice", "F"); return err },
		func() error { _, err := Rows(rows); return err },
		func() error { _, err := Rows([]any{"x"}, "F"); return err },
	} {
		if bad() == nil {
			t.Error("Rows accepted bad input")
		}
	}
}
