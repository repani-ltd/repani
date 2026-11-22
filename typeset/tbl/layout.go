package tbl

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"repani.com/typeset/format"
	"repani.com/typeset/raster"
	"repani.com/typeset/wrap"
	"repani.com/typeset/wrap/hyphen"
)

// Table is the rows from one full format to the next, each with the
// resolved format it is under. A host starts a new Table at every
// full format.
type Table struct {
	entries []entry
	prev    int // the entry of the last data row, -1 after a blank or rule row
}

// entry is a row, the format it is under, and for a data row its
// boxes, resolved once, when Add checks the row.
type entry struct {
	fm    Format
	row   Row
	boxes []RowBox
}

// RowBox is a box of a data row as Add resolves it, before any
// measure: its format columns; Cont, whether it continues a box of the
// row above (^); Top and Rows, the row that began it and how many rows
// it covers -- 1 for a box that runs down no rows; Cell, the index of
// the row's cell it holds, or -1 for a box it continues; and the box's
// settings, resolved here and nowhere else -- the alignment and clip
// of its first column (a note's cells L and wrapping, whatever their
// columns), its colours (the cell's mark, else the format's row code,
// else the column's; 0 where none is set) and its link. A continuing
// box keeps the settings of the cell that began it. A host that draws
// rules draws none between the rows a box covers.
type RowBox struct {
	Box
	Cont      bool
	Top, Rows int
	Cell      int
	Align     byte
	Clip      bool
	FG, BG    raster.Color
	Target    string
}

// Add appends a row under its resolved format. It refuses a data row
// with more cells than its format has groups and a note row with no
// data row before it; and, a host's mistake, a format of another grid
// (every format of one table resolves from one full format) and a
// data row with no cells, which ParseRow never makes.
func (t *Table) Add(fm Format, r Row) error {
	if len(t.entries) == 0 {
		t.prev = -1
	}
	if len(t.entries) > 0 && !sameGrid(t.entries[0].fm, fm) {
		return errors.New("tbl: a row under a format of another grid; start a new Table at each full format")
	}
	if r.Kind == Data && len(r.Cells) == 0 {
		return errors.New("tbl: a data row with no cells")
	}
	if r.Kind == Data {
		if g := len(groups(fm)); len(r.Cells) > g {
			return errAt(r.Line, r.Cells[g].Col, ErrCells, "%d cells where the format has %d", len(r.Cells), g)
		}
		if r.Role == Note && !t.hasData() {
			return errAt(r.Line, r.Cells[0].Col, ErrNote, "a note annotates the row above it")
		}
	}
	e := entry{fm: fm, row: r}
	// A ^ continues the box of the data row above, a note's included;
	// a blank or rule row ends every box running down.
	if r.Kind == Data {
		var above []RowBox
		if t.prev >= 0 {
			above = t.entries[t.prev].boxes
		}
		boxes, own, err := rowBoxes(fm, r, above)
		if err != nil {
			return err
		}
		k, cell := len(t.entries), 0
		for i, b := range boxes {
			if own[i] {
				e.boxes = append(e.boxes, resolve(fm, r, cell, b, k))
				cell++
				continue
			}
			// The box above, continued: it and every row it covers
			// already cover one row more.
			rb := *findBox(above, b)
			for j := rb.Top; j < k; j++ {
				findBox(t.entries[j].boxes, b).Rows++
			}
			rb.Cont, rb.Cell, rb.Rows = true, -1, rb.Rows+1
			e.boxes = append(e.boxes, rb)
		}
		t.prev = k
	} else {
		t.prev = -1
	}
	t.entries = append(t.entries, e)
	return nil
}

// resolve is cell i of row r, the k-th row, in its own box b.
func resolve(f Format, r Row, i int, b Box, k int) RowBox {
	col := f.Cols[b.First]
	if r.Role == Note {
		col.Align, col.Clip = 'L', false
	}
	c := r.Cells[i]
	code := c.Code.Over(f.Row.Over(col.Code))
	return RowBox{Box: b, Top: k, Rows: 1, Cell: i, Align: col.Align, Clip: col.Clip, FG: code.FG, BG: code.BG, Target: c.Target}
}

// findBox is the box of boxes at b's columns; rowBoxes has made sure
// there is one.
func findBox(boxes []RowBox, b Box) *RowBox {
	for i := range boxes {
		if boxes[i].Box == b {
			return &boxes[i]
		}
	}
	panic("tbl: a continued box missing from the row above")
}

// Rows returns the table's rows in order.
func (t *Table) Rows() []Row {
	out := make([]Row, len(t.entries))
	for i, e := range t.entries {
		out[i] = e.row
	}
	return out
}

// Len is the number of rows the table holds.
func (t *Table) Len() int { return len(t.entries) }

// Format is the resolved format row i was added under.
func (t *Table) Format(i int) Format { return t.entries[i].fm }

