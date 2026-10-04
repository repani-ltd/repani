// Line breaking for pica: the breaker itself -- Knuth-Plass with
// Knuth-Liang hyphenation -- is repani.com/typeset/wrap, shared with
// the table language. This file keeps pica's names for it and adds
// the one thing that is pica's own: _emphasis_ tokenization.
package pica

import (
	"unicode/utf8"

	"repani.com/typeset/wrap"
)

// Measurer reports advance widths in abstract integer units; see
// wrap.Measurer.
type Measurer = wrap.Measurer

// Line is one wrapped line; see wrap.Line.
type Line = wrap.Line

// Mono measures text for monospace surfaces: every rune, including
// the interword space, is one unit wide.
var Mono = wrap.Mono

// LineOf assembles a Line from words, measuring the natural width
// under m (word widths plus one space per gap).
func LineOf(parts []string, m Measurer) Line { return wrap.LineOf(parts, m) }

// WrapLines wraps ONE paragraph ragged-right under the measurer,
// with the prose hyphen penalty.
func WrapLines(para string, width int, m Measurer) []Line {
	return wrap.Ragged(para, width, wrap.PenaltyProse, m)
}

// WrapLinesRunIn is WrapLines with the first line on the measure
// first, what a run-in lead (a .term label) leaves of it.
func WrapLinesRunIn(para string, first, width int, m Measurer) []Line {
	return wrap.RaggedRunIn(para, first, width, wrap.PenaltyProse, m)
}

// JustifyLines chooses justified line breaks under the measurer,
// returning lines at natural spacing; see wrap.Justify.
func JustifyLines(para string, width int, m Measurer) []Line {
	return wrap.Justify(para, width, m)
}

// JustifyParagraph wraps ONE paragraph with the justified breaker
// under Mono and flushes every non-final line to width.
func JustifyParagraph(para string, width int) []string {
	return wrap.JustifyParagraph(para, width)
}

// JustifyParagraphRunIn is JustifyParagraph with the first line on
// the measure first.
func JustifyParagraphRunIn(para string, first, width int) []string {
	return wrap.JustifyParagraphRunIn(para, first, width)
}

// HangHyphen is the width a line-final hyphen protrudes into the
// right margin under m; see wrap.HangHyphen.
func HangHyphen(m Measurer) int { return wrap.HangHyphen(m) }

// JustifyLinesEmph is JustifyLines for a paragraph carrying _..._
// emphasis markers (doc.go, Emphasis): the markers are removed,
// each emphasized token is measured with em -- the emphasis face's
// measurer, so justification stays exact when the renderer switches
// faces -- and every returned Line carries the parallel Emph flags.
// Emphasis is whole-token: punctuation attached to an emphasized
// word sets with it, the classic compositor's rule. Interword
// spaces, and the hyphen hang, stay on the body measurer m. A
// paragraph without markers behaves exactly as JustifyLines.
func JustifyLinesEmph(para string, width int, m, em Measurer) []Line {
	return wrap.JustifyTokens(emphTokens(para, m, em), width, width, m)
}

// JustifyLinesEmphRunIn is JustifyLinesEmph with the first line on
// the measure first (see WrapLinesRunIn).
func JustifyLinesEmphRunIn(para string, first, width int, m, em Measurer) []Line {
	return wrap.JustifyTokens(emphTokens(para, m, em), first, width, m)
}

// emphTokens tokenizes a marked paragraph: EmphSegments strips the
// markers, tokens split at breaking whitespace as wrap.Fields does,
// and a token any rune of which is emphasized is measured whole
// with em.
func emphTokens(para string, m, em Measurer) []wrap.Token {
	segs := EmphSegments(para)
	var clean []rune
	var flags []bool
	for _, sg := range segs {
		for _, r := range sg.Text {
			clean = append(clean, r)
			flags = append(flags, sg.Emph)
		}
	}
	var toks []wrap.Token
	start := -1 // rune index where the current token began
	tokEmph := false
	flush := func(end int) {
		if start < 0 {
			return
		}
		mm := m
		if tokEmph {
			mm = em
		}
		toks = append(toks, wrap.Token{Text: string(clean[start:end]), M: mm, Emph: tokEmph})
		start, tokEmph = -1, false
	}
	for i, r := range clean {
		if wrap.IsBreakingSpace(r) {
			flush(i)
			continue
		}
		if start < 0 {
			start = i
		}
		if flags[i] {
			tokEmph = true
		}
	}
	flush(len(clean))
	return toks
}

// wrapParagraph is the monospace text of WrapLines.
func wrapParagraph(para string, width int) []string {
	return wrap.Flatten(WrapLines(para, width, Mono))
}

// wrapParagraphRunIn is wrapParagraph with the first line on the
// measure first: the monospace text of WrapLinesRunIn.
func wrapParagraphRunIn(para string, first, width int) []string {
	return wrap.Flatten(WrapLinesRunIn(para, first, width, Mono))
}

// runeLen returns the number of runes in a string.
func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}
