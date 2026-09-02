package tessera

import "fmt"

// The cell table (TESSERA.t, "Cells"): all 256 values defined, unassigned
// values render blank, the table grows by appending.
//
//	0x00        blank
//	0x01..0x1F  symbols: box drawing, arrows, shades, a practical tail
//	0x20..0x7E  ASCII
//	0x7F        €
//	0x80..0x87  ink: foreground palette 0..7
//	0x88..0x8F  ink: background palette 0..7
//	0x90..0x97  weather and marine: ☀ ☁ ☂ ☾ ❄ ↯ ⚓ ⚠
//	0x98..0xBF  unassigned
//	0xC0..0xFF  the Greek page

// symbolRunes maps 0x01..0x1F (index 1..31) and, at index 0, 0x7F.
var symbolRunes = [32]rune{
	'€',                                                   // 0x7F, stored at index 0
	'─', '│', '┌', '┐', '└', '┘', '├', '┤', '┬', '┴', '┼', // 0x01..0x0B
	'═', '║', '╔', '╗', '╚', '╝', // 0x0C..0x11
	'←', '↑', '→', '↓', // 0x12..0x15
	'░', '▒', '▓', // 0x16..0x18
	'°', '±', '×', '÷', '•', '·', '§', // 0x19..0x1F
}

// weatherRunes maps 0x90..0x97: sun, cloud, umbrella, moon, snowflake,
// lightning, anchor, warning.
var weatherRunes = [8]rune{'☀', '☁', '☂', '☾', '❄', '↯', '⚓', '⚠'}

// greekRunes maps 0xC0..0xFF.
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
	case b >= 0x01 && b <= 0x1F:
		return symbolRunes[b]
	case b >= 0x20 && b <= 0x7E:
		return rune(b)
	case b == 0x7F:
		return symbolRunes[0]
	case b >= 0x90 && b <= 0x97:
		return weatherRunes[b-0x90]
	case b >= 0xC0:
		return greekRunes[b-0xC0]
	default:
		return ' '
	}
}

// runeToCell is the compiler's reverse map, built from the tables.
var runeToCell = func() map[rune]byte {
	m := make(map[rune]byte, 192)
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
	for i, r := range greekRunes {
		m[r] = byte(0xC0 + i)
	}
	return m
}()

// Transcode maps content text (UTF-8) to cell bytes. A rune outside the
// repertoire is an error, never a substitution.
func Transcode(text string) ([]byte, error) {
	out := make([]byte, 0, len(text))
	for _, r := range text {
		b, ok := runeToCell[r]
		if !ok {
			return nil, fmt.Errorf("tessera: %q is outside the cell repertoire", r)
		}
		out = append(out, b)
	}
	return out, nil
}
