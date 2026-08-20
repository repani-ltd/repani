// Table renderer: wrapped cells by default, "!" clips a column.
// Spec syntax and rendering rules live in doc.go.
package pica

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrTableEmptySpec    = errors.New("pica: empty column spec")
	ErrTableInvalidToken = errors.New("pica: invalid column token")
	ErrTableInvalidAlign = errors.New("pica: column align must be L, R, C, N, or P")
	ErrTableAutoConflict = errors.New("pica: only one auto-span column allowed")
	ErrTableInvalidWidth = errors.New("pica: invalid column width")
	ErrTableAutoNoRoom   = errors.New("pica: no space left for auto-span column")
	ErrTableOverflow     = errors.New("pica: column widths exceed table width")
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

type colSpec struct {
	width int
	align byte // 'L', 'R', 'C', 'N', or 'P'
	auto  bool // width computed from remaining space
	clip  bool // truncate instead of wrapping ("!" suffix)

	// N-column metrics, resolved at fit time from the data rows:
	// frac is the widest fraction tail (separator included) in
	// runes; paren reserves a trailing slot for accounting
	// negatives like "(1,234.56)".
	frac  int
	paren bool
}

// NewTable parses a column spec ("3L *L 4R!") and returns an empty
// table, or an error if the spec is malformed. Whether the columns
// fit is checked at Layout, where the total width is known.
func NewTable(spec string) (*Table, error) {
	tokens := strings.Fields(spec)
	if len(tokens) == 0 {
		return nil, ErrTableEmptySpec
	}

	cols := make([]colSpec, len(tokens))
	autoSeen := false
	for i, tok := range tokens {
		if clipped, ok := strings.CutSuffix(tok, "!"); ok {
			cols[i].clip = true
			tok = clipped
		}
		if len(tok) < 2 {
			return nil, fmt.Errorf("%w: %q", ErrTableInvalidToken, tok)
		}
		alignChar := tok[len(tok)-1]
		if !strings.ContainsRune("LRCNP", rune(alignChar)) {
			return nil, fmt.Errorf("%w: %q", ErrTableInvalidAlign, tok)
		}
		cols[i].align = alignChar

		widthPart := tok[:len(tok)-1]
		if widthPart == "*" {
			if autoSeen {
				return nil, ErrTableAutoConflict
			}
			autoSeen = true
			cols[i].auto = true
			continue
		}
		w, err := strconv.Atoi(widthPart)
		if err != nil || w < 1 {
			return nil, fmt.Errorf("%w: %q", ErrTableInvalidWidth, tok)
		}
		cols[i].width = w
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
const separator = "-"

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
// the cell's offsets within a formatted line. TableLayout.Cols
// holds one per column; ProseCols the P columns' only, whose cells
// LayoutMeasured additionally wraps under a real measurer for
// writers that set prose cells in the body face (in the text
// writer and mono documents a P cell lays out as L).
type Span struct {
	Start, End int
}

// NumCol is one N column's resolved geometry on the rune grid: its
// Span, the widest fraction tail (separator included), and whether
// the column reserves the accounting paren slot. A writer
// re-rendering numeric cells in a proportional face anchors on
// SepIndex; the mono grid needs none of this (alignment is baked
// into the padded strings).
type NumCol struct {
	Span
	Frac  int
	Paren bool
}

// SepIndex is the rune cell every decimal point in the column
// occupies (one past the units digit when the column has no
// fractions). Integer parts end there; fraction tails start there.
func (c NumCol) SepIndex() int {
	slot := 0
	if c.Paren {
		slot = 1
	}
	return c.End - slot - c.Frac
}

// SplitNumeric splits a numeric cell for separator-anchored drawing:
// intPart is everything before the decimal separator (the opening
// paren included), tail everything from the separator on (the
// closing paren included). ok is false for content that does not
// read as a number — headers, "n/a" — which stays as formatted.
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
	cols, err := t.fit(width)
	if err != nil {
		return nil, err
	}
	tl := &TableLayout{}
	start := 0
	for _, col := range cols {
		span := Span{Start: start, End: start + col.width}
		tl.Cols = append(tl.Cols, span)
		tl.Aligns = append(tl.Aligns, col.align)
		switch col.align {
		case 'N':
			tl.NumCols = append(tl.NumCols, NumCol{Span: span, Frac: col.frac, Paren: col.paren})
		case 'P':
			tl.ProseCols = append(tl.ProseCols, span)
		}
		start += col.width + 1
	}
	// Sep is always computable; Lines and writers gate the header
	// separator on Header presence, and total rows use it (dash
	// form) regardless.
	tl.Sep = separatorLine(cols)
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
				lines := wrapCellMeasured(s, col.width*runeUnits, mHead)
				tl.HeaderProse[i] = lines
				heights[i] = max(1, len(lines))
			}
			tl.Header = renderRow(cols, t.header, " ", heights)
		} else {
			tl.Header = renderRow(cols, t.header, " ", nil)
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
				lines := wrapCellMeasured(s, col.width*runeUnits, m)
				prose[k] = lines
				heights[i] = max(1, len(lines))
				k++
			}
		}
		tl.Rows = append(tl.Rows, renderRow(cols, row.cells, " ", heights))
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
	for i, col := range cols {
		scaled[i] = colSpec{width: col.width * scale, align: 'L'}
	}
	return renderRow(scaled, cells, strings.Repeat(" ", scale), nil)
}

