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
	ErrTableInvalidAlign = errors.New("typeset: column align must be L, R, or C")
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
	rows   [][]string
}

type colSpec struct {
	width int
	align byte // 'L', 'R', 'C'
	auto  bool // width computed from remaining space
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
		if alignChar != 'L' && alignChar != 'R' && alignChar != 'C' {
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
	t.rows = append(t.rows, cells)
	return t
}

// separatorRune is the character used to draw header underline rows.
// ASCII "-" guarantees exact monospace alignment in any viewer.
const separatorRune = "-"

// TableLayout is a table laid out at a concrete width. Header holds
// the header lines plus the separator row; each element of Rows is
// one data row's lines (more than one when a cell wrapped). A
// column-splitting writer treats each row as atomic and repeats
// Header after a split.
type TableLayout struct {
	Header []string
	Rows   [][]string
}

// Lines flattens the layout in order.
func (tl *TableLayout) Lines() []string {
	out := append([]string{}, tl.Header...)
	for _, r := range tl.Rows {
		out = append(out, r...)
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
	if t.header != nil {
		tl.Header = formatRow(cols, t.header)
		tl.Header = append(tl.Header, separatorLine(cols))
	}
	for _, row := range t.rows {
		tl.Rows = append(tl.Rows, formatRow(cols, row))
	}
	return tl, nil
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
	return cols, nil
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
		if col.clip {
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
			parts[i] = formatCell(s, col.width, col.align)
		}
		lines[h] = strings.TrimRight(strings.Join(parts, " "), " ")
	}
	return lines
}

// wrapCell greedily wraps a cell's words to width; a single word
// longer than width is hard-cut into chunks.
func wrapCell(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := ""
	emit := func() {
		if cur != "" {
			lines = append(lines, cur)
			cur = ""
		}
	}
	for _, w := range words {
		for runeLen(w) > width {
			emit()
			r := []rune(w)
			lines = append(lines, string(r[:width]))
			w = string(r[width:])
		}
		if w == "" {
			continue
		}
		switch {
		case cur == "":
			cur = w
		case runeLen(cur)+1+runeLen(w) <= width:
			cur += " " + w
		default:
			emit()
			cur = w
		}
	}
	emit()
	return lines
}

func separatorLine(cols []colSpec) string {
	parts := make([]string, len(cols))
	for i, col := range cols {
		parts[i] = strings.Repeat(separatorRune, col.width)
	}
	return strings.Join(parts, " ")
}

// formatCell truncates and aligns a string within a fixed width.
func formatCell(s string, width int, align byte) string {
	runes := []rune(s)
	if len(runes) > width {
		runes = runes[:width]
	}
	n := len(runes)
	pad := width - n
	switch align {
	case 'R':
		return strings.Repeat(" ", pad) + string(runes)
	case 'C':
		left := pad / 2
		right := pad - left
		return strings.Repeat(" ", left) + string(runes) + strings.Repeat(" ", right)
	default: // 'L'
		return string(runes) + strings.Repeat(" ", pad)
	}
}
