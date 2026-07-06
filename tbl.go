// Table renderer. Column spec syntax and rendering rules live
// in doc.go.
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

// Table is a fixed-width table builder.
type Table struct {
	cols   []colSpec
	width  int
	header []string
	rows   [][]string
}

type colSpec struct {
	width int
	align byte // 'L', 'R', 'C'
	auto  bool // true if width is computed from remaining space
}

// NewTable parses a column spec and returns a table that fits in
// width total columns, or an error if the spec is malformed or does
// not fit.
func NewTable(spec string, width int) (*Table, error) {
	tokens := strings.Fields(spec)
	if len(tokens) == 0 {
		return nil, ErrTableEmptySpec
	}

	cols := make([]colSpec, len(tokens))
	autoIdx := -1
	fixedSum := 0

	for i, tok := range tokens {
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
			if autoIdx >= 0 {
				return nil, ErrTableAutoConflict
			}
			autoIdx = i
			cols[i].auto = true
			continue
		}
		w, err := strconv.Atoi(widthPart)
		if err != nil || w < 1 {
			return nil, fmt.Errorf("%w: %q", ErrTableInvalidWidth, tok)
		}
		cols[i].width = w
		fixedSum += w
	}

	// Compute auto-span column width.
	separators := len(cols) - 1
	if autoIdx >= 0 {
		remaining := width - fixedSum - separators
		if remaining < 1 {
			return nil, ErrTableAutoNoRoom
		}
		cols[autoIdx].width = remaining
	} else {
		if fixedSum+separators > width {
			return nil, ErrTableOverflow
		}
	}

	return &Table{cols: cols, width: width}, nil
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
// ASCII "-" guarantees exact monospace alignment in any viewer
// (Unicode box-drawing chars like U+2500 may be rendered slightly
// wider than monospace by some text editors and terminals).
const separatorRune = "-"

// Render produces the formatted table as a string with newline-separated
// lines. The header (if set) is followed by a separator row drawn with
// separatorRune.
func (t *Table) Render() string {
	var out []string

	if t.header != nil {
		out = append(out, t.formatRow(t.header))
		out = append(out, t.separator())
	}
	for _, row := range t.rows {
		out = append(out, t.formatRow(row))
	}

	return strings.Join(out, "\n")
}

func (t *Table) formatRow(cells []string) string {
	parts := make([]string, len(t.cols))
	for i, col := range t.cols {
		var s string
		if i < len(cells) {
			s = cells[i]
		}
		parts[i] = formatCell(s, col.width, col.align)
	}
	return strings.Join(parts, " ")
}

func (t *Table) separator() string {
	parts := make([]string, len(t.cols))
	for i, col := range t.cols {
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
