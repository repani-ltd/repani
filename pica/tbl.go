// Table renderer. The language -- the column spec, rows and their
// roles, cells on the grid, wrapping and clipping -- is the table
// language pica shares with board, repani.com/typeset/tbl (its
// SPEC.t), and so is the monospace layout. This file adds what tbl
// leaves to its host: which rows pica accepts, the header and its
// rule, half-size note rows, total rows under a rule, prose cells
// measured in a proportional face, numbers a writer sets itself, and
// the layout the writers read.
package pica

import (
	"errors"
	"fmt"
	"strings"

	"repani.com/typeset/tbl"
	"repani.com/typeset/wrap"
	"repani.com/typeset/wrap/hyphen"
)

// The refusals that are pica's own; the language's errors are tbl's.
// Both come as a *tbl.Error, at the line and column at fault.
var (
	ErrTableRelative = errors.New("pica: a table's first .fmt gives every column a width")
	ErrTableLink     = errors.New("pica: tables set no links")
	ErrTableHeader   = errors.New("pica: the header row (^) is the table's first row")
	ErrTableRow      = errors.New("pica: a blank line or a --- rule is not a pica table row")
	ErrTableEmpty    = errors.New("pica: a full .fmt with no rows under it")
)

// Table is one grid of a parsed .fmt block: the rows from a full
// format to the next, each under the format it was written under,
// laid out at a width when a writer asks. A block whose body holds
// several full formats is several Tables, the later ones Tight.
type Table struct {
	tt tbl.Table // the rows, each with its format
}

// tableFormats reads a .fmt block's formats. Every refusal comes as a
// *tbl.Error at the line and column at fault.
type tableFormats struct {
	fs  tbl.Formats
	cur tbl.Format
}

// apply reads one spec, starting at line line, column col, and
// reports whether it is full -- the start of a new grid. The first
// spec of a block must be full.
func (f *tableFormats) apply(spec string, line, col int) (bool, error) {
	fm, full, err := f.fs.Apply(spec, line, col)
	if errors.Is(err, tbl.ErrNoFull) {
		return false, &tbl.Error{Line: line, Col: col, Err: fmt.Errorf("%w: %q", ErrTableRelative, spec)}
	}
	if err != nil {
		return false, err
	}
	f.cur = fm
	return full, nil
}

// Narrow is the width the spec narrows the table to, or 0.
func (t *Table) Narrow() int { return t.tt.Format(0).Narrow }

// add appends a parsed row under fm, refusing what pica does not set:
// blank and rule rows, links, a header after the first row, and what
// tbl refuses (more cells than groups, a note with no row above it, a
// ^ with no box to continue). Colours pass: the form layout and HTML
// set them.
func (t *Table) add(fm tbl.Format, r tbl.Row) error {
	if r.Kind != tbl.Data {
		return &tbl.Error{Line: r.Line, Col: 1, Err: ErrTableRow}
	}
	if r.Role == tbl.Header && t.tt.Len() > 0 {
		return &tbl.Error{Line: r.Line, Col: r.Cells[0].Col, Err: ErrTableHeader}
	}
	for _, c := range r.Cells {
		if c.Target != "" {
			return &tbl.Error{Line: r.Line, Col: c.Col, Err: ErrTableLink}
		}
	}
	return t.tt.Add(fm, r)
}

// separator is the character used to draw header underline rows.
// ASCII "-" guarantees exact monospace alignment in any viewer.
const separator = '-'

// TableLayout is a table laid out at a concrete width: its header
// (nil when the table is headerless), its rows in order, and every
// column's span. A column-splitting writer treats each row with its
// notes as atomic and repeats the header after a split.
type TableLayout struct {
	Header *TableRow
	Rows   []TableRow
	Cols   []Span
}

// TableRow is one laid row with the notes under it.
//
// Lines are the row on the monospace grid, a measured cell's box
// left blank. Prose holds the measured cells (LayoutMeasured): one
// per box whose first column is P in a data row, every box in the
// header. Nums holds the numbers on the row's first line in single N
// boxes, for a writer that sets them itself anchored on the point.
//
// Notes are the note rows' lines on the half-size grid: a half-size
// rune is half a body rune, so the lines budget twice the runes and
// their offsets are twice the body's. Half-line writers draw them at
// half the body size on half the leading; Lines() renders notes as
// ordinary full-size rows instead. A note row that a box runs down
// through (^) is no note here but an ordinary row: its lines and the
// box's are one text, which the half-size grid would cut apart.
//
// Rule is the rule the row carries -- the header's under it, a total
// row's over it -- one segment per column, leaving out the columns of
// a box that runs across the rule's line.
type TableRow struct {
	Lines []string
	Total bool
	Prose []ProseCell
	Nums  []NumCell
	Notes []string
	Rule  []Span
	// Cont marks a row that continues a box of the row above (a ^
	// column): a splitting writer keeps it with that row.
	Cont bool

	notes []string // the notes on the full-size grid, for Lines
}

