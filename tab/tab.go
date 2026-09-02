// Package tab sets text into tab stops: fixed columns on a
// monospace grid, one line per row, cells padded to their column
// and aligned left, right, centred, or on the decimal point. It
// is the typewriter's tab stop, not a table language: nothing
// wraps (a cell wider than its column is clipped), nothing rules
// or spans, and the package knows no document, page or font. A
// typesetter builds wrapped cells, headers and rules on top of it;
// a template pads a value into a column with it.
//
// A column spec is a run of tokens "<width><align>": the width in
// runes or "*" for the one column that takes what the measure
// leaves, and the align letter L, R, C or N. An N column aligns
// its cells on the decimal point, reserves a trailing slot for
// accounting negatives like "(1,234.56)" once any cell has one,
// and right-aligns non-numeric cells ("n/a", a header) at the
// units position. Columns are joined by a gap of blank cells, one
// by default.
//
// Widths count runes: one rune, one cell.
package tab

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrEmptySpec    = errors.New("tab: empty column spec")
	ErrInvalidToken = errors.New("tab: invalid column token")
	ErrInvalidAlign = errors.New("tab: column align must be L, R, C, or N")
	ErrAutoConflict = errors.New("tab: only one auto-span column allowed")
	ErrInvalidWidth = errors.New("tab: invalid column width")
	ErrAutoNoRoom   = errors.New("tab: no space left for auto-span column")
	ErrOverflow     = errors.New("tab: column widths exceed table width")
)

// Col is one column: its width in runes (resolved by Fit when
// Auto), whether it is the auto-span column, and its align letter.
type Col struct {
	Width int
	Auto  bool
	Align byte // 'L', 'R', 'C', or 'N'
}

// Parse reads a column spec ("6L 8L 8L *N") into columns. Widths
// are as written; an auto column has Width 0 until Fit.
func Parse(spec string) ([]Col, error) {
	tokens := strings.Fields(spec)
	if len(tokens) == 0 {
		return nil, ErrEmptySpec
	}
	cols := make([]Col, len(tokens))
	autoSeen := false
	for i, tok := range tokens {
		if len(tok) < 2 {
			return nil, fmt.Errorf("%w: %q", ErrInvalidToken, tok)
		}
		align := tok[len(tok)-1]
		if !strings.ContainsRune("LRCN", rune(align)) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidAlign, tok)
		}
		cols[i].Align = align
		widthPart := tok[:len(tok)-1]
		if widthPart == "*" {
			if autoSeen {
				return nil, ErrAutoConflict
			}
			autoSeen = true
			cols[i].Auto = true
			continue
		}
		w, err := strconv.Atoi(widthPart)
		if err != nil || w < 1 {
			return nil, fmt.Errorf("%w: %q", ErrInvalidWidth, tok)
		}
		cols[i].Width = w
	}
	return cols, nil
}

// Fit resolves the columns against a measure of width runes with
// gap blank cells between columns: the auto column takes what the
// fixed columns leave, and fixed columns that do not fit are an
// error. The input is not modified.
func Fit(cols []Col, width, gap int) ([]Col, error) {
	out := make([]Col, len(cols))
	copy(out, cols)
	fixedSum := 0
	autoIdx := -1
	for i, c := range out {
		if c.Auto {
			autoIdx = i
			continue
		}
		fixedSum += c.Width
	}
	gaps := (len(out) - 1) * gap
	if autoIdx >= 0 {
		remaining := width - fixedSum - gaps
		if remaining < 1 {
			return nil, ErrAutoNoRoom
		}
		out[autoIdx].Width = remaining
		out[autoIdx].Auto = false
	} else if fixedSum+gaps > width {
		return nil, ErrOverflow
	}
	return out, nil
}

// Span is one column's [Start,End) rune interval on a line.
type Span struct {
	Start, End int
}

// Num is one N column's resolved geometry: its Span, the widest
// fraction tail (separator included) in runes, and whether the
// column reserves the accounting paren slot. A renderer that sets
// numbers in a proportional face anchors on SepIndex; on the mono
// grid the alignment is already in the padded cells.
type Num struct {
	Span
	Frac  int
	Paren bool
}

// SepIndex is the rune cell every decimal point in the column
// occupies (one past the units digit when the column has no
// fractions). Integer parts end there; fraction tails start there.
func (n Num) SepIndex() int {
	slot := 0
	if n.Paren {
		slot = 1
	}
	return n.End - slot - n.Frac
}

// SplitNumeric splits a numeric cell at its decimal separator:
// intPart is everything before it (an opening paren included),
// tail everything from it on (a closing paren included). ok is
// false for content that does not read as a number.
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

// numericParts reduces SplitNumeric to the column metrics: the
// rune length of the fraction tail (separator included, closing
// paren excluded) and whether the cell is an accounting negative.
func numericParts(s string) (fracLen int, hasParen bool, ok bool) {
	_, tail, ok := SplitNumeric(s)
	if !ok {
		return 0, false, false
	}
	frac, hasParen := strings.CutSuffix(tail, ")")
	return utf8.RuneCountInString(frac), hasParen, true
}

