// Table renderer. The language -- the column spec, rows and their
// roles, cells on the grid, wrapping and clipping -- is the table
// language pica shares with board, repani.com/typeset/tbl (its
// SPEC.t), and so is the monospace layout. This file adds what tbl
// leaves to its host: which rows pica accepts, the header and its
// rule, half-size note rows, total rows under a rule, prose cells
// measured in a proportional face, and the layout the writers read.
package pica

import (
	"errors"
	"fmt"
	"strings"

	"repani.com/typeset/tab"
	"repani.com/typeset/tbl"
	"repani.com/typeset/wrap"
	"repani.com/typeset/wrap/hyphen"
)

// The refusals that are pica's own; the language's errors are tbl's.
var (
	ErrTableRelative = errors.New("pica: a .table spec gives every column a width")
	ErrTableColour   = errors.New("pica: tables set no colours or links")
	ErrTableHeader   = errors.New("pica: the header row (^) is the table's first row")
	ErrTableRow      = errors.New("pica: a blank line or a --- rule is not a pica table row")
)

// Table is a fixed-width table builder. The column spec is parsed at
// construction; the total width is supplied at Layout time, so the
// same table can be laid out for different output widths.
type Table struct {
	fm   tbl.Format
	tt   tbl.Table
	rows []tableRow // in order; a header, if any, first
	err  error      // the first row a builder method refused
}

// tableRow is one row as pica keeps it: its role -- data, header,
// total, note -- and its cells' text.
type tableRow struct {
	role  tbl.Role
	cells []string
}

// NewTable parses a column spec ("3L *L 4R!", tbl's full format, a
// leading width narrowing it) and returns an empty table, or an
// error if the spec is malformed. Whether the columns fit is checked
// at Layout, where the total width is known.
func NewTable(spec string) (*Table, error) {
	var fs tbl.Formats
	fm, full, err := fs.Apply(spec, 1)
	if errors.Is(err, tbl.ErrNoFull) {
		return nil, fmt.Errorf("%w: %q", ErrTableRelative, spec)
	}
	if err != nil {
		return nil, err
	}
	if !full {
		return nil, fmt.Errorf("%w: %q", ErrTableRelative, spec)
	}
	coloured := fm.Row.HasFG || fm.Row.HasBG
	for _, c := range fm.Cols {
		coloured = coloured || c.Code.HasFG || c.Code.HasBG
	}
	if coloured {
		return nil, fmt.Errorf("%w: %q", ErrTableColour, spec)
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
		return ErrTableRow
	}
	if r.Role == tbl.Header && len(t.rows) > 0 {
		return ErrTableHeader
	}
	cells := make([]string, len(r.Cells))
	for i, c := range r.Cells {
		if c.Code.HasFG || c.Code.HasBG || c.Target != "" {
			return fmt.Errorf("%w: column %d", ErrTableColour, c.Col)
		}
		cells[i] = c.Text
	}
	if err := t.tt.Add(t.fm, r); err != nil {
		return err
	}
	t.rows = append(t.rows, tableRow{role: r.Role, cells: cells})
	return nil
}

// build adds a row from a builder method; the first refusal is kept
// and returned by Layout.
func (t *Table) build(role tbl.Role, cells []string) *Table {
	r := tbl.Row{Kind: tbl.Data, Role: role}
	for _, c := range cells {
		r.Cells = append(r.Cells, tbl.Cell{Text: c})
	}
	if err := t.add(r); err != nil && t.err == nil {
		t.err = err
	}
	return t
}

// Header sets the header row, which must come first.
func (t *Table) Header(cells ...string) *Table { return t.build(tbl.Header, cells) }

// Row appends a data row.
func (t *Table) Row(cells ...string) *Table { return t.build(tbl.None, cells) }

// Note appends a note row: half-size annotation lines under the row
// above it, the header included. Note cells left-align in their
// boxes and wrap at the half-size rune budget (twice the box width).
func (t *Table) Note(cells ...string) *Table { return t.build(tbl.Note, cells) }

// Total appends a total row: formatted like a data row (its numbers
// weigh into N-column metrics) but set bold under a rule by writers
// whose medium can; the plain-text writer draws the rule as a dash
// row.
func (t *Table) Total(cells ...string) *Table { return t.build(tbl.Total, cells) }

// boxes returns row i's boxes: cell k's first and last column.
func (t *Table) boxes(i int) []tbl.Box { return t.fm.Boxes(len(t.rows[i].cells)) }

// separator is the character used to draw header underline rows.
// ASCII "-" guarantees exact monospace alignment in any viewer.
const separator = '-'

