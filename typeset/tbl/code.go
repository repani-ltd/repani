package tbl

import (
	"fmt"
	"unicode/utf8"
)

// Color is a palette index, 0 to 7, as raster's palette numbers it.
type Color uint8

const (
	Default Color = iota
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	White
)

// letters are the colour letters in palette order.
const letters = "drgybmcw"

// Letter returns c's letter in a colour code, '?' outside the
// palette.
func (c Color) Letter() byte {
	if int(c) < len(letters) {
		return letters[c]
	}
	return '?'
}

func (c Color) String() string {
	names := [...]string{"default", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}
	if int(c) < len(names) {
		return names[c]
	}
	return fmt.Sprintf("Color(%d)", uint8(c))
}

// colorOf returns the colour a letter names.
func colorOf(b byte) (Color, bool) {
	for i := range len(letters) {
		if letters[i] == b {
			return Color(i), true
		}
	}
	return 0, false
}

// Code is a colour code: a foreground and a background, each set or
// unset. The zero Code sets neither. A set Default is a colour, not
// unset.
type Code struct {
	FG, BG       Color
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
		b = append(b, c.FG.Letter())
	}
	if c.HasBG {
		b = append(b, '/', c.BG.Letter())
	}
	return string(b)
}

// ParseCode reads a colour code, s starting at source column col:
// an optional foreground letter, then optionally "/" and a
// background letter, at least one of the two.
func ParseCode(s string, col int) (Code, error) {
	var c Code
	i := 0
	if i < len(s) && s[i] != '/' {
		fg, ok := colorOf(s[i])
		if !ok {
			return Code{}, errAt(col, ErrCode, "%s is not a colour letter (d r g y b m c w)", quote(s, i))
		}
		c.FG, c.HasFG = fg, true
		i++
	}
	if i < len(s) && s[i] == '/' {
		i++
		if i == len(s) {
			return Code{}, errAt(col+runes(s[:i-1]), ErrCode, "%q has no background after /", s)
		}
		bg, ok := colorOf(s[i])
		if !ok {
			return Code{}, errAt(col+runes(s[:i]), ErrCode, "%s is not a colour letter (d r g y b m c w)", quote(s, i))
		}
		c.BG, c.HasBG = bg, true
		i++
	}
	switch {
	case !c.HasFG && !c.HasBG:
		return Code{}, errAt(col, ErrCode, "empty")
	case i < len(s):
		return Code{}, errAt(col+runes(s[:i]), ErrCode, "%s after %q", quote(s, i), s[:i])
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
