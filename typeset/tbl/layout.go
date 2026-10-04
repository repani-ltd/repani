package tbl

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"repani.com/typeset/tab"
	"repani.com/typeset/wrap"
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
// with more cells than its format has groups, a note row with no data
// row before it, and a format of another grid, which is a host's
// mistake: every format of one table resolves from one full format.
func (t *Table) Add(fm Format, r Row) error {
	if len(t.entries) > 0 && !sameGrid(t.entries[0].fm, fm) {
		return errors.New("tbl: a row under a format of another grid; start a new Table at each full format")
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

// box is one cell's place: its first and last column, and whether it
// is a single column, which alone takes an N column's metrics.
type box struct {
	first, last int
	single      bool
}

// boxes returns the boxes of a row of n cells under f: cell i fills
// group i, and the last cell of a short row runs to the last column.
func boxes(f Format, n int) []box {
	gs := groups(f)
	out := make([]box, n)
	for i := range n {
		first, last := gs[i][0], gs[i][1]
		if i == n-1 {
			last = len(f.Cols) - 1
		}
		out[i] = box{first, last, first == last}
	}
	return out
}

// Layout lays the table out on a measure: board's 40, a document's
// width for pica.
func (t *Table) Layout(measure int) ([]LaidRow, error) {
	if len(t.entries) == 0 {
		return nil, nil
	}
	spans, err := fit(t.entries[0].fm, measure)
	if err != nil {
		return nil, err
	}

	// The N columns' decimal metrics, over the single boxes of data
	// and total rows under a format where the column is N.
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
		for i, b := range boxes(e.fm, len(e.row.Cells)) {
			if b.single && e.fm.Cols[b.first].Align == 'N' {
				cells[b.first] = e.row.Cells[i].Text
			}
		}
		grid.Measure(cells)
	}

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
			lr.Lines = layRow(e.fm, e.row, spans, grid)
		}
		out[k] = lr
	}
	return out, nil
}

// layRow lays one data row out as lines of boxes.
func layRow(f Format, r Row, spans []span, grid *tab.Grid) [][]Placed {
	bs := boxes(f, len(r.Cells))
	stacks := make([][]string, len(bs))
	height := 1
	for i, b := range bs {
		col := f.Cols[b.first]
		w := spans[b.last].end - spans[b.first].start
		text := r.Cells[i].Text
		switch {
		case col.Align == 'N' && b.single:
			stacks[i] = []string{grid.Cell(b.first, text)}
		case col.Align == 'N':
			stacks[i] = []string{pad(cut(text, w), w, 'R')}
		case col.Clip:
			stacks[i] = []string{pad(cut(text, w), w, col.Align)}
		default:
			for _, ln := range wrap.Cell(text, w) {
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
			code := c.Code.Over(f.Row.Over(f.Cols[b.first].Code))
			p := Placed{
				Start:  spans[b.first].start,
				End:    spans[b.last].end,
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
	return lines
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
