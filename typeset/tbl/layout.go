package tbl

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"repani.com/typeset/tab"
	"repani.com/typeset/wrap"
	"repani.com/typeset/wrap/hyphen"
)

// gap is the blank columns between two columns.
const gap = 1

// Table is the rows from one full format to the next, each with the
// resolved format it is under. A host starts a new Table at every
// full format.
type Table struct {
	entries []entry
}

type entry struct {
	fm  Format
	row Row
}

// Add appends a row under its resolved format. It refuses a data row
// with more cells than its format has groups and a note row with no
// data row before it; and, a host's mistake, a format of another grid
// (every format of one table resolves from one full format) and a
// data row with no cells, which ParseRow never makes.
func (t *Table) Add(fm Format, r Row) error {
	if len(t.entries) > 0 && !sameGrid(t.entries[0].fm, fm) {
		return errors.New("tbl: a row under a format of another grid; start a new Table at each full format")
	}
	if r.Kind == Data && len(r.Cells) == 0 {
		return errors.New("tbl: a data row with no cells")
	}
	if r.Kind == Data {
		if g := len(groups(fm)); len(r.Cells) > g {
			return errAt(r.Cells[g].Col, ErrCells, "%d cells where the format has %d", len(r.Cells), g)
		}
		if r.Role == Note && !t.hasData() {
			return errAt(r.Cells[0].Col, ErrNote, "a note annotates the row above it")
		}
	}
	t.entries = append(t.entries, entry{fm, r})
	return nil
}

func (t *Table) hasData() bool {
	for _, e := range t.entries {
		if e.row.Kind == Data {
			return true
		}
	}
	return false
}

func sameGrid(a, b Format) bool {
	if a.Narrow != b.Narrow || len(a.Cols) != len(b.Cols) {
		return false
	}
	for i := range a.Cols {
		if a.Cols[i].Width != b.Cols[i].Width || a.Cols[i].Auto != b.Cols[i].Auto {
			return false
		}
	}
	return true
}

// LaidRow is one row laid out: its kind and role, its resolved row
// colours -- for the gaps between boxes and the width the format
// leaves -- and, for a data row, its lines, each holding every box of
// the row in column order. Blank and rule rows have no lines.
type LaidRow struct {
	Kind   Kind
	Role   Role
	FG, BG Color
	Lines  [][]Placed
}

// Placed is one cell's box on one line: grid columns Start to End
// (exclusive), Text of exactly End - Start code points, resolved
// colours, and the cell's link target or "".
type Placed struct {
	Start, End int
	Text       string
	FG, BG     Color
	Target     string
}

// Fit reports whether f's columns fit a measure: the width is the
// narrowing when it is less than the measure, else the measure.
func (f Format) Fit(measure int) error {
	_, err := fit(f, measure)
	return err
}

// span is a column's grid columns, end exclusive.
type span struct{ start, end int }

// fit resolves f's column widths against measure and returns each
// column's span.
func fit(f Format, measure int) ([]span, error) {
	width := measure
	if f.Narrow > 0 && f.Narrow < measure {
		width = f.Narrow
	}
	cols := make([]tab.Col, len(f.Cols))
	for i, c := range f.Cols {
		cols[i] = tab.Col{Width: c.Width, Auto: c.Auto, Align: 'L'}
	}
	fitted, err := tab.Fit(cols, width, gap)
	if err != nil {
		return nil, fmt.Errorf("%w: %d columns in a width of %d: %v", ErrFit, len(cols), width, err)
	}
	spans := make([]span, len(fitted))
	at := 0
	for i, c := range fitted {
		spans[i] = span{at, at + c.Width}
		at += c.Width + gap
	}
	return spans, nil
}

// groups returns the format's groups as column ranges: a column that
// is not S begins one, and each S column after it joins it.
func groups(f Format) [][2]int {
	var gs [][2]int
	for i, c := range f.Cols {
		if c.Align == 'S' && len(gs) > 0 {
			gs[len(gs)-1][1] = i
			continue
		}
		gs = append(gs, [2]int{i, i})
	}
	return gs
}

// Box is where one cell of a row goes: its first and last format
// column, the gaps between them inside it.
type Box struct{ First, Last int }

// single reports whether b is one column, which alone takes an N
// column's metrics.
func (b Box) single() bool { return b.First == b.Last }

// Boxes returns the boxes of a row of n cells under f, at most its
// groups: cell i fills group i, and the last cell of a short row runs
// to the last column.
func (f Format) Boxes(n int) []Box {
	gs := groups(f)
	out := make([]Box, n)
	for i := range n {
		first, last := gs[i][0], gs[i][1]
		if i == n-1 {
			last = len(f.Cols) - 1
		}
		out[i] = Box{first, last}
	}
	return out
}

