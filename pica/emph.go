// Emphasis: the _..._ inline span, the language's single inline
// concept. The gates make escaping unnecessary (an interior
// underscore is never a marker), the scanner here is the one
// automaton every consumer shares, and Parse rejects an unclosed
// opener loudly. Documented in doc.go.
package pica

import "unicode"

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

// EmphLine scans ONE already-wrapped monospace line, carrying the
// span state across lines of a block: clean is the line with each
// marker underscore blanked to a space (the grid never moves),
// spans are the rune intervals an underline covers -- marker cells
// included, so the drawn rule occupies exactly the cells the text
// page gives to the underscores -- and still is the state handed
// to the next line. A span open at the line's end underlines to
// the end and continues; a line beginning inside a span underlines
// from its first rune.
func EmphLine(line string, open bool) (clean string, spans []Span, still bool) {
	runes := []rune(line)
	marks, still := emphWalk(runes, open)
	if len(marks) == 0 && !open {
		return line, nil, false
	}
	start := 0 // meaningful only while inside a span
	for _, i := range marks {
		runes[i] = ' '
		if open {
			spans = append(spans, Span{Start: start, End: i + 1})
		} else {
			start = i
		}
		open = !open
	}
	if open && start < len(runes) {
		spans = append(spans, Span{Start: start, End: len(runes)})
	}
	return string(runes), spans, still
}
