package tbl

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"repani.com/typeset/format"
)

// gap is the blank columns between two columns.
const gap = 1

// Span is a stretch of grid columns, Start to End, the end exclusive.
type Span struct{ Start, End int }

// Num is one N column's geometry on the grid: its span, the widest
// fraction tail (the point included) in code points, and whether the
// column reserves the accounting paren slot. A host that sets numbers
// in a proportional face anchors them on SepIndex; on the monospace
// grid the padding already aligns them.
type Num struct {
	Span
	Frac  int
	Paren bool
}

// SepIndex is the grid column every decimal point in the column
// occupies (one past the units digit when the column has no
// fractions). Integer parts end there; fraction tails start there.
func (n Num) SepIndex() int {
	slot := 0
	if n.Paren {
		slot = 1
	}
	return n.End - slot - n.Frac
}

// SplitNumeric splits a numeric cell at its decimal point: intPart
// is everything before it (an opening paren included), tail
// everything from it on (a closing paren included). ok is false for
// content that does not read as a number.
func SplitNumeric(s string) (intPart, tail string, ok bool) {
	if !isNumeric(s) {
		return "", "", false
	}
	core := strings.TrimSuffix(s, ")")
	paren := ""
	if core != s {
		paren = ")"
	}
	if i := strings.LastIndex(core, "."); i >= 0 {
		return core[:i], core[i:] + paren, true
	}
	return core, paren, true
}

// isNumeric reports whether a cell reads as a number: at least one
// digit and only digits, grouping and sign punctuation, currency
// marks, or percent.
func isNumeric(s string) bool {
	hasDigit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case strings.ContainsRune(".,+-−()$€%", r):
		default:
			return false
		}
	}
	return hasDigit
}

// numericParts reduces SplitNumeric to the column metrics: the code
// points of the fraction tail (the point included, the closing paren
// not) and whether the cell is an accounting negative.
func numericParts(s string) (fracLen int, hasParen bool, ok bool) {
	_, tail, ok := SplitNumeric(s)
	if !ok {
		return 0, false, false
	}
	frac, hasParen := strings.CutSuffix(tail, ")")
	return utf8.RuneCountInString(frac), hasParen, true
}

// Grid is a table's columns fitted to a measure, with the decimal
// metrics of its N columns: the grid every row of the table is laid
// out on.
type Grid struct {
	spans []Span
	num   []bool // the column is N under some format of the table
	frac  []int
	paren []bool
}

// Spans returns every column's span.
func (g *Grid) Spans() []Span { return append([]Span(nil), g.spans...) }

// Nums returns the N columns' geometry, left to right.
func (g *Grid) Nums() []Num {
	var out []Num
	for i, sp := range g.spans {
		if g.num[i] {
			out = append(out, Num{Span: sp, Frac: g.frac[i], Paren: g.paren[i]})
		}
	}
	return out
}

// fit fits f's columns to a measure -- the narrowing when it is less,
// else the measure -- with gap columns between them: the auto column
// takes what the others and the gaps leave, at least 1; columns that
// cannot fit are an error. The grid has no N metrics yet.
func fit(f Format, measure int) (*Grid, error) {
	width := measure
	if f.Narrow > 0 && f.Narrow < measure {
		width = f.Narrow
	}
	widths := make([]int, len(f.Cols))
	fixed, auto := (len(f.Cols)-1)*gap, -1
	for i, c := range f.Cols {
		if c.Auto {
			auto = i
			continue
		}
		widths[i] = c.Width
		fixed += c.Width
	}
	switch {
	case auto >= 0 && width-fixed < 1:
		return nil, fmt.Errorf("%w: no room for the * column: %d columns need %d of %d", ErrFit, len(widths), fixed+1, width)
	case auto >= 0:
		widths[auto] = width - fixed
	case fixed > width:
		return nil, fmt.Errorf("%w: %d columns need %d of %d", ErrFit, len(widths), fixed, width)
	}
	g := &Grid{spans: make([]Span, len(widths)), num: make([]bool, len(widths)),
		frac: make([]int, len(widths)), paren: make([]bool, len(widths))}
	at := 0
	for i, w := range widths {
		g.spans[i] = Span{at, at + w}
		at += w + gap
	}
	return g, nil
}

// measure folds a cell of N column i into the column's metrics: the
// widest fraction tail sets where the point sits, and an accounting
// negative reserves the paren slot. A cell that is not a number is
// ignored.
func (g *Grid) measure(i int, s string) {
	fracLen, hasParen, ok := numericParts(s)
	if !ok {
		return
	}
	g.frac[i] = max(g.frac[i], fracLen)
	g.paren[i] = g.paren[i] || hasParen
}

// number sets s in N column i: padded on the right so that, once
// right-aligned, its decimal point falls in the column's point cell
// -- a short fraction padded out to the widest, the paren slot kept
// open on a cell that is not an accounting negative -- then cut and
// aligned to the column. Text that is not a number right-aligns at
// the units position; empty text stays empty. A fraction longer than
// any measured one cannot align on the point and sets flush.
func (g *Grid) number(i int, s string) string {
	w := g.spans[i].End - g.spans[i].Start
	if s == "" {
		return pad("", w, 'R')
	}
	slot := 0
	if g.paren[i] {
		slot = 1
	}
	fracLen, hasParen, ok := numericParts(s)
	n := g.frac[i] + slot
	if ok {
		n = g.frac[i] - fracLen + slot
		if hasParen {
			n--
		}
	}
	return pad(format.Trunc(s+strings.Repeat(" ", max(n, 0)), w), w, 'R')
}
