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
	ErrTableRelative = errors.New("pica: a .table spec gives every column a width")
	ErrTableColour   = errors.New("pica: tables set no colours or links")
	ErrTableHeader   = errors.New("pica: the header row (^) is the table's first row")
	ErrTableRow      = errors.New("pica: a blank line or a --- rule is not a pica table row")
)

// Table is a parsed .table: its format and its rows, laid out at a
// width when a writer asks.
type Table struct {
	fm tbl.Format
	tt tbl.Table
}

// newTable parses a column spec -- tbl's full format, a leading width
// narrowing it -- that starts at line line, column col.
func newTable(spec string, line, col int) (*Table, error) {
	var fs tbl.Formats
	fm, _, err := fs.Apply(spec, line, col)
	if errors.Is(err, tbl.ErrNoFull) {
		return nil, &tbl.Error{Line: line, Col: col, Err: fmt.Errorf("%w: %q", ErrTableRelative, spec)}
	}
	if err != nil {
		return nil, err
	}
	coloured := fm.Row.HasFG || fm.Row.HasBG
	for _, c := range fm.Cols {
		coloured = coloured || c.Code.HasFG || c.Code.HasBG
	}
	if coloured {
		return nil, &tbl.Error{Line: line, Col: col, Err: fmt.Errorf("%w: %q", ErrTableColour, spec)}
	}
	return &Table{fm: fm}, nil
}

// Narrow is the width the spec narrows the table to, or 0.
func (t *Table) Narrow() int { return t.fm.Narrow }

// add appends a parsed row, refusing what pica does not set: blank
// and rule rows, colours and links, a header after the first row,
// and what tbl refuses (more cells than groups, a note with no row
// above it).
func (t *Table) add(r tbl.Row) error {
	if r.Kind != tbl.Data {
		return &tbl.Error{Line: r.Line, Col: 1, Err: ErrTableRow}
	}
	if r.Role == tbl.Header && len(t.tt.Rows()) > 0 {
		return &tbl.Error{Line: r.Line, Col: r.Cells[0].Col, Err: ErrTableHeader}
	}
	for _, c := range r.Cells {
		if c.Code.HasFG || c.Code.HasBG || c.Target != "" {
			return &tbl.Error{Line: r.Line, Col: c.Col, Err: ErrTableColour}
		}
	}
	return t.tt.Add(t.fm, r)
}

// separator is the character used to draw header underline rows.
// ASCII "-" guarantees exact monospace alignment in any viewer.
const separator = '-'

// TableLayout is a table laid out at a concrete width: its header
// (nil when the table is headerless), its rows in order, and every
// column's span, the segments of the rule under the header and above
// each total row. A column-splitting writer treats each row with its
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
// ordinary full-size rows instead.
type TableRow struct {
	Lines []string
	Total bool
	Prose []ProseCell
	Nums  []NumCell
	Notes []string

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
	parts := make([]string, len(tl.Cols))
	for i, c := range tl.Cols {
		parts[i] = strings.Repeat(string(separator), c.End-c.Start)
	}
	rule := strings.Join(parts, " ")
	var out []string
	if tl.Header != nil {
		out = append(out, tl.Header.Lines...)
		out = append(out, tl.Header.notes...)
		out = append(out, rule)
	}
	for _, r := range tl.Rows {
		if r.Total {
			out = append(out, rule)
		}
		out = append(out, r.Lines...)
		out = append(out, r.notes...)
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
	tl := &TableLayout{Cols: grid.Spans()}
	nums := map[int]tbl.Num{}
	for _, n := range grid.Nums() {
		nums[n.Start] = n
	}
	for i, row := range t.tt.Rows() {
		lines := laid[i].Lines
		texts := make([]string, len(row.Cells))
		for k, c := range row.Cells {
			texts[k] = c.Text
		}
		bs := t.fm.Boxes(len(texts))
		switch row.Role {
		case tbl.Note:
			// Under the row above: the header when no data row has
			// come (tbl refuses a note with neither).
			r := tl.Header
			if len(tl.Rows) > 0 {
				r = &tl.Rows[len(tl.Rows)-1]
			}
			r.Notes = append(r.Notes, halfNote(lines[0], texts)...)
			r.notes = append(r.notes, flatLines(lines)...)
			continue
		case tbl.Header:
			var pcs []ProseCell
			if mHead != nil {
				// Measured header: every cell wraps under the header
				// measurer at its box's measure, the formatted lines
				// reserve the space blank, and the measured lines land
				// in Prose for the writer to set in the body face.
				reserve := make([]bool, len(bs))
				for k, b := range bs {
					pcs = append(pcs, measure(lines[0][k], texts[k], t.fm.Cols[b.First], mHead, runeUnits))
					reserve[k] = true
				}
				lines = reserveBoxes(lines, reserve, pcs)
			}
			tl.Header = &TableRow{Lines: flatLines(lines), Prose: pcs}
			continue
		}
		r := TableRow{Total: row.Role == tbl.Total}
		if m != nil {
			reserve := make([]bool, len(bs))
			for k, b := range bs {
				if col := t.fm.Cols[b.First]; col.Align == 'P' {
					r.Prose = append(r.Prose, measure(lines[0][k], texts[k], col, m, runeUnits))
					reserve[k] = true
				}
			}
			if r.Prose != nil {
				lines = reserveBoxes(lines, reserve, r.Prose)
			}
		}
		for k, b := range bs {
			n, isNum := nums[lines[0][k].Start]
			if !b.Single() || !isNum || t.fm.Cols[b.First].Align != 'N' {
				continue
			}
			if whole, tail, ok := tbl.SplitNumeric(texts[k]); ok {
				r.Nums = append(r.Nums, NumCell{Box: n.Span, Sep: n.SepIndex(), Int: whole, Tail: tail})
			}
		}
		r.Lines = flatLines(lines)
		tl.Rows = append(tl.Rows, r)
	}
	return tl, nil
}

// measure wraps a cell under m at its box's measure; a clip column's
// cell is one line, measured or not.
func measure(p tbl.Placed, text string, col tbl.Column, m Measurer, runeUnits int) ProseCell {
	lines := wrapCellMeasured(text, (p.End-p.Start)*runeUnits, m)
	if col.Clip && len(lines) > 1 {
		lines = lines[:1]
	}
	return ProseCell{Box: Span{Start: p.Start, End: p.End}, Align: col.Align, Lines: lines}
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

// wrapCellMeasured wraps prose cell content under a real measurer
// at the given measure, with the cell-tuned hyphen penalty.
func wrapCellMeasured(s string, measure int, m Measurer) []Line {
	return wrap.Hyphenated(s, measure, measure, hyphen.Default, wrap.PenaltyCell, m)
}

// wrapCell wraps a cell's text to width: wrap.Cell, the breaker the
// table language shares.
func wrapCell(s string, width int) []string { return wrap.Cell(s, width, hyphen.Default) }
