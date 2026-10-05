// Line breaking for pica: the breaker itself -- Knuth-Plass -- is
// repani.com/typeset/wrap, shared with the table language. What is
// pica's is the policy -- it always hyphenates, with
// typeset/wrap/hyphen's patterns, at the prose penalty -- and
// _emphasis_ tokenization. Every function here sets the first line
// on its own measure, first, for a paragraph opened by a run-in lead
// (a .term label); first equals width for any other.
package pica

import (
	"repani.com/typeset/wrap"
	"repani.com/typeset/wrap/hyphen"
)

// Measurer reports advance widths in abstract integer units; see
// wrap.Measurer.
type Measurer = wrap.Measurer

// Line is one wrapped line; see wrap.Line.
type Line = wrap.Line

// Mono measures text for monospace surfaces: every rune, including
// the interword space, is one unit wide.
var Mono = wrap.Mono

// WrapLines wraps ONE paragraph ragged-right under the measurer,
// with the prose hyphen penalty.
func WrapLines(para string, first, width int, m Measurer) []Line {
	return wrap.Hyphenated(para, first, width, hyphen.Default, wrap.PenaltyProse, m)
}

// JustifyLines chooses justified line breaks for a paragraph
// carrying _..._ emphasis markers (doc.go, Emphasis) and returns
// lines at natural spacing (wrap.Justify). The markers are removed,
// each emphasized token is measured with em -- the emphasis face's
// measurer, so justification stays exact when the renderer switches
// faces -- and every returned Line carries the parallel Emph flags.
// Emphasis is whole-token: punctuation attached to an emphasized
// word sets with it, the classic compositor's rule. Interword
// spaces, and the hyphen hang, stay on the body measurer m.
func JustifyLines(para string, first, width int, m, em Measurer) []Line {
	return wrap.Justify(emphTokens(para, m, em), first, width, hyphen.Default, m)
}

// JustifyText justifies ONE paragraph in monospace and flushes every
// line but the last to its measure (wrap.JustifyMono).
func JustifyText(para string, first, width int) []string {
	return wrap.JustifyMono(para, first, width, hyphen.Default)
}

// wrapText is the monospace text of WrapLines.
func wrapText(para string, first, width int) []string {
	return wrap.Flatten(WrapLines(para, first, width, Mono))
}

// emphTokens tokenizes a marked paragraph: emphSegments strips the
// markers, tokens split at breaking whitespace as wrap.Fields does,
// and a token any rune of which is emphasized is measured whole
// with em.
func emphTokens(para string, m, em Measurer) []wrap.Token {
	segs := emphSegments(para)
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
