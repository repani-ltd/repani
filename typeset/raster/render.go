package raster

import (
	"strings"
	"unicode/utf8"
)

// Text renders one panel as Rows rows of Cols runes, plain: ink and
// blanks render as spaces. Rows are trimmed on the right.
func (c *Canvas) Text(panel int) []string {
	out := make([]string, c.Rows)
	var buf []byte
	for r := range c.Rows {
		buf = c.AppendText(buf[:0], panel, r)
		out[r] = string(buf)
	}
	return out
}

// AppendText appends one row of Text to dst.
func (c *Canvas) AppendText(dst []byte, panel, row int) []byte {
	cells := c.Row(panel, row)
	end := len(cells)
	for end > 0 && cells[end-1].blank() {
		end--
	}
	for _, cell := range cells[:end] {
		dst = utf8.AppendRune(dst, CellRune(cell.Glyph))
	}
	return dst
}

// The ANSI SGR sequences by palette index: foreground 30+n, background
// 40+n, with 39 and 49 the terminal's defaults for entry 0.
var sgrFG = [8]string{"\x1b[39m", "\x1b[31m", "\x1b[32m", "\x1b[33m", "\x1b[34m", "\x1b[35m", "\x1b[36m", "\x1b[37m"}
var sgrBG = [8]string{"\x1b[49m", "\x1b[41m", "\x1b[42m", "\x1b[43m", "\x1b[44m", "\x1b[45m", "\x1b[46m", "\x1b[47m"}

// ANSI renders one panel as Rows rows of exactly Cols cells with ANSI
// colors, each row reset at its end.
func (c *Canvas) ANSI(panel int) []string {
	out := make([]string, c.Rows)
	var buf []byte
	for r := range c.Rows {
		buf = c.AppendANSI(buf[:0], panel, r)
		out[r] = string(buf)
	}
	return out
}

// AppendANSI appends one row of ANSI to dst.
func (c *Canvas) AppendANSI(dst []byte, panel, row int) []byte {
	var s Ink
	dst = append(dst, "\x1b[0m"...)
	for _, cell := range c.Row(panel, row) {
		if cell.FG != s.FG {
			dst = append(dst, sgrFG[cell.FG]...)
		}
		if cell.BG != s.BG {
			dst = append(dst, sgrBG[cell.BG]...)
		}
		s = cell.Ink
		dst = utf8.AppendRune(dst, CellRune(cell.Glyph))
	}
	return append(dst, "\x1b[0m"...)
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