// ProseCell is one cell measured in a proportional face: its box on
// the grid, the alignment of the box's first column, and its lines.
type ProseCell struct {
	Box   Span
	Align byte
	Lines []Line
}

// NumCell is a number in a single N box: its box, the grid column its
// decimal point occupies, and the number split there (tbl's
// SplitNumeric: the integer part, the tail from the point on).
type NumCell struct {
	Box       Span
	Sep       int
	Int, Tail string
}

// Span is a stretch of grid columns, Start to End, the end exclusive:
// a column's or a box's place in a formatted line (tbl.Span), and the
// rune interval of an emphasis underline (EmphLines). In the text
// writer and mono documents a P cell lays out as L.
type Span = tbl.Span

// Lines flattens the layout in order for full-size output: header,
// its notes, the rule, then each row with its notes. A total row
// sits under its own dash rule.
func (tl *TableLayout) Lines() []string {
	var out []string
	if tl.Header != nil {
		out = append(out, tl.Header.Lines...)
		out = append(out, tl.Header.notes...)
		out = append(out, ruleLine(tl.Header.Rule))
	}
	for _, r := range tl.Rows {
		if r.Total {
			out = append(out, ruleLine(r.Rule))
		}
		out = append(out, r.Lines...)
		out = append(out, r.notes...)
	}
	return out
}

// ruleLine draws a rule's segments as dash runs at their columns, the
// gaps blank, trailing blanks removed.
func ruleLine(segs []Span) string {
	var buf []rune
	for _, s := range segs {
		for len(buf) < s.Start {
			buf = append(buf, ' ')
		}
		for range s.End - s.Start {
			buf = append(buf, separator)
		}
	}
	return string(buf)
}

// ruleOver is a rule's segments: the spans of the columns of the boxes
// keep admits, in column order.
func ruleOver(spans []Span, boxes []tbl.RowBox, keep func(tbl.RowBox) bool) []Span {
	var out []Span
	for _, b := range boxes {
		if keep(b) {
			out = append(out, spans[b.First:b.Last+1]...)
		}
	}
	return out
}

// Layout lays the table out to fit in width total runes, narrowed by
// the spec's width when that is less. Errors when the fixed columns
// exceed the width or an auto-span column has no room. P columns
// render as L: the mono grid is the layout.
func (t *Table) Layout(width int) (*TableLayout, error) {
	return t.layout(width, nil, nil, 0)
}

// LayoutMeasured is Layout with prose cells measured: each P box's
// cell wraps under m at the box's measure -- its rune width times
// runeUnits, the mono advance expressed in m's units
// (em-thousandths at the drawing size). The formatted lines reserve
// each P box blank at the measured height; the measured lines land
// in TableRow.Prose for positioned drawing. When mHead is non-nil,
// every header cell is measured the same way under it (the header
// row is the table's labels, set in the body face). Everything else
// -- grid, splits, notes, numbers -- is exactly Layout.
func (t *Table) LayoutMeasured(width int, m, mHead Measurer, runeUnits int) (*TableLayout, error) {
	return t.layout(width, m, mHead, runeUnits)
}

