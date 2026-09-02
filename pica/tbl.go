// Table renderer: wrapped cells by default, "!" clips a column.
// Spec syntax and rendering rules live in doc.go. The grid itself
// -- column fitting, alignment, the numeric point -- is the tab
// primitive (repani.com/tab); this file adds what a typeset table
// has over tab stops: wrapped cells, a header and its rule, note
// and total rows, prose cells measured in a proportional face.
package pica

import (
	"errors"
	"fmt"
	"strings"

	"repani.com/tab"
)

// The spec errors are tab's, under pica's names; the align error is
// pica's own, since its alphabet has P.
var (
	ErrTableEmptySpec    = tab.ErrEmptySpec
	ErrTableInvalidToken = tab.ErrInvalidToken
	ErrTableInvalidAlign = errors.New("pica: column align must be L, R, C, N, or P")
	ErrTableAutoConflict = tab.ErrAutoConflict
	ErrTableInvalidWidth = tab.ErrInvalidWidth
	ErrTableAutoNoRoom   = tab.ErrAutoNoRoom
	ErrTableOverflow     = tab.ErrOverflow
)

// Table is a fixed-width table builder. The column spec is parsed at
// construction; the total width is supplied at Layout time, so the
// same table can be laid out for different output widths.
type Table struct {
	cols   []colSpec
	header []string
	rows   []tableRow
}

// tableRow is one source row: data, a half-size note row annotating
// the row (or header) above it, or a total row set bold under a
// rule.
type tableRow struct {
	cells []string
	note  bool
	total bool
}

// colSpec is one column as pica reads it: the tab column (a P
// column is L on the grid) plus the two decorations tab does not
// know -- the pica align letter, which keeps P, and the "!" clip.
type colSpec struct {
	tab.Col
	align byte // 'L', 'R', 'C', 'N', or 'P'
	clip  bool // truncate instead of wrapping ("!" suffix)
}

// NewTable parses a column spec ("3L *L 4R!") and returns an empty
// table, or an error if the spec is malformed. Whether the columns
// fit is checked at Layout, where the total width is known.
func NewTable(spec string) (*Table, error) {
	tokens := strings.Fields(spec)
	if len(tokens) == 0 {
		return nil, ErrTableEmptySpec
	}
	// Strip pica's decorations, hand tab the grammar it owns.
	cols := make([]colSpec, len(tokens))
	plain := make([]string, len(tokens))
	for i, tok := range tokens {
		if clipped, ok := strings.CutSuffix(tok, "!"); ok {
			cols[i].clip = true
			tok = clipped
		}
		if len(tok) < 2 {
			return nil, fmt.Errorf("%w: %q", ErrTableInvalidToken, tok)
		}
		cols[i].align = tok[len(tok)-1]
		if !strings.ContainsRune("LRCNP", rune(cols[i].align)) {
			return nil, fmt.Errorf("%w: %q", ErrTableInvalidAlign, tok)
		}
		if cols[i].align == 'P' {
			tok = tok[:len(tok)-1] + "L"
		}
		plain[i] = tok
	}
	parsed, err := tab.Parse(strings.Join(plain, " "))
	if err != nil {
		return nil, err
	}
	for i := range cols {
		cols[i].Col = parsed[i]
	}
	return &Table{cols: cols}, nil
}

// Header sets the header row.
func (t *Table) Header(cells ...string) *Table {
	t.header = cells
	return t
}

// Row appends a data row.
func (t *Table) Row(cells ...string) *Table {
	t.rows = append(t.rows, tableRow{cells: cells})
	return t
}

// Note appends a note row: half-size annotation lines under the
// nearest preceding data row, or under the header when no data row
// precedes. Note cells left-align beneath their columns and wrap at
// the half-size rune budget (twice the column width).
func (t *Table) Note(cells ...string) *Table {
	t.rows = append(t.rows, tableRow{cells: cells, note: true})
	return t
}

