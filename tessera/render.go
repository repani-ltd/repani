package tessera

import (
	"fmt"
	"strings"
)

// sgr maps a palette index to its ANSI foreground code; the background
// is the same code plus 10. Entry 0 is the terminal's default.
var sgr = [8]int{39, 31, 32, 33, 34, 35, 36, 37}

// ANSI renders one panel as 28 rows of exactly 34 cells with ANSI
// colors, each row reset at its end. Ink codes render as a space in
// the state they establish.
func (p *Page) ANSI(panel int) []string {
	out := make([]string, Rows)
	for r := range Rows {
		var b strings.Builder
		var s ink
		b.WriteString("\x1b[0m")
		for _, c := range p.Row(panel, r) {
			switch {
			case c >= InkFG && c < InkBG:
				s.fg = c - InkFG
				fmt.Fprintf(&b, "\x1b[%dm ", sgr[s.fg])
			case c >= InkBG && c <= inkLast:
				s.bg = c - InkBG
				fmt.Fprintf(&b, "\x1b[%dm ", sgr[s.bg]+10)
			default:
				b.WriteRune(CellRune(c))
			}
		}
		b.WriteString("\x1b[0m")
		out[r] = b.String()
	}
	return out
}

// Layout arranges the four panels' rendered rows in reading order,
// across panels per row of panels, with a two-space gutter between
// panels and a blank line between rows of panels. Each panel must be
// exactly Rows lines; plain rows are padded to Cols cells.
func Layout(panels [][]string, across int) []string {
	if across < 1 {
		across = 1
	}
	var out []string
	for first := 0; first < len(panels); first += across {
		if first > 0 {
			out = append(out, "")
		}
		last := min(first+across, len(panels))
		for r := range Rows {
			var b strings.Builder
			for i := first; i < last; i++ {
				if i > first {
					b.WriteString("  ")
				}
				line := panels[i][r]
				b.WriteString(line)
				if n := len([]rune(line)); n < Cols && !strings.Contains(line, "\x1b") {
					b.WriteString(strings.Repeat(" ", Cols-n))
				}
			}
			out = append(out, strings.TrimRight(b.String(), " "))
		}
	}
	return out
}
