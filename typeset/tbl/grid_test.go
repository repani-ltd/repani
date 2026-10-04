package tbl

import (
	"errors"
	"strings"
	"testing"
)

// grid fits spec's full format to measure, failing the test on error.
func grid(t *testing.T, spec string, measure int) *Grid {
	t.Helper()
	var f Formats
	fm, _ := apply(t, &f, spec)
	g, err := fit(fm, measure)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range fm.Cols {
		g.num[i] = c.Align == 'N'
	}
	return g
}

func TestFit(t *testing.T) {
	if g := grid(t, "6L *L 4R", 20); g.spans[1] != (Span{7, 15}) {
		t.Errorf("auto column %v, want 7..15", g.spans[1])
	}
	var f Formats
	auto, _ := apply(t, &f, "6L *L 4R")
	if _, err := fit(auto, 12); !errors.Is(err, ErrFit) {
		t.Errorf("no room for *: %v", err)
	}
	fixed, _ := apply(t, &f, "6L 4R")
	if _, err := fit(fixed, 10); !errors.Is(err, ErrFit) {
		t.Errorf("overflow: %v", err)
	}
	if g, err := fit(fixed, 11); err != nil || g.spans[1] != (Span{7, 11}) {
		t.Errorf("exact fit %v, %v", g, err)
	}
}

func TestGridNumeric(t *testing.T) {
	// Decimal points align on one cell; the paren slot is reserved
	// for the whole column once any cell is an accounting negative;
	// non-numeric cells right-align at the units position.
	g := grid(t, "12N", 12)
	rows := []string{"41,234.56", "1,102", "(2,340.10)", "n/a", "315.7"}
	for _, r := range rows {
		g.measure(0, r)
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
		if got := g.number(0, r); got != want[i] {
			t.Errorf("number(%q) = %q, want %q", r, got, want[i])
		}
	}
	n := g.Nums()
	if len(n) != 1 || n[0] != (Num{Span{0, 12}, 3, true}) {
		t.Errorf("Nums = %+v", n)
	}
	if n[0].SepIndex() != 8 {
		t.Errorf("SepIndex = %d, want 8", n[0].SepIndex())
	}
	for _, r := range rows[:3] {
		c := g.number(0, r)
		intPart, _, _ := SplitNumeric(r)
		if idx := strings.Index(c, intPart) + len(intPart); idx != 8 {
			t.Errorf("number(%q) = %q: point at %d, want 8", r, c, idx)
		}
	}
}

func TestGridNumericNoFractions(t *testing.T) {
	g := grid(t, "6N", 6)
	g.measure(0, "12")
	g.measure(0, "1,234")
	if got := g.number(0, "12"); got != "    12" {
		t.Errorf("number = %q", got)
	}
	if n := g.Nums()[0]; n.Frac != 0 || n.Paren || n.SepIndex() != 6 {
		t.Errorf("Num = %+v", n)
	}
	// A longer fraction than any measured row right-aligns flush.
	if got := g.number(0, "1.5"); got != "   1.5" {
		t.Errorf("unmeasured fraction = %q", got)
	}
	if got := g.number(0, ""); got != "      " {
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