// Boxes is row i's boxes in column order, nil for a blank or rule row:
// the table's structure, which no measure changes.
func (t *Table) Boxes(i int) []RowBox { return t.entries[i].boxes }

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
// the row in column order, and its boxes (Table.Boxes), parallel to
// every line. Blank and rule rows have no lines and no boxes.
type LaidRow struct {
	Kind   Kind
	Role   Role
	FG, BG raster.Color
	Lines  [][]Placed
	Boxes  []RowBox
}

// Placed is one cell's box on one line: grid columns Start to End
// (exclusive), Text of exactly End - Start code points, resolved
// colours, and the cell's link target or "" -- a raster segment, so
// a board's line of boxes is its row's segments as they stand.
type Placed = raster.Segment

// groups returns the format's groups as column ranges: a column that
// is neither S nor ^ begins one, and each S column after it joins it.
// A ^ column is in no group: it continues the box above, and the row
// gives it no cell.
func groups(f Format) [][2]int {
	var gs [][2]int
	for i, c := range f.Cols {
		switch c.Align {
		case '^':
		case 'S':
			gs[len(gs)-1][1] = i // the format refuses S first or after ^
		default:
			gs = append(gs, [2]int{i, i})
		}
	}
	return gs
}

// Box is where one cell of a row goes: its first and last format
// column, the gaps between them inside it.
type Box struct{ First, Last int }

// Single reports whether b is one column: only a single box takes an
// N column's decimal metrics, and only its number aligns on the
// column's point.
func (b Box) Single() bool { return b.First == b.Last }

// Boxes returns the boxes of a row of n cells under f, at most its
// groups: cell i fills group i, and the last cell of a short row runs
// to the last column, or to the column before the next ^ column.
func (f Format) Boxes(n int) []Box {
	gs := groups(f)
	out := make([]Box, n)
	for i := range n {
		first, last := gs[i][0], gs[i][1]
		if i == n-1 {
			last = len(f.Cols) - 1
			for c := gs[i][1] + 1; c < len(f.Cols); c++ {
				if f.Cols[c].Align == '^' {
					last = c - 1
					break
				}
			}
		}
		out[i] = Box{first, last}
	}
	return out
}

// carets reports whether f has a ^ column.
func (f Format) carets() bool {
	for _, c := range f.Cols {
		if c.Align == '^' {
			return true
		}
	}
	return false
}

// rowBoxes returns the boxes of a data row under f: its own cells'
// boxes and the boxes of above, the data row above's, that its ^
// columns continue, in column order, with own[i] true for a box of its
// own. It refuses ^ with no row above, ^ over part of a box above, and
// columns left in no box -- a short row stopping at a ^ with plain
// columns after it.
func rowBoxes(f Format, r Row, above []RowBox) (boxes []Box, own []bool, err error) {
	at := func(format string, args ...any) error {
		return errAt(r.Line, r.Cells[0].Col, ErrContinue, format, args...)
	}
	mine := f.Boxes(len(r.Cells))
	if !f.carets() {
		return mine, trues(len(mine)), nil
	}
	if above == nil {
		return nil, nil, at("^ continues the box above, and there is no data row above")
	}
	var cont []Box
	for _, b := range above {
		n := 0
		for c := b.First; c <= b.Last; c++ {
			if f.Cols[c].Align == '^' {
				n++
			}
		}
		switch {
		case n == b.Last-b.First+1:
			cont = append(cont, b.Box)
		case n > 0:
			return nil, nil, at("^ continues a whole box, and the box above runs over columns %d to %d", b.First+1, b.Last+1)
		}
	}
	i, j := 0, 0
	for i < len(mine) || j < len(cont) {
		if j == len(cont) || (i < len(mine) && mine[i].First < cont[j].First) {
			boxes, own = append(boxes, mine[i]), append(own, true)
			i++
		} else {
			boxes, own = append(boxes, cont[j]), append(own, false)
			j++
		}
	}
	next := 0
	for _, b := range boxes {
		if b.First != next {
			return nil, nil, at("column %d is in no box: a short row stops at a ^ column, so give the columns after it cells", next+1)
		}
		next = b.Last + 1
	}
	if next != len(f.Cols) {
		return nil, nil, at("column %d is in no box: a short row stops at a ^ column, so give the columns after it cells", next+1)
	}
	return boxes, own, nil
}

func trues(n int) []bool {
	out := make([]bool, n)
	for i := range out {
		out[i] = true
	}
	return out
}

// Columns fits the table's columns to a measure: the grid every row is
// laid on, with the decimal metrics of its N columns, for a host that
// places the boxes itself.
func (t *Table) Columns(measure int) (*Grid, error) {
	if len(t.entries) == 0 {
		return nil, errors.New("tbl: a table with no rows")
	}
	grid, err := fit(t.entries[0].fm, measure)
	if err != nil {
		return nil, err
	}
	// The N columns' decimal metrics, over the single boxes of data
	// and total rows under a format where the column is N.
	for _, e := range t.entries {
		for i, c := range e.fm.Cols {
			grid.num[i] = grid.num[i] || c.Align == 'N'
		}
		if e.row.Kind != Data || (e.row.Role != None && e.row.Role != Total) {
			continue
		}
		for _, b := range e.boxes {
			if !b.Cont && b.Single() && b.Align == 'N' {
				grid.measure(b.First, e.row.Cells[b.Cell].Text)
			}
		}
	}
	return grid, nil
}