func (t *Table) layout(width int, m, mHead Measurer, runeUnits int) (*TableLayout, error) {
	grid, laid, err := t.tt.Layout(width)
	if err != nil {
		return nil, err
	}
	spans := grid.Spans()
	tl := &TableLayout{Cols: spans}
	nums := map[int]tbl.Num{}
	for _, n := range grid.Nums() {
		nums[n.Start] = n
	}
	for i, row := range t.tt.Rows() {
		boxes, lines := laid[i].Boxes, laid[i].Lines
		text := func(b tbl.RowBox) string { return row.Cells[b.Cell].Text }
		// measured wraps the row's own boxes that keep admits under
		// mm at their measures, reserving them blank in the lines; a
		// box that runs down rows stays on the mono grid, its lines
		// crossing rows, which measured drawing does not.
		measured := func(mm Measurer, keep func(tbl.RowBox) bool) []ProseCell {
			var pcs []ProseCell
			reserve := make([]bool, len(boxes))
			for at, b := range boxes {
				if !b.Cont && b.Rows == 1 && keep(b) {
					p := lines[0][at]
					pcs = append(pcs, ProseCell{
						Box: Span{Start: p.Start, End: p.End}, Align: b.Align,
						Lines: CellLines(text(b), (p.End-p.Start)*runeUnits, mm, b.Clip),
					})
					reserve[at] = true
				}
			}
			if pcs != nil {
				lines = reserveBoxes(lines, reserve, pcs)
			}
			return pcs
		}
		switch {
		case row.Role == tbl.Note && !runsThrough(boxes):
			// Under the row above: the header when no data row has
			// come (tbl refuses a note with neither).
			r := tl.Header
			if len(tl.Rows) > 0 {
				r = &tl.Rows[len(tl.Rows)-1]
			}
			var mine []tbl.Placed
			var texts []string
			for at, b := range boxes {
				mine, texts = append(mine, lines[0][at]), append(texts, text(b))
			}
			r.Notes = append(r.Notes, halfNote(mine, texts)...)
			r.notes = append(r.notes, flatLines(lines)...)
			continue
		case row.Role == tbl.Header:
			// The header row is the table's labels: measured, every
			// cell, in the header face, for a writer that sets them in
			// the body face. Its rule leaves out the columns of a box
			// that runs on down.
			var pcs []ProseCell
			if mHead != nil {
				pcs = measured(mHead, func(tbl.RowBox) bool { return true })
			}
			rule := ruleOver(spans, boxes, func(b tbl.RowBox) bool { return b.Rows == 1 })
			tl.Header = &TableRow{Lines: flatLines(lines), Prose: pcs, Rule: rule}
			continue
		}
		r := TableRow{Total: row.Role == tbl.Total}
		for _, b := range boxes {
			r.Cont = r.Cont || b.Cont
		}
		if r.Total {
			r.Rule = ruleOver(spans, boxes, func(b tbl.RowBox) bool { return !b.Cont })
		}
		if m != nil {
			r.Prose = measured(m, func(b tbl.RowBox) bool { return b.Align == 'P' })
		}
		for at, b := range boxes {
			n, isNum := nums[lines[0][at].Start]
			if b.Cont || !b.Single() || !isNum || b.Align != 'N' {
				continue
			}
			if whole, tail, ok := tbl.SplitNumeric(text(b)); ok {
				r.Nums = append(r.Nums, NumCell{Box: n.Span, Sep: n.SepIndex(), Int: whole, Tail: tail})
			}
		}
		r.Lines = flatLines(lines)
		tl.Rows = append(tl.Rows, r)
	}
	return tl, nil
}

// runsThrough reports whether a box runs down through a row: one it
// continues, or one of its own that rows below continue.
func runsThrough(boxes []tbl.RowBox) bool {
	for _, b := range boxes {
		if b.Cont || b.Rows > 1 {
			return true
		}
	}
	return false
}

// CellRow is one row of a table as a writer that draws every cell
// itself sees it (the form layout, HTML): its role and its cells in
// column order.
type CellRow struct {
	Header, Total, Note bool
	Cells               []Cell
}

// Cell is one box of a row with what it holds: tbl's resolved box --
// its format columns, whether it continues a box above (^), the row it
// began on and how many rows it covers, its alignment, clip and colours
// -- and, for a box of the row's own, its cell's text. A continuing box
// has no text of its own: the text of the cell that began it runs
// down.
type Cell struct {
	tbl.RowBox
	Text string
}

// The print palette of tbl's eight colours, by index (d r g y b m c w):
// as ink, dark enough to read as text on paper; as fill, a pale tint
// text stays legible on. Index 0, the default, is black ink and no
// fill. The yellow and blue tints are the spreadsheet's input and
// lookup fills.
var (
	inks = [8][3]float64{
		{0, 0, 0}, {0.75, 0.08, 0.08}, {0.05, 0.45, 0.15}, {0.6, 0.42, 0},
		{0.1, 0.25, 0.6}, {0.55, 0.1, 0.5}, {0, 0.45, 0.5}, {1, 1, 1},
	}
	tints = [8][3]float64{
		{1, 1, 1}, {1, 0.87, 0.87}, {0.88, 0.96, 0.88}, {1, 1, 0.88},
		{0.87, 0.93, 1}, {0.97, 0.88, 0.97}, {0.87, 0.97, 0.97}, {1, 1, 1},
	}
)

// Ink is colour c as a text colour, its red, green and blue from 0 to
// 1.
func Ink(c uint8) (r, g, b float64) { k := inks[c&7]; return k[0], k[1], k[2] }

