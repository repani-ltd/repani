// Emphasis: the _..._ inline span, the language's single inline
// concept. The gates make escaping unnecessary (an interior
// underscore is never a marker), the scanner here is the one
// automaton every consumer shares, and Parse rejects an unclosed
// opener loudly. Documented in doc.go.
package pica

import (
	"unicode"

	"repani.com/typeset/wrap"
)

// isEmphOpen reports whether an underscore between prev and next
// opens emphasis: preceded by nothing (start of text or line),
// whitespace, or punctuation that can precede a word -- an opening
// bracket, an opening quote, or a dash -- and followed by a
// printing rune. The open set is deliberately narrower than the
// close set: joining punctuation ("repos/_attic", "pkg._foo")
// must not gate, while "_word_," needs the broad close. prev == 0
// marks start, next == 0 end. A second underscore on either side
// never gates, so "__" is always literal.
func isEmphOpen(prev, next rune) bool {
	if next == 0 || next == '_' || unicode.IsSpace(next) {
		return false
	}
	return prev == 0 || unicode.IsSpace(prev) ||
		prev == '"' || prev == '\'' ||
		unicode.In(prev, unicode.Ps, unicode.Pi, unicode.Pd)
}

// isEmphClose reports whether an underscore between prev and next
// closes emphasis: preceded by a printing rune and followed by
// nothing (end of text or line), whitespace, or punctuation.
func isEmphClose(prev, next rune) bool {
	if prev == 0 || prev == '_' || unicode.IsSpace(prev) {
		return false
	}
	return next == 0 || unicode.IsSpace(next) ||
		(next != '_' && unicode.IsPunct(next))
}

// emphWalk scans runes with the gates above, starting from the
// carried state (open == true continues a span begun earlier),
// and returns the marker indices in order -- alternating closer/
// opener relative to the initial state -- plus the final state.
func emphWalk(runes []rune, open bool) (marks []int, still bool) {
	at := func(i int) rune {
		if i < 0 || i >= len(runes) {
			return 0
		}
		return runes[i]
	}
	for i, r := range runes {
		if r != '_' {
			continue
		}
		if !open && isEmphOpen(at(i-1), at(i+1)) {
			marks = append(marks, i)
			open = true
		} else if open && isEmphClose(at(i-1), at(i+1)) {
			marks = append(marks, i)
			open = false
		}
	}
	return marks, open
}

// emphUnclosed returns the rune index of an unclosed emphasis
// opener in s, or -1 when every span closes. Parse runs it over
// every prose block, so writers only ever see balanced text.
func emphUnclosed(s string) int {
	runes := []rune(s)
	marks, open := emphWalk(runes, false)
	if !open {
		return -1
	}
	return marks[len(marks)-1]
}

// EmphSeg is one run of a prose string as segmented by
// EmphSegments: its text with the emphasis markers removed, and
// whether the run is emphasized.
type EmphSeg struct {
	Text string
	Emph bool
}

// EmphSegments splits prose into maximal runs of plain and
// emphasized text, removing the _ markers. Text with no emphasis
// returns as one plain segment. An unclosed opener (which Parse
// rejects, so parsed documents never carry one) is treated as a
// literal underscore.
func EmphSegments(s string) []EmphSeg {
	runes := []rune(s)
	marks, open := emphWalk(runes, false)
	if open {
		marks = marks[:len(marks)-1]
	}
	if len(marks) == 0 {
		return []EmphSeg{{Text: s}}
	}
	var segs []EmphSeg
	add := func(from, to int, emph bool) {
		if from < to {
			segs = append(segs, EmphSeg{Text: string(runes[from:to]), Emph: emph})
		}
	}
	prev := 0
	for k := 0; k+1 < len(marks); k += 2 {
		add(prev, marks[k], false)
		add(marks[k]+1, marks[k+1], true)
		prev = marks[k+1] + 1
	}
	add(prev, len(runes), false)
	if segs == nil {
		segs = []EmphSeg{{}}
	}
	return segs
}

// EmphLines finds a paragraph's emphasis once, over the whole
// paragraph, and maps it onto the monospace lines a breaker made of
// it -- the paragraph's words in order, gaps widened, a hyphen added
// at a break. For each line, clean is the line with each marker
// underscore blanked to a space (the grid never moves) and spans
// are the rune intervals an underline covers -- marker cells
// included, so the drawn rule occupies exactly the cells the text
// page gives to the underscores. A span open at a line's end
// underlines to the end; the next line underlines from its start.
// Scanning line by line instead would read a gate at a line's edge
// or beside an added hyphen that the paragraph does not have.
func EmphLines(para string, lines []string) (clean []string, spans [][]Span) {
	src := []rune(para)
	marks, open := emphWalk(src, false)
	if open {
		marks = marks[:len(marks)-1] // unclosed: a literal underscore
	}
	isMark := make(map[int]bool, len(marks))
	for _, i := range marks {
		isMark[i] = true
	}
	p, open := 0, false
	clean, spans = make([]string, len(lines)), make([][]Span, len(lines))
	for li, line := range lines {
		runes := []rune(line)
		start := 0 // meaningful only while inside a span
		for k, r := range runes {
			if r == ' ' {
				continue
			}
			for p < len(src) && wrap.IsBreakingSpace(src[p]) {
				p++
			}
			if p >= len(src) || src[p] != r {
				continue // a hyphen the breaker added
			}
			if isMark[p] {
				runes[k] = ' '
				if open {
					spans[li] = append(spans[li], Span{Start: start, End: k + 1})
				} else {
					start = k
				}
				open = !open
			}
			p++
		}
		if open && start < len(runes) {
			spans[li] = append(spans[li], Span{Start: start, End: len(runes)})
		}
		clean[li] = string(runes)
	}
	return clean, spans
}
