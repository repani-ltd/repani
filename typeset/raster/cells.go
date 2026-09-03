package raster

import "fmt"

// The cell table (RASTER.t, "Cells"), in rune tables by range. Every
// glyph is one column wide (East Asian Width not Wide) with text
// presentation, so a row of cells is a row of columns in any
// monospace renderer.

// symbolRunes maps 0x01..0x10 (index 1..16) and, at index 0, 0x7F.
var symbolRunes = [17]rune{
	'€',      // 0x7F, stored at index 0
	'─', '│', // 0x01..0x02 rules
	'←', '↑', '→', '↓', // 0x03..0x06 arrows
	'░', '▒', '▓', '█', // 0x07..0x0A blocks
	'°', '±', '×', '÷', '•', '·', // 0x0B..0x10 symbols
}

// weatherRunes maps 0x90..0x96: sun, cloud, umbrella, moon, snowflake,
// lightning, warning.
var weatherRunes = [7]rune{'☀', '☁', '☂', '☾', '❄', '↯', '⚠'}

// typoRunes maps 0x97..0x9C: the quotes and dashes text generators
// emit by default.
var typoRunes = [6]rune{'‘', '’', '“', '”', '–', '—'}

// markRunes maps 0x9D..0xA5: smile, sad, heart, star, check, cross,
// full and empty status dots, pound.
var markRunes = [9]rune{'☺', '☹', '♥', '★', '✓', '✗', '●', '○', '£'}

// greekRunes maps 0xC0..0xFF: monotonic Greek and its punctuation.
var greekRunes = [64]rune{
	'α', 'β', 'γ', 'δ', 'ε', 'ζ', 'η', 'θ', 'ι', 'κ', 'λ', 'μ',
	'ν', 'ξ', 'ο', 'π', 'ρ', 'ς', 'σ', 'τ', 'υ', 'φ', 'χ', 'ψ',
	'ω', 'ά', 'έ', 'ή', 'ί', 'ό', 'ύ', 'ώ', 'ϊ', 'ϋ', 'ΐ', 'ΰ',
	'Α', 'Β', 'Γ', 'Δ', 'Ε', 'Ζ', 'Η', 'Θ', 'Ι', 'Κ', 'Λ', 'Μ',
	'Ν', 'Ξ', 'Ο', 'Π', 'Ρ', 'Σ', 'Τ', 'Υ', 'Φ', 'Χ', 'Ψ', 'Ω',
	'«', '»', '…', '―',
}

// CellRune returns the display rune of a cell byte. Blanks, ink codes
// and unassigned values render as a space.
func CellRune(b byte) rune {
	switch {
	case b >= 0x01 && b <= 0x10:
		return symbolRunes[b]
	case b >= 0x20 && b <= 0x7E:
		return rune(b)
	case b == 0x7F:
		return symbolRunes[0]
	case b >= 0x90 && b <= 0x96:
		return weatherRunes[b-0x90]
	case b >= 0x97 && b <= 0x9C:
		return typoRunes[b-0x97]
	case b >= 0x9D && b <= 0xA5:
		return markRunes[b-0x9D]
	case b >= 0xC0:
		return greekRunes[b-0xC0]
	default:
		return ' '
	}
}

// runeToCell is the compiler's reverse map, built from the tables.
var runeToCell = func() map[rune]byte {
	m := make(map[rune]byte, 256)
	for i := 0x20; i <= 0x7E; i++ {
		m[rune(i)] = byte(i)
	}
	for i, r := range symbolRunes {
		if i == 0 {
			m[r] = 0x7F
		} else {
			m[r] = byte(i)
		}
	}
	for i, r := range weatherRunes {
		m[r] = byte(0x90 + i)
	}
	for i, r := range typoRunes {
		m[r] = byte(0x97 + i)
	}
	for i, r := range markRunes {
		m[r] = byte(0x9D + i)
	}
	for i, r := range greekRunes {
		m[r] = byte(0xC0 + i)
	}
	return m
}()

// Transcode maps content text (UTF-8) to cell bytes. A rune outside the
// repertoire is an error, never a substitution.
func Transcode(text string) ([]byte, error) {
	return AppendTranscode(make([]byte, 0, len(text)), text)
}

// AppendTranscode is Transcode appending to dst.
func AppendTranscode(dst []byte, text string) ([]byte, error) {
	for _, r := range text {
		if r >= 0x20 && r <= 0x7E {
			dst = append(dst, byte(r))
			continue
		}
		b, ok := runeToCell[r]
		if !ok {
			return nil, fmt.Errorf("raster: %q is outside the cell repertoire", r)
		}
		dst = append(dst, b)
	}
	return dst, nil
}
