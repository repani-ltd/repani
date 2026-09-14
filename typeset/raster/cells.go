package raster

import "fmt"

// The cell table (RASTER.t, "Cells"), in rune tables by range. Every
// glyph is one column wide (East Asian Width not Wide) with text
// presentation, so a row of cells is a row of columns in any
// monospace renderer. The two non-ASCII stretches are contiguous,
// 0x01..0x1C and 0x7F..0xDA, and the table grows by appending.

// lowRunes maps 0x01..0x1C (index 1..28); index 0 is unused.
var lowRunes = [29]rune{
	0,
	'─', '│', // 0x01..0x02 rules
	'←', '↑', '→', '↓', // 0x03..0x06 arrows
	'░', '▒', '▓', '█', // 0x07..0x0A blocks
	'▀', '▄', // 0x0B..0x0C half blocks: a bitmap's square pixel
	'°', '±', '×', '÷', '•', '·', // 0x0D..0x12 symbols
	'┌', '┐', '└', '┘', '├', '┤', '┬', '┴', '┼', // 0x13..0x1B junctions
	'©', // 0x1C
}

// highRunes maps 0x7F..0xDA (index 0..91).
var highRunes = [92]rune{
	'€',                          // 0x7F
	'‘', '’', '“', '”', '–', '—', // 0x80..0x85 typographic
	'☺', '☹', '♥', '★', '✓', '✗', // 0x86..0x8B marks
	'●', '○', '£', // 0x8C..0x8E status, currency
	'à', 'è', 'é', 'ì', 'ò', 'ù', 'À', 'È', 'É', 'Ì', 'Ò', 'Ù', // 0x8F..0x9A Italian
	// 0x9B..0xDA monotonic Greek and its punctuation
	'α', 'β', 'γ', 'δ', 'ε', 'ζ', 'η', 'θ', 'ι', 'κ', 'λ', 'μ',
	'ν', 'ξ', 'ο', 'π', 'ρ', 'ς', 'σ', 'τ', 'υ', 'φ', 'χ', 'ψ',
	'ω', 'ά', 'έ', 'ή', 'ί', 'ό', 'ύ', 'ώ', 'ϊ', 'ϋ', 'ΐ', 'ΰ',
	'Α', 'Β', 'Γ', 'Δ', 'Ε', 'Ζ', 'Η', 'Θ', 'Ι', 'Κ', 'Λ', 'Μ',
	'Ν', 'Ξ', 'Ο', 'Π', 'Ρ', 'Σ', 'Τ', 'Υ', 'Φ', 'Χ', 'Ψ', 'Ω',
	'«', '»', '…', '―',
}

// CellRune returns the display rune of a glyph byte. Blanks and
// unassigned values render as a space.
func CellRune(b byte) rune {
	switch {
	case b >= 0x01 && b <= 0x1C:
		return lowRunes[b]
	case b >= 0x20 && b <= 0x7E:
		return rune(b)
	case b >= 0x7F && b <= 0xDA:
		return highRunes[b-0x7F]
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
	for i, r := range lowRunes {
		if i > 0 {
			m[r] = byte(i)
		}
	}
	for i, r := range highRunes {
		m[r] = byte(0x7F + i)
	}
	// Capitals with tonos or dialytika: the plain capital's cell.
	for accented, plain := range map[rune]rune{'Ά': 'Α', 'Έ': 'Ε', 'Ή': 'Η', 'Ί': 'Ι', 'Ό': 'Ο', 'Ύ': 'Υ', 'Ώ': 'Ω', 'Ϊ': 'Ι', 'Ϋ': 'Υ'} {
		m[accented] = m[plain]
	}
	return m
}()

// Transcode maps content text (UTF-8) to cell bytes. A rune outside the
// repertoire is an error, never a substitution, with one stated
// exception: a Greek capital with tonos or dialytika is transcoded to
// its plain capital, the convention of Greek typography for capitals.
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