// Grid is a set of resolved columns with a gap between them, plus
// the N-column metrics gathered by Measure.
type Grid struct {
	cols  []Col
	gap   int
	frac  []int  // parallel to cols; N columns only
	paren []bool // parallel to cols; N columns only
}

// New makes a grid over resolved columns (see Fit) with gap blank
// cells between them; a gap below one is one. An auto column that
// was never resolved is a programmer error.
func New(cols []Col, gap int) *Grid {
	for _, c := range cols {
		if c.Auto || c.Width < 1 {
			panic(fmt.Sprintf("tab: unresolved column %+v", c))
		}
	}
	return &Grid{cols: append([]Col(nil), cols...), gap: max(gap, 1),
		frac: make([]int, len(cols)), paren: make([]bool, len(cols))}
}

// Measure folds one row's cells into the N-column metrics: the
// widest fraction tail sets where the decimal point sits, and any
// accounting negative reserves the paren slot. Call it with every
// row whose numbers should align before formatting any of them;
// cells that do not read as numbers, and cells beyond the columns,
// are ignored.
func (g *Grid) Measure(cells []string) {
	for i, c := range g.cols {
		if c.Align != 'N' || i >= len(cells) {
			continue
		}
		fracLen, hasParen, ok := numericParts(cells[i])
		if !ok {
			continue
		}
		g.frac[i] = max(g.frac[i], fracLen)
		g.paren[i] = g.paren[i] || hasParen
	}
}

// Cols returns the resolved columns.
func (g *Grid) Cols() []Col { return append([]Col(nil), g.cols...) }

// Gap returns the blank cells between columns.
func (g *Grid) Gap() int { return g.gap }

// Width is the full line width: every column plus the gaps.
func (g *Grid) Width() int {
	w := (len(g.cols) - 1) * g.gap
	for _, c := range g.cols {
		w += c.Width
	}
	return w
}

// Spans returns every column's [Start,End) rune interval.
func (g *Grid) Spans() []Span {
	out := make([]Span, len(g.cols))
	start := 0
	for i, c := range g.cols {
		out[i] = Span{Start: start, End: start + c.Width}
		start += c.Width + g.gap
	}
	return out
}

// Nums returns the N columns' geometry, left to right.
func (g *Grid) Nums() []Num {
	var out []Num
	for i, sp := range g.Spans() {
		if g.cols[i].Align == 'N' {
			out = append(out, Num{Span: sp, Frac: g.frac[i], Paren: g.paren[i]})
		}
	}
	return out
}

// Cell formats s for column i: clipped to the column, aligned, and
// padded to exactly the column's width.
func (g *Grid) Cell(i int, s string) string {
	c := g.cols[i]
	if c.Align == 'N' {
		s = g.numeric(i, s)
	}
	s = clip(s, c.Width)
	pad := c.Width - utf8.RuneCountInString(s)
	switch c.Align {
	case 'R', 'N':
		return strings.Repeat(" ", pad) + s
	case 'C':
		left := pad / 2
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", pad-left)
	default:
		return s + strings.Repeat(" ", pad)
	}
}

// Line formats one row: each cell in its column, columns joined by
// the gap, trailing blanks removed. Missing cells are empty; cells
// beyond the columns are dropped.
func (g *Grid) Line(cells []string) string {
	parts := make([]string, len(g.cols))
	for i := range g.cols {
		var s string
		if i < len(cells) {
			s = cells[i]
		}
		parts[i] = g.Cell(i, s)
	}
	return strings.TrimRight(strings.Join(parts, strings.Repeat(" ", g.gap)), " ")
}

// Rule draws r across every column, the gaps blank: a header
// underline, a total rule.
func (g *Grid) Rule(r rune) string {
	parts := make([]string, len(g.cols))
	for i, c := range g.cols {
		parts[i] = strings.Repeat(string(r), c.Width)
	}
	return strings.Join(parts, strings.Repeat(" ", g.gap))
}

// numeric pads a cell's right side so that, once right-aligned,
// every number in the column has its decimal point in the same
// cell: short fractions pad out to the column's widest, and the
// paren slot stays open on cells that are not accounting
// negatives. Non-numeric cells right-align at the units position.
// Empty cells stay empty. A cell with a longer fraction than any
// measured row cannot be aligned on the point; it right-aligns
// flush instead of producing a negative pad.
func (g *Grid) numeric(i int, s string) string {
	if s == "" {
		return s
	}
	slot := 0
	if g.paren[i] {
		slot = 1
	}
	fracLen, hasParen, ok := numericParts(s)
	pad := g.frac[i] + slot
	if ok {
		pad = g.frac[i] - fracLen + slot
		if hasParen {
			pad--
		}
	}
	return s + strings.Repeat(" ", max(pad, 0))
}

// clip hard-cuts s to width runes.
func clip(s string, width int) string {
	if len(s) <= width {
		return s
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width])
}