// Grid fits the table's columns to a measure and gathers the N
// columns' decimal metrics, over the single boxes of data and total
// rows under a format where the column is N: the grid every row is
// laid out on. A host that draws numbers itself -- anchored on the
// point in a proportional face -- reads its N geometry here.
func (t *Table) Grid(measure int) (*tab.Grid, error) {
	if len(t.entries) == 0 {
		return nil, errors.New("tbl: a table with no rows has no grid")
	}
	spans, err := fit(t.entries[0].fm, measure)
	if err != nil {
		return nil, err
	}
	tcols := make([]tab.Col, len(spans))
	for i, s := range spans {
		tcols[i] = tab.Col{Width: s.end - s.start, Align: 'L'}
		for _, e := range t.entries {
			if e.fm.Cols[i].Align == 'N' {
				tcols[i].Align = 'N'
			}
		}
	}
	grid := tab.New(tcols, gap)
	for _, e := range t.entries {
		if e.row.Kind != Data || (e.row.Role != None && e.row.Role != Total) {
			continue
		}
		cells := make([]string, len(spans))
		for i, b := range e.fm.Boxes(len(e.row.Cells)) {
			if b.single() && e.fm.Cols[b.First].Align == 'N' {
				cells[b.First] = e.row.Cells[i].Text
			}
		}
		grid.Measure(cells)
	}
	return grid, nil
}

// Layout lays the table out on a measure: board's 40, a document's
// width for pica.
func (t *Table) Layout(measure int) ([]LaidRow, error) {
	if len(t.entries) == 0 {
		return nil, nil
	}
	grid, err := t.Grid(measure)
	if err != nil {
		return nil, err
	}
	spans := grid.Spans()

	out := make([]LaidRow, len(t.entries))
	for k, e := range t.entries {
		row := e.fm.Row
		lr := LaidRow{Kind: e.row.Kind, Role: e.row.Role, FG: row.FG, BG: row.BG}
		if !row.HasFG {
			lr.FG = Default
		}
		if !row.HasBG {
			lr.BG = Default
		}
		if e.row.Kind == Data {
			if lr.Lines, err = layRow(e.fm, e.row, spans, grid); err != nil {
				return nil, err
			}
		}
		out[k] = lr
	}
	return out, nil
}

// layRow lays one data row out as lines of boxes. A number in an N
// box wider than the box is an error: cutting it would show another
// number.
func layRow(f Format, r Row, spans []tab.Span, grid *tab.Grid) ([][]Placed, error) {
	bs := f.Boxes(len(r.Cells))
	stacks := make([][]string, len(bs))
	height := 1
	for i, b := range bs {
		col := f.Cols[b.First]
		w := spans[b.Last].End - spans[b.First].Start
		text := r.Cells[i].Text
		if _, _, num := tab.SplitNumeric(text); col.Align == 'N' && num && utf8.RuneCountInString(text) > w {
			return nil, errAt(r.Cells[i].Col, ErrNumber, "%q in a box of %d", text, w)
		}
		switch {
		case col.Align == 'N' && b.single():
			stacks[i] = []string{grid.Cell(b.First, text)}
		case col.Align == 'N':
			stacks[i] = []string{pad(cut(text, w), w, 'R')}
		case col.Clip:
			stacks[i] = []string{pad(cut(text, w), w, col.Align)}
		default:
			for _, ln := range wrap.Cell(text, w, hyphen.Default) {
				stacks[i] = append(stacks[i], pad(ln, w, col.Align))
			}
		}
		height = max(height, len(stacks[i]))
	}
	lines := make([][]Placed, height)
	for h := range lines {
		line := make([]Placed, len(bs))
		for i, b := range bs {
			c := r.Cells[i]
			code := c.Code.Over(f.Row.Over(f.Cols[b.First].Code))
			p := Placed{
				Start:  spans[b.First].Start,
				End:    spans[b.Last].End,
				FG:     code.FG,
				BG:     code.BG,
				Target: c.Target,
			}
			if h < len(stacks[i]) {
				p.Text = stacks[i][h]
			} else {
				p.Text = strings.Repeat(" ", p.End-p.Start)
			}
			line[i] = p
		}
		lines[h] = line
	}
	return lines, nil
}

// cut returns at most w code points of s.
func cut(s string, w int) string {
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	return string([]rune(s)[:w])
}

// pad pads s, at most w code points, to exactly w by its alignment:
// right for R, centred with the odd space on the right for C, left
// for everything else.
func pad(s string, w int, align byte) string {
	n := w - utf8.RuneCountInString(s)
	switch align {
	case 'R':
		return strings.Repeat(" ", n) + s
	case 'C':
		return strings.Repeat(" ", n/2) + s + strings.Repeat(" ", n-n/2)
	}
	return s + strings.Repeat(" ", n)
}
