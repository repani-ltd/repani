// Table renderer: wrapped cells by default, "!" clips a column.
// Spec syntax and rendering rules live in doc.go.
package typeset

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrTableEmptySpec    = errors.New("typeset: empty column spec")
	ErrTableInvalidToken = errors.New("typeset: invalid column token")
	ErrTableInvalidAlign = errors.New("typeset: column align must be L, R, C, or N")
	ErrTableAutoConflict = errors.New("typeset: only one auto-span column allowed")
	ErrTableInvalidWidth = errors.New("typeset: invalid column width")
	ErrTableAutoNoRoom   = errors.New("typeset: no space left for auto-span column")
	ErrTableOverflow     = errors.New("typeset: column widths exceed table width")
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
	align byte // 'L', 'R', 'C', 'N'
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
		if alignChar != 'L' && alignChar != 'R' && alignChar != 'C' && alignChar != 'N' {
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
// the header lines (Sep is the dashed separator row, "" when the
// table is headerless); each element of Rows is one data row's lines
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

	headerNotesText []string   // full-grid note lines, for Lines
	rowNotesText    [][]string // parallel to Rows
}

// NumCol is one N column's resolved geometry on the rune grid: the
// cell's [Start,End) offsets within a formatted line, the widest
// fraction tail (separator included), and whether the column
// reserves the accounting paren slot. A writer re-rendering numeric
// cells in a proportional face anchors on SepIndex; the mono grid
// needs none of this (alignment is baked into the padded strings).
type NumCol struct {
	Start, End int
	Frac       int
	Paren      bool
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
	if _, _, ok = numericParts(s); !ok {
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

// Layout lays the table out to fit in width total runes. Errors when
// the fixed columns exceed width or an auto-span column has no room.
func (t *Table) Layout(width int) (*TableLayout, error) {
	cols, err := t.fit(width)
	if err != nil {
		return nil, err
	}
	tl := &TableLayout{}
	start := 0
	for _, col := range cols {
		if col.align == 'N' {
			tl.NumCols = append(tl.NumCols, NumCol{
				Start: start, End: start + col.width,
				Frac: col.frac, Paren: col.paren,
			})
		}
		start += col.width + 1
	}
	// Sep is always computable; Lines and writers gate the header
	// separator on Header presence, and total rows use it (dash
	// form) regardless.
	tl.Sep = separatorLine(cols)
	if t.header != nil {
		tl.Header = formatRow(cols, t.header)
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
		tl.Rows = append(tl.Rows, formatRow(cols, row.cells))
		tl.Totals = append(tl.Totals, row.total)
		tl.RowNotes = append(tl.RowNotes, nil)
		tl.rowNotesText = append(tl.rowNotesText, nil)
	}
	return tl, nil
}

// noteRow formats one note row on a grid scaled by scale: scale 2 is
// the half-size grid (widths and the column gap double), scale 1 the
// full-size grid for plain-text output. Notes always left-align and
// wrap.
func noteRow(cols []colSpec, cells []string, scale int) []string {
	stacks := make([][]string, len(cols))
	height := 1
	for i, col := range cols {
		var s string
		if i < len(cells) {
			s = cells[i]
		}
		stacks[i] = wrapCell(s, col.width*scale)
		if len(stacks[i]) > height {
			height = len(stacks[i])
		}
	}
	gap := strings.Repeat(" ", scale)
	lines := make([]string, height)
	for h := range lines {
		parts := make([]string, len(cols))
		for i, col := range cols {
			var s string
			if h < len(stacks[i]) {
				s = stacks[i][h]
			}
			parts[i] = formatCell(s, colSpec{width: col.width * scale, align: 'L'})
		}
		lines[h] = strings.TrimRight(strings.Join(parts, gap), " ")
	}
	return lines
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

// numericParts reports whether a cell reads as a number and, if so,
// the rune length of its fraction tail (decimal separator included)
// and whether it is an accounting negative with a trailing ")". A
// number contains at least one digit and only digits, grouping and
// sign punctuation, currency marks, or percent.
func numericParts(s string) (fracLen int, hasParen bool, ok bool) {
	if s == "" {
		return 0, false, false
	}
	hasDigit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case strings.ContainsRune(".,+-−()$€%", r):
		default:
			return 0, false, false
		}
	}
	if !hasDigit {
		return 0, false, false
	}
	core := strings.TrimSuffix(s, ")")
	hasParen = core != s
	if i := strings.LastIndex(core, "."); i >= 0 {
		fracLen = runeLen(core[i:])
	}
	return fracLen, hasParen, true
}

// formatRow renders one logical row, wrapping cells to their column
// widths; the result is one or more physical lines.
func formatRow(cols []colSpec, cells []string) []string {
	// Wrap (or clip) each cell into its column's line stack.
	stacks := make([][]string, len(cols))
	height := 1
	for i, col := range cols {
		var s string
		if i < len(cells) {
			s = cells[i]
		}
		if col.clip || col.align == 'N' {
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
	for h := 0; h < height; h++ {
		parts := make([]string, len(cols))
		for i, col := range cols {
			var s string
			if h < len(stacks[i]) {
				s = stacks[i][h]
			}
			parts[i] = formatCell(s, col)
		}
		lines[h] = strings.TrimRight(strings.Join(parts, " "), " ")
	}
	return lines
}

// wrapCell wraps a cell's text to width with the same Knuth-Plass
// breaker paragraphs get, but with the cell-tuned hyphen penalty:
// in a narrow column, "Isolated thunder-" / "storms inland" beats
// one word per line. A word that is longer than the column even
// after hyphenation is hard-cut into chunks. Cells hyphenate with
// every embedded pattern set (cell content is short and often
// mixed; .lang applies to prose).
func wrapCell(s string, width int) []string {
	var out []string
	for _, ln := range flattenLines(wrapRagged(s, width, hyphenPenaltyCell, Mono, defaultHyphenator)) {
		for runeLen(ln) > width {
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
	runes := []rune(s)
	if len(runes) > width {
		runes = runes[:width]
	}
	n := len(runes)
	pad := width - n
	switch col.align {
	case 'R', 'N':
		return strings.Repeat(" ", pad) + string(runes)
	case 'C':
		left := pad / 2
		right := pad - left
		return strings.Repeat(" ", left) + string(runes) + strings.Repeat(" ", right)
	default: // 'L'
		return string(runes) + strings.Repeat(" ", pad)
	}
}

// numericCell pads a cell's right side so that, once right-aligned,
// every number in the column has its decimal point in the same rune
// position: short fractions pad out to the column's widest, and the
// paren slot stays open on cells that are not accounting negatives.
// Non-numeric cells (headers, "n/a") right-align at the units
// position. Empty cells stay empty.
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
	return s + strings.Repeat(" ", pad)
}
