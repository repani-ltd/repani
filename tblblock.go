// Table block expansion: replace .table ... .end blocks in text
// with rendered, fixed-width tables. Block syntax lives in doc.go.
package typeset

import (
	"strconv"
	"strings"
)

// ExpandTables walks text and replaces every .table block with the
// rendered table output. The .table and .end markers are consumed
// and do not appear in the output. A block's total width is the
// optional leading integer of its spec ("​.table 44 3L *L 4R"),
// defaulting to DefaultWidth.
func ExpandTables(text string) (string, error) {
	lines := strings.Split(text, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		spec, ok := tableStart(lines[i])
		if !ok {
			out = append(out, lines[i])
			i++
			continue
		}
		// Collect rows until .end (or EOF).
		end := i + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != ".end" {
			end++
		}
		rendered, err := renderTableBlock(spec, lines[i+1:end])
		if err != nil {
			return "", err
		}
		out = append(out, strings.Split(rendered, "\n")...)
		if end < len(lines) {
			i = end + 1
		} else {
			i = end
		}
	}
	return strings.Join(out, "\n"), nil
}

// tableStart returns the column spec if the line is a .table marker.
func tableStart(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if t == ".table" || strings.HasPrefix(t, ".table ") {
		spec := strings.TrimSpace(strings.TrimPrefix(t, ".table"))
		return spec, true
	}
	return "", false
}

// renderTableBlock builds a Table from rows and renders it. A
// leading all-digits token in spec is the block's total width.
// First row is treated as the header. Empty rows are skipped.
func renderTableBlock(spec string, rows []string) (string, error) {
	width := DefaultWidth
	if first, rest, ok := strings.Cut(spec, " "); ok {
		if w, err := strconv.Atoi(first); err == nil {
			width = w
			spec = rest
		}
	}
	tbl, err := NewTable(spec, width)
	if err != nil {
		return "", err
	}

	first := true
	for _, line := range rows {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cells := splitTableCells(line)
		if first {
			tbl.Header(cells...)
			first = false
		} else {
			tbl.Row(cells...)
		}
	}
	return tbl.Render(), nil
}

// splitTableCells splits a row by "|" and trims whitespace from each cell.
func splitTableCells(line string) []string {
	parts := strings.Split(line, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}
