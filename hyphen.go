// Knuth-Liang hyphenation. TeX pattern files for English and
// Greek are embedded at compile time. Algorithm summary lives
// in doc.go.
package typeset

import (
	_ "embed"
	"strings"
	"unicode"
	"unicode/utf8"
)

//go:embed patterns/en.txt
var patternsEN string

//go:embed patterns/el.txt
var patternsEL string

// hyphenator holds compiled hyphenation patterns.
type hyphenator struct {
	patterns map[string][]int
}

var defaultHyphenator *hyphenator

func init() {
	defaultHyphenator = newHyphenator(patternsEN, patternsEL)
}

func newHyphenator(patternSets ...string) *hyphenator {
	h := &hyphenator{patterns: make(map[string][]int)}
	for _, set := range patternSets {
		h.loadPatterns(set)
	}
	return h
}

func (h *hyphenator) loadPatterns(text string) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%") || strings.HasPrefix(line, "#") {
			continue
		}
		h.addPattern(line)
	}
}

func (h *hyphenator) addPattern(pattern string) {
	var key []rune
	var levels []int

	runes := []rune(pattern)
	levels = append(levels, 0)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r >= '0' && r <= '9' {
			levels[len(levels)-1] = int(r - '0')
		} else {
			key = append(key, unicode.ToLower(r))
			levels = append(levels, 0)
		}
	}

	h.patterns[string(key)] = levels
}

// Hyphenate returns valid hyphenation points for a word as rune
// indices into the (case-preserved) original word. The returned
// indices are NOT byte offsets -- callers that slice the original
// string must convert through []rune first. Attached punctuation
// ("judgment.", "(who") is ignored: patterns and the fragment-
// length guards apply to the letter core only, so a break never
// strands punctuation with fewer than 2 letters. Returns nil for
// cores shorter than 5 runes (no break is ever placed in the
// first or last 2 characters).
func (h *hyphenator) Hyphenate(word string) []int {
	all := []rune(strings.ToLower(word))
	start, stop := 0, len(all)
	for start < stop && !unicode.IsLetter(all[start]) {
		start++
	}
	for stop > start && !unicode.IsLetter(all[stop-1]) {
		stop--
	}
	runes := all[start:stop]
	if len(runes) < 5 {
		return nil
	}

	work := make([]rune, len(runes)+2)
	work[0] = '.'
	copy(work[1:], runes)
	work[len(work)-1] = '.'

	levels := make([]int, len(work)+1)

	for i := 0; i < len(work); i++ {
		for j := i + 1; j <= len(work); j++ {
			sub := string(work[i:j])
			if pat, ok := h.patterns[sub]; ok {
				for k, v := range pat {
					if i+k < len(levels) && v > levels[i+k] {
						levels[i+k] = v
					}
				}
			}
		}
	}

	var points []int
	for i := 3; i < len(levels)-2; i++ {
		if levels[i]%2 == 1 {
			pos := i - 1
			if pos > 1 && pos < len(runes)-1 {
				points = append(points, start+pos)
			}
		}
	}

	return points
}

// runeLen returns the number of runes in a string.
func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}