// Layout lays the table out on a measure -- board's 40, a document's
// width for pica -- and returns the grid it laid the rows on and the
// rows. A host that draws numbers itself, anchored on the point in a
// proportional face, reads its N geometry from the grid.
func (t *Table) Layout(measure int) (*Grid, []LaidRow, error) {
	grid, err := t.Columns(measure)
	if err != nil {
		return nil, nil, err
	}
	// Every box is laid once, by the row whose cell it holds; a box
	// that runs down rows sets its lines down them. Rows are first as
	// tall as their boxes that run down no rows; then, in the order of
	// the row they end on, boxes that run down rows grow their last row
	// by what the rows they cover leave short; then every row's lines
	// are cut from its boxes' stacks.
	lines := make([][][]string, len(t.entries)) // per row, per box
	h := make([]int, len(t.entries))
	for k, e := range t.entries {
		lines[k] = make([][]string, len(e.boxes))
		h[k] = 1
		for i, b := range e.boxes {
			if b.Cont {
				lines[k][i] = lines[b.Top][topIndex(t.entries[b.Top].boxes, b.Box)]
				continue
			}
			if lines[k][i], err = layBox(b, e.row, grid); err != nil {
				return nil, nil, err
			}
			if b.Rows == 1 {
				h[k] = max(h[k], len(lines[k][i]))
			}
		}
	}
	type run struct{ from, to, n int }
	var runs []run
	for k, e := range t.entries {
		for i, b := range e.boxes {
			if !b.Cont && b.Rows > 1 {
				runs = append(runs, run{k, k + b.Rows - 1, len(lines[k][i])})
			}
		}
	}
	slices.SortStableFunc(runs, func(a, b run) int { return a.to - b.to })
	for _, r := range runs {
		have := 0
		for k := r.from; k <= r.to; k++ {
			have += h[k]
		}
		if need := r.n - have; need > 0 {
			h[r.to] += need
		}
	}
	out := make([]LaidRow, len(t.entries))
	for k, e := range t.entries {
		row := e.fm.Row
		lr := LaidRow{Kind: e.row.Kind, Role: e.row.Role, FG: row.FG, BG: row.BG, Boxes: e.boxes}
		if !row.HasFG {
			lr.FG = raster.Default
		}
		if !row.HasBG {
			lr.BG = raster.Default
		}
		if e.row.Kind == Data {
			// Where this row's first line falls in each box's lines: 0
			// for a box that begins here, the rows above for one that
			// runs down to it.
			off := make([]int, len(e.boxes))
			for i, b := range e.boxes {
				for j := b.Top; j < k; j++ {
					off[i] += h[j]
				}
			}
			lr.Lines = make([][]Placed, h[k])
			for l := range lr.Lines {
				line := make([]Placed, len(e.boxes))
				for i, b := range e.boxes {
					p := Placed{
						Start: grid.spans[b.First].Start, End: grid.spans[b.Last].End,
						FG: b.FG, BG: b.BG, Target: b.Target,
					}
					if at := off[i] + l; at < len(lines[k][i]) {
						p.Text = lines[k][i][at]
					} else {
						p.Text = strings.Repeat(" ", p.End-p.Start)
					}
					line[i] = p
				}
				lr.Lines[l] = line
			}
		}
		out[k] = lr
	}
	return grid, out, nil
}

// topIndex is the position among boxes of the box at b's columns.
func topIndex(boxes []RowBox, b Box) int {
	for i := range boxes {
		if boxes[i].Box == b {
			return i
		}
	}
	panic("tbl: a continued box missing from the row that began it")
}

// layBox lays the cell of row r that box b holds. A number in an N
// box wider than the box is an error: cutting it would show another
// number.
func layBox(b RowBox, r Row, grid *Grid) ([]string, error) {
	w := grid.spans[b.Last].End - grid.spans[b.First].Start
	c := r.Cells[b.Cell]
	text := c.Text
	if _, _, num := SplitNumeric(text); b.Align == 'N' && num && utf8.RuneCountInString(text) > w {
		return nil, errAt(r.Line, c.Col, ErrNumber, "%q in a box of %d", text, w)
	}
	switch {
	case b.Align == 'N' && b.Single():
		return []string{grid.number(b.First, text)}, nil
	case b.Align == 'N':
		return []string{pad(format.Trunc(text, w), w, 'R')}, nil
	case b.Clip:
		return []string{pad(format.Trunc(text, w), w, b.Align)}, nil
	}
	var lines []string
	for _, ln := range wrap.Cell(text, w, hyphen.Default) {
		lines = append(lines, pad(ln, w, b.Align))
	}
	return lines, nil
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