// Tint is colour c as a cell's fill.
func Tint(c uint8) (r, g, b float64) { k := tints[c&7]; return k[0], k[1], k[2] }

// Cells returns every row's cells: the table's structure and text,
// which no width changes, for a writer that measures and draws the
// cells itself.
func (t *Table) Cells() []CellRow {
	rows := make([]CellRow, t.tt.Len())
	for i, row := range t.tt.Rows() {
		r := CellRow{Header: row.Role == tbl.Header, Total: row.Role == tbl.Total, Note: row.Role == tbl.Note}
		for _, b := range t.tt.Boxes(i) {
			c := Cell{RowBox: b}
			if !b.Cont {
				c.Text = row.Cells[b.Cell].Text
			}
			r.Cells = append(r.Cells, c)
		}
		rows[i] = r
	}
	return rows
}

// Columns fits the table's columns to width: each column's span on
// the grid, for a writer that places the cells itself.
func (t *Table) Columns(width int) ([]Span, error) {
	grid, err := t.tt.Columns(width)
	if err != nil {
		return nil, err
	}
	return grid.Spans(), nil
}

// reserveBoxes blanks the reserved boxes of a laid row on every line
// and sets the row's height to what the rest of its boxes need or
// the measured cells' lines, whichever is more: the measured lines
// draw positioned, outside the mono grid.
func reserveBoxes(lines [][]tbl.Placed, reserve []bool, measured []ProseCell) [][]tbl.Placed {
	height := 1
	for _, pc := range measured {
		height = max(height, len(pc.Lines))
	}
	for k, keep := range reserve {
		if keep {
			continue
		}
		// A mono cell's lines are never blank but its last ones
		// (empty text is one empty line), so its height is its last
		// line with text.
		for h := len(lines) - 1; h > 0; h-- {
			if strings.TrimSpace(lines[h][k].Text) != "" {
				height = max(height, h+1)
				break
			}
		}
	}
	out := make([][]tbl.Placed, height)
	for h := range out {
		src := lines[min(h, len(lines)-1)]
		line := make([]tbl.Placed, len(src))
		for k, p := range src {
			if reserve[k] || h >= len(lines) {
				p.Text = strings.Repeat(" ", p.End-p.Start)
			}
			line[k] = p
		}
		out[h] = line
	}
	return out
}

// flatLines draws laid lines as text: each box at its columns, the
// gaps blank, trailing blanks removed.
func flatLines(lines [][]tbl.Placed) []string {
	out := make([]string, len(lines))
	for h, line := range lines {
		out[h] = placeLine(line, 1, nil)
	}
	return out
}

// placeLine draws one line of boxes on a grid scaled by scale, each
// box's text from texts when given, else its own.
func placeLine(line []tbl.Placed, scale int, texts []string) string {
	end := 0
	for _, p := range line {
		end = max(end, p.End*scale)
	}
	buf := []rune(strings.Repeat(" ", end))
	for k, p := range line {
		s := p.Text
		if texts != nil {
			s = texts[k]
		}
		copy(buf[p.Start*scale:], []rune(s))
	}
	return strings.TrimRight(string(buf), " ")
}

// halfNote sets a note row in its boxes on the half-size grid, where
// widths and gaps double: each cell left-aligned and wrapped at twice
// its box's width. (tbl lays the full-size note.)
func halfNote(boxes []tbl.Placed, cells []string) []string {
	stacks := make([][]string, len(boxes))
	height := 1
	for k, p := range boxes {
		stacks[k] = wrapCell(cells[k], 2*(p.End-p.Start))
		height = max(height, len(stacks[k]))
	}
	out := make([]string, height)
	for h := range out {
		texts := make([]string, len(boxes))
		for k := range boxes {
			if h < len(stacks[k]) {
				texts[k] = stacks[k][h]
			}
		}
		out[h] = placeLine(boxes, 2, texts)
	}
	return out
}

// CellLines breaks a table cell's text under a real measurer at
// measure, in m's units: the cell breaker every writer that measures
// cells shares -- ragged, hyphenating at the cell penalty -- cut to its
// first line for a clip cell.
func CellLines(s string, measure int, m Measurer, clip bool) []Line {
	lines := wrap.Hyphenated(s, measure, measure, hyphen.Default, wrap.PenaltyCell, m)
	if clip && len(lines) > 1 {
		lines = lines[:1]
	}
	return lines
}

// wrapCell wraps a cell's text to width: wrap.Cell, the breaker the
// table language shares.
func wrapCell(s string, width int) []string { return wrap.Cell(s, width, hyphen.Default) }