// fit resolves the auto-span column against the total width and
// verifies the fixed columns fit.
func (t *Table) fit(width int) ([]colSpec, error) {
	cols := make([]colSpec, len(t.cols))
	copy(cols, t.cols)

	fixedSum := 0
	autoIdx := -1
	for i, c := range cols {
		if c.auto {
			autoIdx = i
			continue
		}
		fixedSum += c.width
	}
	separators := len(cols) - 1
	if autoIdx >= 0 {
		remaining := width - fixedSum - separators
		if remaining < 1 {
			return nil, ErrTableAutoNoRoom
		}
		cols[autoIdx].width = remaining
	} else if fixedSum+separators > width {
		return nil, ErrTableOverflow
	}

	// Resolve N-column metrics from the data rows: the widest
	// fraction tail sets where the decimal point sits, and any
	// accounting negative reserves the trailing paren slot.
	for i := range cols {
		if cols[i].align != 'N' {
			continue
		}
		for _, row := range t.rows {
			if row.note || i >= len(row.cells) {
				continue
			}
			fracLen, hasParen, ok := numericParts(row.cells[i])
			if !ok {
				continue
			}
			if fracLen > cols[i].frac {
				cols[i].frac = fracLen
			}
			if hasParen {
				cols[i].paren = true
			}
		}
	}
	return cols, nil
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

// numericParts is SplitNumeric reduced to the column metrics: the
// rune length of the fraction tail (decimal separator included,
// closing paren excluded) and whether the cell is an accounting
// negative with a trailing ")".
func numericParts(s string) (fracLen int, hasParen bool, ok bool) {
	_, tail, ok := SplitNumeric(s)
	if !ok {
		return 0, false, false
	}
	frac, hasParen := strings.CutSuffix(tail, ")")
	return runeLen(frac), hasParen, true
}

// renderRow wraps each cell into its column's line stack and emits
// the padded physical lines, columns joined by gap. A column with a
// positive proseHeights entry has its space reserved blank at that
// height instead (the measured lines draw positioned, outside the
// mono grid); a nil slice renders every cell.
func renderRow(cols []colSpec, cells []string, gap string, proseHeights []int) []string {
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
			stacks[i] = wrapCell(s, col.width)
		}
		if len(stacks[i]) > height {
			height = len(stacks[i])
		}
	}

	lines := make([]string, height)
	for h := range lines {
		parts := make([]string, len(cols))
		for i, col := range cols {
			var s string
			if h < len(stacks[i]) {
				s = stacks[i][h]
			}
			parts[i] = formatCell(s, col)
		}
		lines[h] = strings.TrimRight(strings.Join(parts, gap), " ")
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

func separatorLine(cols []colSpec) string {
	parts := make([]string, len(cols))
	for i, col := range cols {
		parts[i] = strings.Repeat(separator, col.width)
	}
	return strings.Join(parts, " ")
}

// formatCell truncates and aligns a string within its column.
func formatCell(s string, col colSpec) string {
	if col.align == 'N' {
		s = numericCell(s, col)
	}
	width := col.width
	s = TruncLine(s, width)
	pad := width - runeLen(s)
	switch col.align {
	case 'R', 'N':
		return strings.Repeat(" ", pad) + s
	case 'C':
		left := pad / 2
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", pad-left)
	default: // 'L'
		return s + strings.Repeat(" ", pad)
	}
}

// numericCell pads a cell's right side so that, once right-aligned,
// every number in the column has its decimal point in the same rune
// position: short fractions pad out to the column's widest, and the
// paren slot stays open on cells that are not accounting negatives.
// Non-numeric cells (headers, "n/a") right-align at the units
// position. Empty cells stay empty. The column metrics come from
// the data rows only, so a header (or total) cell that happens to
// read as a number with a longer fraction than any data row cannot
// be aligned on the point; it is right-aligned flush instead of
// producing a negative pad.
func numericCell(s string, col colSpec) string {
	if s == "" {
		return s
	}
	slot := 0
	if col.paren {
		slot = 1
	}
	fracLen, hasParen, ok := numericParts(s)
	pad := col.frac + slot
	if ok {
		pad = col.frac - fracLen + slot
		if hasParen {
			pad--
		}
	}
	return s + strings.Repeat(" ", max(pad, 0))
}
