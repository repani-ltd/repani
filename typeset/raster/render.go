package raster

import (
	"fmt"
	"strings"
)

// Text renders one panel as Rows rows of Cols runes, plain: ink and
// blanks render as spaces. Rows are trimmed on the right.
func (c *Canvas) Text(panel int) []string {
	out := make([]string, c.Rows)
	for r := range c.Rows {
		runes := make([]rune, c.Cols)
		for x, cell := range c.Row(panel, r) {
			runes[x] = CellRune(cell.Glyph)
		}
		end := c.Cols
		for end > 0 && runes[end-1] == ' ' {
			end--
		}
		out[r] = string(runes[:end])
	}
	return out
}

// sgr maps a palette index to its ANSI foreground code; the background
// is the same code plus 10. Entry 0 is the terminal's default.
var sgr = [8]int{39, 31, 32, 33, 34, 35, 36, 37}

// ANSI renders one panel as Rows rows of exactly Cols cells with ANSI
// colors, each row reset at its end.
func (c *Canvas) ANSI(panel int) []string {
	out := make([]string, c.Rows)
	for r := range c.Rows {
		var b strings.Builder
		var s Ink
		b.WriteString("\x1b[0m")
		for _, cell := range c.Row(panel, r) {
			if cell.FG != s.FG {
				fmt.Fprintf(&b, "\x1b[%dm", sgr[cell.FG])
			}
			if cell.BG != s.BG {
				fmt.Fprintf(&b, "\x1b[%dm", sgr[cell.BG]+10)
			}
			s = cell.Ink
			b.WriteRune(CellRune(cell.Glyph))
		}
		b.WriteString("\x1b[0m")
		out[r] = b.String()
	}
	return out
}

// Text is Decode(p).Text.
func (p *Page) Text(panel int) []string { return Decode(p).Text(panel) }

// ANSI is Decode(p).ANSI.
func (p *Page) ANSI(panel int) []string { return Decode(p).ANSI(panel) }

// Layout arranges panels' rendered rows in reading order, across
// panels per row of panels, with a two-space gutter between panels
// and a blank line between rows of panels. Every panel must have the
// same number of lines; plain rows are padded to cols cells.
func Layout(panels [][]string, cols, across int) []string {
	if across < 1 {
		across = 1
	}
	var out []string
	for first := 0; first < len(panels); first += across {
		if first > 0 {
			out = append(out, "")
		}
		last := min(first+across, len(panels))
		for r := range panels[first] {
			var b strings.Builder
			for i := first; i < last; i++ {
				if i > first {
					b.WriteString("  ")
				}
				line := panels[i][r]
				b.WriteString(line)
				if n := len([]rune(line)); n < cols && !strings.Contains(line, "\x1b") {
					b.WriteString(strings.Repeat(" ", cols-n))
				}
			}
			out = append(out, strings.TrimRight(b.String(), " "))
		}
	}
	return out
}

// Rendered returns every panel through render, in order: the shape
// Layout takes.
func (p *Page) Rendered(render func(panel int) []string) [][]string {
	out := make([][]string, p.Panels)
	for i := range out {
		out[i] = render(i)
	}
	return out
}
