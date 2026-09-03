package tab

import (
	"errors"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cols, err := Parse("6L *N 4R 3C")
	if err != nil {
		t.Fatal(err)
	}
	want := []Col{{6, false, 'L'}, {0, true, 'N'}, {4, false, 'R'}, {3, false, 'C'}}
	if len(cols) != len(want) {
		t.Fatalf("cols = %+v", cols)
	}
	for i := range want {
		if cols[i] != want[i] {
			t.Errorf("col %d = %+v, want %+v", i, cols[i], want[i])
		}
	}
	for spec, wantErr := range map[string]error{
		"":         ErrEmptySpec,
		"L":        ErrInvalidToken,
		"5P":       ErrInvalidAlign,
		"5L!":      ErrInvalidAlign,
		"*L *R":    ErrAutoConflict,
		"0L":       ErrInvalidWidth,
		"xL":       ErrInvalidWidth,
		"5L 12345": ErrInvalidAlign,
	} {
		if _, err := Parse(spec); !errors.Is(err, wantErr) {
			t.Errorf("Parse(%q) err = %v, want %v", spec, err, wantErr)
		}
	}
}

func TestFit(t *testing.T) {
	cols, _ := Parse("6L *L 4R")
	got, err := Fit(cols, 20, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got[1].Width != 8 || got[1].Auto {
		t.Errorf("auto column = %+v, want width 8 resolved", got[1])
	}
	if cols[1].Width != 0 {
		t.Error("Fit modified its input")
	}
	if _, err := Fit(cols, 12, 1); !errors.Is(err, ErrAutoNoRoom) {
		t.Errorf("no room err = %v", err)
	}
	fixed, _ := Parse("6L 4R")
	if _, err := Fit(fixed, 10, 1); !errors.Is(err, ErrOverflow) {
		t.Errorf("overflow err = %v", err)
	}
	if got, err := Fit(fixed, 11, 1); err != nil || got[0].Width != 6 {
		t.Errorf("exact fit = %+v, %v", got, err)
	}
	// A wider gap counts against the measure.
	if _, err := Fit(fixed, 11, 2); !errors.Is(err, ErrOverflow) {
		t.Errorf("gap 2 overflow err = %v", err)
	}
}

func TestGridAlign(t *testing.T) {
	cols, _ := Parse("5L 5R 5C")
	g := New(cols, 1)
	if got := g.Line([]string{"ab", "ab", "ab"}); got != "ab       ab  ab" {
		t.Errorf("Line = %q", got)
	}
	// Centre: the odd pad goes right; missing cells are blank;
	// extra cells drop; trailing blanks trim.
	if got := g.Line([]string{"", "", "abcd", "extra"}); got != "            abcd" {
		t.Errorf("Line = %q", got)
	}
	// Clipping, counted in runes.
	if got := g.Cell(0, "αβγδεζη"); got != "αβγδε" {
		t.Errorf("clip = %q", got)
	}
	if got := g.Cell(2, "αβ"); got != " αβ  " {
		t.Errorf("centre = %q", got)
	}
	if g.Width() != 17 {
		t.Errorf("Width = %d", g.Width())
	}
	if sp := g.Spans(); sp[2] != (Span{12, 17}) {
		t.Errorf("Spans = %v", sp)
	}
	if got := g.Rule('-'); got != "----- ----- -----" {
		t.Errorf("Rule = %q", got)
	}
	if got := New(cols, 2).Rule('─'); got != "─────  ─────  ─────" {
		t.Errorf("Rule gap 2 = %q", got)
	}
}

func TestGridNumeric(t *testing.T) {
	// Decimal points align on one cell; the paren slot is reserved
	// for the whole column once any cell is an accounting negative;
	// non-numeric cells right-align at the units position.
	cols, _ := Parse("12N")
	g := New(cols, 1)
	rows := []string{"41,234.56", "1,102", "(2,340.10)", "n/a", "315.7"}
	for _, r := range rows {
		g.Measure([]string{r})
	}
	// Twelve cells each: the point at cell 8, the paren slot last.
	want := []string{
		"  41,234.56 ",
		"   1,102    ",
		"  (2,340.10)",
		"     n/a    ",
		"     315.7  ",
	}
	for i, r := range rows {
		if got := g.Cell(0, r); got != want[i] {
			t.Errorf("Cell(%q) = %q, want %q", r, got, want[i])
		}
	}
	n := g.Nums()
	if len(n) != 1 || n[0] != (Num{Span{0, 12}, 3, true}) {
		t.Errorf("Nums = %+v", n)
	}
	if n[0].SepIndex() != 8 {
		t.Errorf("SepIndex = %d, want 8", n[0].SepIndex())
	}
	// Every number's separator sits at SepIndex.
	for _, r := range rows[:3] {
		c := g.Cell(0, r)
		intPart, _, _ := SplitNumeric(r)
		if idx := strings.Index(c, intPart) + len(intPart); idx != 8 {
			t.Errorf("Cell(%q) = %q: separator at %d, want 8", r, c, idx)
		}
	}
}

func TestGridNumericNoFractions(t *testing.T) {
	cols, _ := Parse("6N")
	g := New(cols, 1)
	g.Measure([]string{"12"})
	g.Measure([]string{"1,234"})
	if got := g.Cell(0, "12"); got != "    12" {
		t.Errorf("Cell = %q", got)
	}
	if n := g.Nums()[0]; n.Frac != 0 || n.Paren || n.SepIndex() != 6 {
		t.Errorf("Num = %+v", n)
	}
	// A longer fraction than any measured row right-aligns flush.
	if got := g.Cell(0, "1.5"); got != "   1.5" {
		t.Errorf("unmeasured fraction = %q", got)
	}
	if got := g.Cell(0, ""); got != "      " {
		t.Errorf("empty numeric cell = %q", got)
	}
}

func TestSplitNumeric(t *testing.T) {
	for _, c := range []struct {
		in, intPart, tail string
		ok                bool
	}{
		{"1,234.56", "1,234", ".56", true},
		{"(2,340.10)", "(2,340", ".10)", true},
		{"12", "12", "", true},
		{"(12)", "(12", ")", true},
		{"-3.5%", "-3", ".5%", true},
		{"n/a", "", "", false},
		{"", "", "", false},
		{"€", "", "", false},
	} {
		i, tl, ok := SplitNumeric(c.in)
		if i != c.intPart || tl != c.tail || ok != c.ok {
			t.Errorf("SplitNumeric(%q) = %q, %q, %v", c.in, i, tl, ok)
		}
	}
}

func TestNewPanicsOnUnresolved(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New accepted an auto column")
		}
	}()
	cols, _ := Parse("*L")
	New(cols, 1)
}
