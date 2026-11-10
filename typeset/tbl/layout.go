package tbl

import (
	"errors"
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
	prev    []Box // the boxes of the last data row, nil after a blank or rule row
}

// entry is a row, the format it is under, and for a data row its
// boxes in column order, own[i] false for a box it continues (^) --
// found once, when Add checks the row, and laid by Layout.
type entry struct {
	fm    Format
	row   Row
	boxes []Box
	own   []bool
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
			return errAt(r.Line, r.Cells[g].Col, ErrCells, "%d cells where the format has %d", len(r.Cells), g)
		}
		if r.Role == Note && !t.hasData() {
			return errAt(r.Line, r.Cells[0].Col, ErrNote, "a note annotates the row above it")
		}
	}
	// A ^ continues the box of the data row above, a note's included;
	// a blank or rule row ends every span.
	e := entry{fm: fm, row: r}
	if r.Kind == Data {
		var err error
		if e.boxes, e.own, err = rowBoxes(fm, r, t.prev); err != nil {
			return err
		}
	}
	t.prev = e.boxes
	t.entries = append(t.entries, e)
	return nil
}

// Rows returns the table's rows in order.
func (t *Table) Rows() []Row {
	out := make([]Row, len(t.entries))
	for i, e := range t.entries {
		out[i] = e.row
	}
	return out
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
	FG, BG raster.Color
	Lines  [][]Placed
	// Cont holds the boxes of this row that continue a box of the row
	// above (^ columns), in column order: their lines carry on the
	// text of the cell that began the box, and a host that draws
	// rules draws none between them and the box above.
	Cont []Box
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
// boxes and the boxes of prev, the boxes of the data row above, that
// its ^ columns continue, in column order, with own[i] true for a box
// of its own. It refuses ^ with no row above, ^ over part of a box
// above, and columns left in no box -- a short row stopping at a ^
// with plain columns after it.
func rowBoxes(f Format, r Row, prev []Box) (boxes []Box, own []bool, err error) {
	at := func(format string, args ...any) error {
		return errAt(r.Line, r.Cells[0].Col, ErrVSpan, format, args...)
	}
	mine := f.Boxes(len(r.Cells))
	if !f.carets() {
		return mine, trues(len(mine)), nil
	}
	if prev == nil {
		return nil, nil, at("^ continues the box above, and there is no data row above")
	}
	var cont []Box
	for _, b := range prev {
		n := 0
		for c := b.First; c <= b.Last; c++ {
			if f.Cols[c].Align == '^' {
				n++
			}
		}
		switch {
		case n == b.Last-b.First+1:
			cont = append(cont, b)
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

// Layout lays the table out on a measure -- board's 40, a document's
// width for pica -- and returns the grid it laid the rows on and the
// rows. A host that draws numbers itself, anchored on the point in a
// proportional face, reads its N geometry from the grid.
func (t *Table) Layout(measure int) (*Grid, []LaidRow, error) {
	if len(t.entries) == 0 {
		return nil, nil, errors.New("tbl: a table with no rows")
	}
	grid, err := fit(t.entries[0].fm, measure)
	if err != nil {
		return nil, nil, err
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
		for i, b := range e.fm.Boxes(len(e.row.Cells)) {
			if b.Single() && e.fm.Cols[b.First].Align == 'N' {
				grid.measure(b.First, e.row.Cells[i].Text)
			}
		}
	}
	// A box is laid once, by the row whose cell begins it; a box that
	// later rows continue (^) is a span, whose lines run down the rows
	// it covers. Rows are first laid on their own boxes, a span's first
	// box not counting towards its row's height; then the last row of
	// each span grows by what the rows it covers leave short; then
	// every row's lines are cut from its boxes' stacks.
	type laid struct {
		boxes []box
		h     int
	}
	rows := make([]laid, len(t.entries))
	var spans []*span
	var prev []box
	for k, e := range t.entries {
		if e.row.Kind != Data {
			prev = nil
			continue
		}
		var cur []box
		cell := 0
		for i, b := range e.boxes {
			if !e.own[i] {
				p := find(prev, b)
				if p.span == nil {
					p.span = &span{lines: p.lines, from: p.row}
					spans = append(spans, p.span)
				}
				p.span.to = k
				cur = append(cur, box{Box: b, span: p.span, placed: p.placed, row: k})
				continue
			}
			x, err := layBox(e.fm, e.row, cell, b, grid)
			if err != nil {
				return nil, nil, err
			}
			x.row = k
			cur = append(cur, x)
			cell++
		}
		rows[k].boxes = cur
		prev = cur
	}
	for k := range rows {
		rows[k].h = 1
		for _, b := range rows[k].boxes {
			if b.span == nil {
				rows[k].h = max(rows[k].h, len(b.lines))
			}
		}
	}
	for _, s := range spans {
		have := 0
		for k := s.from; k <= s.to; k++ {
			have += rows[k].h
		}
		if need := len(s.lines) - have; need > 0 {
			rows[s.to].h += need
		}
	}
	out := make([]LaidRow, len(t.entries))
	for k, e := range t.entries {
		row := e.fm.Row
		lr := LaidRow{Kind: e.row.Kind, Role: e.row.Role, FG: row.FG, BG: row.BG}
		if !row.HasFG {
			lr.FG = raster.Default
		}
		if !row.HasBG {
			lr.BG = raster.Default
		}
		if e.row.Kind == Data {
			// Each box's lines, and where this row's first line falls
			// in them: 0 for its own, the rows above it for a span's.
			bs := rows[k].boxes
			src, off := make([][]string, len(bs)), make([]int, len(bs))
			for i, b := range bs {
				src[i] = b.lines
				if b.span != nil {
					src[i] = b.span.lines
					for j := b.span.from; j < k; j++ {
						off[i] += rows[j].h
					}
				}
			}
			lr.Lines = make([][]Placed, rows[k].h)
			for h := range lr.Lines {
				line := make([]Placed, len(bs))
				for i, b := range bs {
					p := b.placed
					if at := off[i] + h; at < len(src[i]) {
						p.Text = src[i][at]
					} else {
						p.Text = strings.Repeat(" ", p.End-p.Start)
					}
					line[i] = p
				}
				lr.Lines[h] = line
			}
			for _, b := range rows[k].boxes {
				if b.span != nil && b.row != b.span.from {
					lr.Cont = append(lr.Cont, b.Box)
				}
			}
		}
		out[k] = lr
	}
	return grid, out, nil
}

// box is one box of a laid row: where it is, its cell's lines and the
// rest of its placing (columns, colours, link), and, when it begins or
// continues a span, the span.
type box struct {
	Box
	lines  []string
	placed Placed
	span   *span
	row    int // the entry the box is in
}

// span is a box that rows below continue: its cell's lines, and the
// first and last rows it covers.
type span struct {
	lines    []string
	from, to int
}

func find(bs []box, b Box) *box {
	for i := range bs {
		if bs[i].Box == b {
			return &bs[i]
		}
	}
	panic("tbl: a continued box missing from the row above")
}

// layBox lays cell i of a data row in box b. A note row's cells are L
// and wrap whatever their columns are. A number in an N box wider
// than the box is an error: cutting it would show another number.
func layBox(f Format, r Row, i int, b Box, grid *Grid) (box, error) {
	col := f.Cols[b.First]
	if r.Role == Note {
		col.Align, col.Clip = 'L', false
	}
	w := grid.spans[b.Last].End - grid.spans[b.First].Start
	c := r.Cells[i]
	text := c.Text
	if _, _, num := SplitNumeric(text); col.Align == 'N' && num && utf8.RuneCountInString(text) > w {
		return box{}, errAt(r.Line, c.Col, ErrNumber, "%q in a box of %d", text, w)
	}
	var lines []string
	switch {
	case col.Align == 'N' && b.Single():
		lines = []string{grid.number(b.First, text)}
	case col.Align == 'N':
		lines = []string{pad(format.Trunc(text, w), w, 'R')}
	case col.Clip:
		lines = []string{pad(format.Trunc(text, w), w, col.Align)}
	default:
		for _, ln := range wrap.Cell(text, w, hyphen.Default) {
			lines = append(lines, pad(ln, w, col.Align))
		}
	}
	code := c.Code.Over(f.Row.Over(f.Cols[b.First].Code))
	return box{
		Box:   b,
		lines: lines,
		placed: Placed{
			Start:  grid.spans[b.First].Start,
			End:    grid.spans[b.Last].End,
			FG:     code.FG,
			BG:     code.BG,
			Target: c.Target,
		},
	}, nil
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