// TableLayout is a table laid out at a concrete width. Header holds
// the header lines (nil when the table is headerless); Sep is the
// dashed separator row, always computed: it underlines the header
// when there is one and sits above every total row regardless. Each
// element of Rows is one data row's lines
// (more than one when a cell wrapped). A column-splitting writer
// treats each row plus its notes as atomic and repeats the header
// after a split.
//
// HeaderNotes and RowNotes hold note-row lines formatted on the
// half-size grid: a half-size rune is half a body rune, so those
// lines budget twice the runes and their column offsets are twice
// the body offsets. Half-line writers draw them at half the body
// size on half the leading; Lines() instead renders notes as
// ordinary full-size rows for plain-text output.
type TableLayout struct {
	Header      []string
	Sep         string
	Rows        [][]string
	Totals      []bool     // parallel to Rows: row is a total row
	HeaderNotes []string   // half-grid note lines under the header
	RowNotes    [][]string // parallel to Rows; nil = no notes
	NumCols     []NumCol   // resolved N-column geometry, left to right
	Cols        []Span     // every column's [Start,End) rune interval
	Aligns      []byte     // every column's align letter, parallel to Cols

	// RowProse holds each row's P cells measured (LayoutMeasured
	// only): one ProseCell per box whose first column is P. The
	// formatted Rows reserve that box blank at the measured height;
	// a positioning writer draws the lines at the box's offset.
	RowProse [][]ProseCell

	// HeaderProse holds the header cells measured, one per box
	// (LayoutMeasured with a header measurer only): the header row is
	// the table's labels, set in the body face by writers that can.
	// The formatted Header reserves the space blank, as with
	// RowProse.
	HeaderProse []ProseCell

	headerNotesText []string   // full-grid note lines, for Lines
	rowNotesText    [][]string // parallel to Rows
}

// ProseCell is one cell measured in a proportional face: its box on
// the grid, the alignment of the box's first column, and its lines.
type ProseCell struct {
	Box   Span
	Align byte
	Lines []Line
}

// Span is one column's [Start,End) rune interval on the full grid:
// the cell's offsets within a formatted line (tab.Span; also the
// rune interval of an emphasis underline, see EmphLines). In the text
// writer and mono documents a P cell lays out as L.
type Span = tab.Span

// NumCol is one N column's resolved geometry on the rune grid
// (tab.Num): its Span, the widest fraction tail, and whether the
// column reserves the accounting paren slot; SepIndex is the cell
// every decimal point occupies.
type NumCol = tab.Num

// SplitNumeric splits a numeric cell for separator-anchored drawing:
// intPart is everything before the decimal separator (the opening
// paren included), tail everything from the separator on (the
// closing paren included). ok is false for content that does not
// read as a number -- headers, "n/a" -- which stays as formatted.
func SplitNumeric(s string) (intPart, tail string, ok bool) {
	return tab.SplitNumeric(s)
}

// Lines flattens the layout in order for full-size output: header,
// its notes, the separator, then each row with its notes. A total
// row sits under its own dash rule.
func (tl *TableLayout) Lines() []string {
	out := append([]string{}, tl.Header...)
	out = append(out, tl.headerNotesText...)
	if len(tl.Header) > 0 {
		out = append(out, tl.Sep)
	}
	for i, r := range tl.Rows {
		if tl.Totals[i] {
			out = append(out, tl.Sep)
		}
		out = append(out, r...)
		out = append(out, tl.rowNotesText[i]...)
	}
	return out
}

// RowLines reports, for each data row, the half-open interval of
// Lines() indices the row occupies: its wrapped lines and its note
// lines, excluding the separator that precedes a total row. A
// consumer that styles by row (a cell-grid renderer filling a whole
// row) uses it instead of re-deriving the renderer's walk.
func (tl *TableLayout) RowLines() []Span {
	n := len(tl.Header) + len(tl.headerNotesText)
	if len(tl.Header) > 0 {
		n++ // the header separator
	}
	out := make([]Span, len(tl.Rows))
	for i, r := range tl.Rows {
		if tl.Totals[i] {
			n++
		}
		start := n
		n += len(r) + len(tl.rowNotesText[i])
		out[i] = Span{Start: start, End: n}
	}
	return out
}

// Layout lays the table out to fit in width total runes. Errors when
// the fixed columns exceed width or an auto-span column has no room.
// P columns render as L: the mono grid is the layout.
func (t *Table) Layout(width int) (*TableLayout, error) {
	return t.layout(width, nil, nil, 0)
}

