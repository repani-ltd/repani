package main

import (
	"strings"
	"testing"
	"text/template"
)

func render(t *testing.T, tmplText string, data any) string {
	t.Helper()
	tmpl, err := template.New("t").Funcs(funcMap()).Parse(tmplText)
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
	fm := funcMap()
	expected := []string{"round", "decimal", "trunc", "pad", "shortTime", "shortDate", "dur", "wrap", "justify", "truncl"}
	for _, name := range expected {
		if _, ok := fm[name]; !ok {
			t.Errorf("missing function %q in funcMap", name)
		}
	}
}

func TestLayoutHelpers_WidthFirstAndPipeline(t *testing.T) {
	long := strings.Repeat("alpha beta gamma delta ", 5)
	data := map[string]any{"body": long}

	direct := render(t, `{{wrap 20 .body}}`, data)
	piped := render(t, `{{.body | wrap 20}}`, data)
	if direct != piped {
		t.Error("direct and pipeline wrap calls differ")
	}
	for _, ln := range strings.Split(direct, "\n") {
		if len([]rune(ln)) > 20 {
			t.Errorf("wrapped line exceeds 20 runes: %q", ln)
		}
	}

	justified := render(t, `{{justify 20 .body}}`, data)
	lines := strings.Split(justified, "\n")
	for _, ln := range lines[:len(lines)-1] {
		if len([]rune(ln)) != 20 {
			t.Errorf("justified line not 20 runes: %q", ln)
		}
	}

	cut := render(t, `{{truncl 5 .body}}`, data)
	for _, ln := range strings.Split(cut, "\n") {
		if len([]rune(ln)) > 5 {
			t.Errorf("truncl line exceeds 5 runes: %q", ln)
		}
	}
}

func TestLayoutHelpers_BadWidthErrors(t *testing.T) {
	tmpl, err := template.New("t").Funcs(funcMap()).Parse(`{{wrap 0 .body}}`)
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, map[string]any{"body": "x"}); err == nil {
		t.Fatal("wrap 0 should be a template error")
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
