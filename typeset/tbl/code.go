package tbl

import (
	"unicode/utf8"

	"repani.com/typeset/raster"
)

// letters are the colour letters in raster's palette order, d for
// raster.Default to w for raster.White.
const letters = "drgybmcw"

// letter returns c's letter in a colour code, '?' outside the
// palette.
func letter(c raster.Color) byte {
	if int(c) < len(letters) {
		return letters[c]
	}
	return '?'
}

// colorOf returns the colour a letter names.
func colorOf(b byte) (raster.Color, bool) {
	for i := range len(letters) {
		if letters[i] == b {
			return raster.Color(i), true
		}
	}
	return 0, false
}

// Code is a colour code: a foreground and a background from raster's
// palette, each set or unset. The zero Code sets neither. A set
// raster.Default is a colour, not unset.
type Code struct {
	FG, BG       raster.Color
	HasFG, HasBG bool
}

// Over returns c with each unset half taken from lower: the
// precedence of a cell's code over its row's over its column's,
// applied one level at a time.
func (c Code) Over(lower Code) Code {
	if !c.HasFG {
		c.FG, c.HasFG = lower.FG, lower.HasFG
	}
	if !c.HasBG {
		c.BG, c.HasBG = lower.BG, lower.HasBG
	}
	return c
}

// String returns c as it is written: "r", "/y", "r/b", or "" when
// neither half is set.
func (c Code) String() string {
	var b []byte
	if c.HasFG {
		b = append(b, letter(c.FG))
	}
	if c.HasBG {
		b = append(b, '/', letter(c.BG))
	}
	return string(b)
}

// parseCode reads a colour code, s starting at source column col:
// an optional foreground letter, then optionally "/" and a
// background letter, at least one of the two.
func parseCode(s string, col int) (Code, error) {
	var c Code
	i := 0
	if i < len(s) && s[i] != '/' {
		fg, ok := colorOf(s[i])
		if !ok {
			return Code{}, errAt(0, col, ErrCode, "%s is not a colour letter (d r g y b m c w)", quote(s, i))
		}
		c.FG, c.HasFG = fg, true
		i++
	}
	if i < len(s) && s[i] == '/' {
		i++
		if i == len(s) {
			return Code{}, errAt(0, col+runes(s[:i-1]), ErrCode, "%q has no background after /", s)
		}
		bg, ok := colorOf(s[i])
		if !ok {
			return Code{}, errAt(0, col+runes(s[:i]), ErrCode, "%s is not a colour letter (d r g y b m c w)", quote(s, i))
		}
		c.BG, c.HasBG = bg, true
		i++
	}
	switch {
	case !c.HasFG && !c.HasBG:
		return Code{}, errAt(0, col, ErrCode, "empty")
	case i < len(s):
		return Code{}, errAt(0, col+runes(s[:i]), ErrCode, "%s after %q", quote(s, i), s[:i])
	}
	return c, nil
}

// quote returns the code point of s at byte i, quoted.
func quote(s string, i int) string {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return "'" + string(r) + "'"
}

// runes counts the code points in s.
func runes(s string) int { return utf8.RuneCountInString(s) }