// Total appends a total row: formatted like a data row (its numbers
// weigh into N-column metrics) but set bold under a rule by writers
// whose medium can; the plain-text writer draws the rule as a dash
// row.
func (t *Table) Total(cells ...string) *Table {
	t.rows = append(t.rows, tableRow{cells: cells, total: true})
	return t
}

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
	ProseCols   []Span     // resolved P-column intervals, left to right
	Cols        []Span     // every column's [Start,End) rune interval
	Aligns      []byte     // every column's align letter, parallel to Cols

	// RowProse holds P cells' measured lines (LayoutMeasured only):
	// RowProse[row][k] are the wrapped lines of the cell in
	// ProseCols[k]. The formatted Rows reserve that cell's space
	// blank at the measured height; a positioning writer draws the
	// lines at the column's grid offset.
	RowProse [][][]Line

	// HeaderProse holds the header cells' measured lines, one entry
	// per column (LayoutMeasured with a header measurer only): the
	// header row is the table's labels, set in the body face by
	// writers that can. The formatted Header reserves the space
	// blank, as with RowProse.
	HeaderProse [][]Line

	headerNotesText []string   // full-grid note lines, for Lines
	rowNotesText    [][]string // parallel to Rows
}

// Span is one column's [Start,End) rune interval on the full grid:
// the cell's offsets within a formatted line (tab.Span; also the
// rune interval of an emphasis underline, see EmphLine).
// TableLayout.Cols holds one per column; ProseCols the P columns'
// only, whose cells LayoutMeasured additionally wraps under a real
// measurer for writers that set prose cells in the body face (in
// the text writer and mono documents a P cell lays out as L).
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

// LayoutMeasured is Layout with prose cells measured: each P
// column's cells wrap under m at the column's measure — its rune
// width times runeUnits, the mono advance expressed in m's units
// (em-thousandths at the drawing size). The formatted Rows reserve
// each P cell's space blank at the measured height; the measured
// lines land in RowProse for positioned drawing. When mHead is
// non-nil, every header cell is measured the same way under it
// (the header row is the table's labels, set in the body face) and
// lands in HeaderProse. Everything else — grid, splits, notes, N
// metrics — is exactly Layout.
func (t *Table) LayoutMeasured(width int, m, mHead Measurer, runeUnits int) (*TableLayout, error) {
	return t.layout(width, m, mHead, runeUnits)
}

func (t *Table) layout(width int, m, mHead Measurer, runeUnits int) (*TableLayout, error) {
	cols, grid, err := t.fit(width)
	if err != nil {
		return nil, err
	}
	tl := &TableLayout{Cols: grid.Spans(), NumCols: grid.Nums()}
	for i, col := range cols {
		tl.Aligns = append(tl.Aligns, col.align)
		if col.align == 'P' {
			tl.ProseCols = append(tl.ProseCols, tl.Cols[i])
		}
	}
	// Sep is always computable; Lines and writers gate the header
	// separator on Header presence, and total rows use it (dash
	// form) regardless.
	tl.Sep = grid.Rule(separator)
	if t.header != nil {
		if mHead != nil {
			// Measured header: every cell wraps under the header
			// measurer at its column's measure, the formatted lines
			// reserve the space blank, and the measured lines land
			// in HeaderProse for the writer to set in the body face.
			tl.HeaderProse = make([][]Line, len(cols))
			heights := make([]int, len(cols))
			for i, col := range cols {
				var s string
				if i < len(t.header) {
					s = t.header[i]
				}
				lines := wrapCellMeasured(s, col.Width*runeUnits, mHead)
				tl.HeaderProse[i] = lines
				heights[i] = max(1, len(lines))
			}
			tl.Header = renderRow(cols, grid, t.header, heights)
		} else {
			tl.Header = renderRow(cols, grid, t.header, nil)
		}
	}
	for _, row := range t.rows {
		if row.note {
			half := noteRow(cols, row.cells, 2)
			text := noteRow(cols, row.cells, 1)
			if len(tl.Rows) == 0 {
				tl.HeaderNotes = append(tl.HeaderNotes, half...)
				tl.headerNotesText = append(tl.headerNotesText, text...)
			} else {
				i := len(tl.Rows) - 1
				tl.RowNotes[i] = append(tl.RowNotes[i], half...)
				tl.rowNotesText[i] = append(tl.rowNotesText[i], text...)
			}
			continue
		}
		var prose [][]Line
		var heights []int
		if m != nil && len(tl.ProseCols) > 0 {
			prose = make([][]Line, len(tl.ProseCols))
			heights = make([]int, len(cols))
			k := 0
			for i, col := range cols {
				if col.align != 'P' {
					continue
				}
				var s string
				if i < len(row.cells) {
					s = row.cells[i]
				}
				lines := wrapCellMeasured(s, col.Width*runeUnits, m)
				prose[k] = lines
				heights[i] = max(1, len(lines))
				k++
			}
		}
		tl.Rows = append(tl.Rows, renderRow(cols, grid, row.cells, heights))
		tl.RowProse = append(tl.RowProse, prose)
		tl.Totals = append(tl.Totals, row.total)
		tl.RowNotes = append(tl.RowNotes, nil)
		tl.rowNotesText = append(tl.rowNotesText, nil)
	}
	return tl, nil
}

