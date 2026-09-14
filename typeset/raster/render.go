package raster

import "unicode/utf8"

// lines renders every row through app, one string a row.
func (r *Raster) lines(app func(dst []byte, row int) []byte) []string {
	out := make([]string, r.Height())
	var buf []byte
	for i := range out {
		buf = app(buf[:0], i)
		out[i] = string(buf)
	}
	return out
}

// Text renders the raster as Height rows of plain text: blanks render
// as spaces, ink is dropped, rows are trimmed on the right.
func (r *Raster) Text() []string { return r.lines(r.AppendText) }

// AppendText appends one row of Text to dst.
func (r *Raster) AppendText(dst []byte, row int) []byte {
	if row >= len(r.Rows) {
		return dst
	}
	cells := r.Rows[row][:]
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

// ANSI renders the raster as Height rows of exactly Cols cells with
// ANSI colors, each row reset at its end.
func (r *Raster) ANSI() []string { return r.lines(r.AppendANSI) }

// AppendANSI appends one row of ANSI to dst.
func (r *Raster) AppendANSI(dst []byte, row int) []byte {
	var blank Row
	cells := blank[:]
	if row < len(r.Rows) {
		cells = r.Rows[row][:]
	}
	var s Ink
	dst = append(dst, "\x1b[0m"...)
	for _, cell := range cells {
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
