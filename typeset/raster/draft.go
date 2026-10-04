package raster

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Draft is a row being written: a producer puts text at columns, in
// any order, overwriting as it likes, and Row derives the canonical
// record, so that no producer computes segments or counts link
// columns. A Draft is not part of the format; it holds a glyph,
// colours and a target per column only until Row.
type Draft struct {
	glyph  [Width]rune
	fg, bg [Width]Color
	target [Width]string
}

// NewDraft returns a row of spaces on background bg, with no links.
func NewDraft(bg Color) *Draft {
	d := new(Draft)
	for i := range d.glyph {
		d.glyph[i], d.bg[i] = ' ', bg
	}
	return d
}

// Columns returns the columns s takes once in NFC, one per code
// point, or an error if a code point does not take exactly one.
func Columns(s string) (int, error) {
	if !utf8.ValidString(s) {
		return 0, fmt.Errorf("invalid UTF-8")
	}
	n := 0
	for _, c := range norm.NFC.String(s) {
		if err := checkColumn(c); err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

// Put writes s in NFC from column col in foreground fg on background
// bg, linked to target ("" for none), and returns the column after
// it. Text that would run past the row, or a code point that does
// not take one column, is an error and writes nothing.
func (d *Draft) Put(col int, s string, fg, bg Color, target string) (int, error) {
	n, err := Columns(s)
	if err != nil {
		return col, fmt.Errorf("raster: put %q: %w", s, err)
	}
	if col < 0 || col+n > Width {
		return col, fmt.Errorf("raster: put %q at column %d: past the row", s, col)
	}
	for _, r := range norm.NFC.String(s) {
		d.glyph[col], d.fg[col], d.bg[col], d.target[col] = r, fg, bg, target
		col++
	}
	return col, nil
}

// Row derives row index's record and checks it. A change of
// background or target starts a segment at its column; a change of
// foreground starts one at the glyph that shows it, the spaces
// before it staying with the glyphs before them. A link whose glyphs
// are in more than one foreground is an error.
func (d *Draft) Row(index int) (Row, error) {
	r := Row{Index: index}
	for start := 0; start < Width; {
		end := start + 1
		for end < Width && d.bg[end] == d.bg[start] && d.target[end] == d.target[start] {
			end++
		}
		segs, err := d.split(start, end)
		if err != nil {
			return Row{}, fmt.Errorf("raster: row %d: %w", index, err)
		}
		r.Segments = append(r.Segments, segs...)
		start = end
	}
	return r, r.Check()
}

// split returns columns start to end, one background and one target,
// as segments: one for a link, one per foreground for plain text.
func (d *Draft) split(start, end int) ([]Segment, error) {
	var segs []Segment
	open := func(col int, fg Color) {
		segs = append(segs, Segment{Start: col, FG: fg, BG: d.bg[start], Target: d.target[start]})
	}
	open(start, Default)
	seen := false // whether the open segment holds a glyph
	for col := start; col < end; col++ {
		g, fg := d.glyph[col], d.fg[col]
		cur := &segs[len(segs)-1]
		switch {
		case g == ' ':
		case !seen:
			cur.FG, seen = fg, true
		case fg == cur.FG:
		case cur.IsLink():
			return nil, fmt.Errorf("link to %s at column %d: a link is one colour, not %s and %s", cur.Target, col, cur.FG, fg)
		default:
			open(col, fg)
		}
	}
	var text strings.Builder
	for i := range segs {
		s := &segs[i]
		s.End = end
		if i+1 < len(segs) {
			s.End = segs[i+1].Start
		}
		text.Reset()
		for col := s.Start; col < s.End; col++ {
			text.WriteRune(d.glyph[col])
		}
		s.Text = text.String()
	}
	return segs, nil
}