// wrapCellMeasured wraps prose cell content under a real measurer
// at the given measure, with the cell-tuned hyphen penalty.
func wrapCellMeasured(s string, measure int, m Measurer) []Line {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return wrapRagged(s, measure, hyphenPenaltyCell, m)
}

// noteRow formats one note row on a grid scaled by scale: scale 2 is
// the half-size grid (widths and the column gap double), scale 1 the
// full-size grid for plain-text output. Notes always left-align and
// wrap.
func noteRow(cols []colSpec, cells []string, scale int) []string {
	scaled := make([]colSpec, len(cols))
	tcols := make([]tab.Col, len(cols))
	for i, col := range cols {
		tcols[i] = tab.Col{Width: col.Width * scale, Align: 'L'}
		scaled[i] = colSpec{Col: tcols[i], align: 'L'}
	}
	return renderRow(scaled, tab.New(tcols, scale), cells, nil)
}

// fit resolves the auto-span column against the total width,
// verifies the fixed columns fit, and builds the grid with the
// N-column metrics measured over the data and total rows (notes
// never weigh in).
func (t *Table) fit(width int) ([]colSpec, *tab.Grid, error) {
	tcols := make([]tab.Col, len(t.cols))
	for i, c := range t.cols {
		tcols[i] = c.Col
	}
	resolved, err := tab.Fit(tcols, width, 1)
	if err != nil {
		return nil, nil, err
	}
	cols := make([]colSpec, len(t.cols))
	for i, c := range t.cols {
		cols[i] = colSpec{Col: resolved[i], align: c.align, clip: c.clip}
	}
	grid := tab.New(resolved, 1)
	for _, row := range t.rows {
		if !row.note {
			grid.Measure(row.cells)
		}
	}
	return cols, grid, nil
}

// renderRow wraps each cell into its column's line stack and emits
// the padded physical lines through the grid. A column with a
// positive proseHeights entry has its space reserved blank at that
// height instead (the measured lines draw positioned, outside the
// mono grid); a nil slice renders every cell.
func renderRow(cols []colSpec, grid *tab.Grid, cells []string, proseHeights []int) []string {
	stacks := make([][]string, len(cols))
	height := 1
	for i, col := range cols {
		var s string
		if i < len(cells) {
			s = cells[i]
		}
		if proseHeights != nil && proseHeights[i] > 0 {
			// Measured prose cell: blank lines hold the space.
			stacks[i] = make([]string, proseHeights[i])
		} else if col.clip || col.align == 'N' {
			// N columns never wrap: a broken number is worse
			// than a truncated one.
			stacks[i] = []string{s}
		} else {
			stacks[i] = wrapCell(s, col.Width)
		}
		if len(stacks[i]) > height {
			height = len(stacks[i])
		}
	}

	lines := make([]string, height)
	for h := range lines {
		parts := make([]string, len(cols))
		for i := range cols {
			if h < len(stacks[i]) {
				parts[i] = stacks[i][h]
			}
		}
		lines[h] = grid.Line(parts)
	}
	return lines
}

// wrapCell wraps a cell's text to width with the same Knuth-Plass
// breaker paragraphs get, but with the cell-tuned hyphen penalty:
// in a narrow column, "Isolated thunder-" / "storms inland" beats
// one word per line. A word that is longer than the column even
// after hyphenation is hard-cut into chunks. Cells hyphenate with
// every embedded pattern set (cell content is short and often
// mixed).
func wrapCell(s string, width int) []string {
	var out []string
	for _, ln := range flattenLines(wrapRagged(s, width, hyphenPenaltyCell, Mono)) {
		for len(ln) > width && runeLen(ln) > width {
			r := []rune(ln)
			out = append(out, string(r[:width]))
			ln = string(r[width:])
		}
		if ln != "" {
			out = append(out, ln)
		}
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}