// LayoutMeasured is Layout with prose cells measured: each P box's
// cell wraps under m at the box's measure -- its rune width times
// runeUnits, the mono advance expressed in m's units
// (em-thousandths at the drawing size). The formatted Rows reserve
// each P box blank at the measured height; the measured lines land
// in RowProse for positioned drawing. When mHead is non-nil, every
// header cell is measured the same way under it (the header row is
// the table's labels, set in the body face) and lands in
// HeaderProse. Everything else -- grid, splits, notes, N metrics --
// is exactly Layout.
func (t *Table) LayoutMeasured(width int, m, mHead Measurer, runeUnits int) (*TableLayout, error) {
	return t.layout(width, m, mHead, runeUnits)
}

func (t *Table) layout(width int, m, mHead Measurer, runeUnits int) (*TableLayout, error) {
	if t.err != nil {
		return nil, t.err
	}
	tt := t.tt
	if len(t.rows) == 0 {
		// An empty table still has a grid: lay out one blank row.
		tt = tbl.Table{}
		tt.Add(t.fm, tbl.Row{Kind: tbl.Blank})
	}
	grid, err := tt.Grid(width)
	if err != nil {
		return nil, err
	}
	laid, err := t.tt.Layout(width)
	if err != nil {
		return nil, err
	}
	tl := &TableLayout{Cols: grid.Spans(), NumCols: grid.Nums(), Sep: grid.Rule(separator)}
	for _, c := range t.fm.Cols {
		tl.Aligns = append(tl.Aligns, c.Align)
	}
	for i, row := range t.rows {
		lines := laid[i].Lines
		bs := t.boxes(i)
		switch row.role {
		case tbl.Note:
			half := noteLines(lines[0], row.cells, 2)
			text := noteLines(lines[0], row.cells, 1)
			if len(tl.Rows) == 0 {
				tl.HeaderNotes = append(tl.HeaderNotes, half...)
				tl.headerNotesText = append(tl.headerNotesText, text...)
			} else {
				k := len(tl.Rows) - 1
				tl.RowNotes[k] = append(tl.RowNotes[k], half...)
				tl.rowNotesText[k] = append(tl.rowNotesText[k], text...)
			}
		case tbl.Header:
			if mHead != nil {
				// Measured header: every cell wraps under the header
				// measurer at its box's measure, the formatted lines
				// reserve the space blank, and the measured lines land
				// in HeaderProse for the writer to set in the body face.
				var pcs []ProseCell
				reserve := make([]bool, len(bs))
				for k, b := range bs {
					pcs = append(pcs, t.measure(lines[0][k], row.cells[k], t.fm.Cols[b.First], mHead, runeUnits))
					reserve[k] = true
				}
				tl.HeaderProse = pcs
				lines = reserveBoxes(lines, reserve, pcs)
			}
			tl.Header = flatLines(lines)
		default:
			var pcs []ProseCell
			if m != nil {
				reserve := make([]bool, len(bs))
				for k, b := range bs {
					if col := t.fm.Cols[b.First]; col.Align == 'P' {
						pcs = append(pcs, t.measure(lines[0][k], row.cells[k], col, m, runeUnits))
						reserve[k] = true
					}
				}
				if pcs != nil {
					lines = reserveBoxes(lines, reserve, pcs)
				}
			}
			tl.Rows = append(tl.Rows, flatLines(lines))
			tl.RowProse = append(tl.RowProse, pcs)
			tl.Totals = append(tl.Totals, row.role == tbl.Total)
			tl.RowNotes = append(tl.RowNotes, nil)
			tl.rowNotesText = append(tl.rowNotesText, nil)
		}
	}
	return tl, nil
}

// measure wraps a cell under m at its box's measure.
func (t *Table) measure(p tbl.Placed, text string, col tbl.Column, m Measurer, runeUnits int) ProseCell {
	lines := wrapCellMeasured(text, (p.End-p.Start)*runeUnits, m)
	if col.Clip && len(lines) > 1 {
		lines = lines[:1] // a clip column is one line, measured or not
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

// noteLines formats a note row in its boxes on a grid scaled by
// scale: scale 2 is the half-size grid (widths and the column gap
// double), scale 1 the full-size grid for plain-text output. Notes
// always left-align and wrap.
func noteLines(boxes []tbl.Placed, cells []string, scale int) []string {
	stacks := make([][]string, len(boxes))
	height := 1
	for k, p := range boxes {
		stacks[k] = wrapCell(cells[k], (p.End-p.Start)*scale)
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
		out[h] = placeLine(boxes, scale, texts)
	}
	return out
}

// wrapCellMeasured wraps prose cell content under a real measurer
// at the given measure, with the cell-tuned hyphen penalty.
func wrapCellMeasured(s string, measure int, m Measurer) []Line {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return wrap.Hyphenated(s, measure, hyphen.Default, wrap.PenaltyCell, m)
}

// wrapCell wraps a cell's text to width: wrap.Cell, the breaker the
// table language shares.
func wrapCell(s string, width int) []string { return wrap.Cell(s, width, hyphen.Default) }
