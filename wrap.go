// Knuth-Plass optimal line breaking with hyphenation, plus the
// exported prose utilities Wrap, Justify, and TruncLines. The
// public API and conventions are documented in doc.go.
package typeset

import (
	"fmt"
	"strings"
)

// checkWidth guards the layout entry points: a non-positive width is
// a programmer error, not an input condition.
func checkWidth(width int) {
	if width <= 0 {
		panic(fmt.Sprintf("typeset: width must be positive, got %d", width))
	}
}

// Wrap reflows blank-line-separated paragraphs of PROSE to fit
// width, using optimal (Knuth-Plass) line breaking with hyphenation.
// Lines are left ragged-right; lines containing 2+ consecutive
// internal spaces are treated as preformatted and pass through
// unchanged. Structured documents (headings, tables, verbatim
// blocks) belong in the Parse -> Doc pipeline instead: Wrap cannot
// tell a narrow table row from prose.
func Wrap(text string, width int) string {
	checkWidth(width)
	return reflow(text, width, wrapParagraph)
}

// Justify reflows paragraphs like Wrap, then distributes extra
// spaces on each non-final line so both edges are flush. The line
// breaker uses a gap-aware cost model: it evaluates the actual
// inter-word gap widths after justification and prefers hyphenation
// over wide gaps, since monospace output cannot hide fractional
// space adjustments. The last line of each paragraph stays
// left-aligned. Preformatted lines pass through as in Wrap.
func Justify(text string, width int) string {
	checkWidth(width)
	return reflow(text, width, JustifyParagraph)
}

// JustifyParagraph wraps ONE paragraph of prose with the gap-aware
// breaker and flushes every non-final line, returning the lines.
// This is the paragraph-level primitive for writers that already
// hold parsed Para blocks (the pica gazette); Justify is the
// document-level convenience over raw prose.
func JustifyParagraph(para string, width int) []string {
	checkWidth(width)
	lines := wrapParagraphJustify(para, width)
	for i := 0; i < len(lines)-1; i++ {
		lines[i] = justifyLine(lines[i], width)
	}
	return lines
}

// TruncLines truncates every line in text to width runes. Lines
// already within the limit pass through unchanged. No reflow, no
// hyphenation -- just a cut.
func TruncLines(text string, width int) string {
	checkWidth(width)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = truncLine(line, width)
	}
	return strings.Join(lines, "\n")
}

// reflow applies perPara to every prose paragraph of text, passing
// blank lines and preformatted lines through unchanged.
func reflow(text string, width int, perPara func(string, int) []string) string {
	paragraphs := splitBlankSeparated(text)
	var out []string
	for _, para := range paragraphs {
		if para == "" {
			out = append(out, "")
			continue
		}
		// Preformatted lines come through splitBlankSeparated as
		// standalone single-line entries; never reflow them.
		if isVerbatimLine(para) {
			out = append(out, para)
			continue
		}
		out = append(out, perPara(para, width)...)
	}
	return strings.Join(out, "\n")
}

// justifyLine distributes extra spaces between words so the line
// fills exactly width runes. Single-word lines and lines already
// at or over width are returned unchanged.
func justifyLine(line string, width int) string {
	words := strings.Fields(line)
	if len(words) <= 1 {
		return line
	}
	totalWordLen := 0
	for _, w := range words {
		totalWordLen += runeLen(w)
	}
	gaps := len(words) - 1
	totalSpaces := width - totalWordLen
	if totalSpaces <= gaps {
		return line
	}
	base := totalSpaces / gaps
	extra := totalSpaces % gaps
	var b strings.Builder
	for i, w := range words {
		b.WriteString(w)
		if i < gaps {
			n := base
			if i < extra {
				n++
			}
			for s := 0; s < n; s++ {
				b.WriteByte(' ')
			}
		}
	}
	return b.String()
}

// isVerbatimLine is the language's per-line verbatim convention: a
// line containing 2+ consecutive internal spaces (column-aligned
// content) is never refilled. Shared by Parse and the prose
// utilities so the rule has exactly one home.
func isVerbatimLine(trimmed string) bool {
	return strings.Contains(trimmed, "  ")
}

// splitBlankSeparated returns paragraphs separated by blank lines.
// Each prose paragraph is the lines joined by single spaces.
// Blank lines and preformatted lines (containing 2+ consecutive
// spaces) are represented as standalone entries.
func splitBlankSeparated(text string) []string {
	lines := strings.Split(text, "\n")
	var paragraphs []string
	var current []string

	flush := func() {
		if len(current) > 0 {
			paragraphs = append(paragraphs, strings.Join(current, " "))
			current = nil
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flush()
			paragraphs = append(paragraphs, "")
			continue
		}
		// Preformatted lines (column-aligned content) are kept as-is.
		if isVerbatimLine(trimmed) {
			flush()
			paragraphs = append(paragraphs, line)
			continue
		}
		current = append(current, trimmed)
	}
	flush()
	return paragraphs
}

// word holds a token plus its hyphenation breakpoints (rune
// indices into text).
type word struct {
	text   string
	points []int
}

// hyphenParts splits w.text at the breakpoints up to (and
// including) point index pi (zero-based). Returns the prefix
// (suffixed with "-") and the remaining suffix.
func (w word) hyphenParts(pi int) (prefix, suffix string) {
	if pi < 0 || pi >= len(w.points) {
		return w.text, ""
	}
	runes := []rune(w.text)
	cut := w.points[pi]
	return string(runes[:cut]) + "-", string(runes[cut:])
}

// hyphenPrefixLen returns the rune length of the prefix
// (including the trailing "-") for breakpoint pi.
func (w word) hyphenPrefixLen(pi int) int {
	if pi < 0 || pi >= len(w.points) {
		return runeLen(w.text)
	}
	return w.points[pi] + 1 // +1 for the "-"
}

// hyphenPenaltyProse is the cost of a hyphen break in ragged
// paragraphs: high, because at prose widths a hyphen is rarely worth
// it. Narrow surfaces (table cells) use hyphenPenaltyCell, where the
// alternative -- one word per line -- costs far more vertically.
const (
	hyphenPenaltyProse = 100
	hyphenPenaltyCell  = 25
)

func wrapParagraph(para string, width int) []string {
	return wrapRagged(para, width, hyphenPenaltyProse)
}

// wrapRagged is the ragged-right Knuth-Plass breaker with the
// hyphen penalty as a parameter.
func wrapRagged(para string, width int, penalty float64) []string {
	tokens := strings.Fields(para)
	if len(tokens) == 0 {
		return nil
	}

	words := make([]word, len(tokens))
	for i, tok := range tokens {
		words[i] = word{
			text:   tok,
			points: defaultHyphenator.Hyphenate(tok),
		}
	}

	n := len(words)
	cost := make([]float64, n+1)
	next := make([]int, n)
	hyph := make([]int, n)

	cost[n] = 0
	raggedDP(words, 0, n, width, penalty, cost, next, hyph)

	// Reconstruct lines from the DP solution. When a hyphen
	// substitutes a suffix, recompute the break at that position so
	// the next line accounts for the shorter token instead of the
	// stale DP entry for the full word.
	var lines []string
	i := 0
	for i < n {
		j := next[i]
		h := hyph[i]
		var parts []string

		if h > 0 {
			// Words [i..j) followed by the prefix of words[j].
			for k := i; k < j; k++ {
				parts = append(parts, words[k].text)
			}
			prefix, suffix := words[j].hyphenParts(h - 1)
			parts = append(parts, prefix)
			words[j] = word{
				text:   suffix,
				points: defaultHyphenator.Hyphenate(suffix),
			}
			raggedDP(words, j, min(j+1, n), width, penalty, cost, next, hyph)
			i = j
		} else {
			// Words [i..j) form a complete line.
			for k := i; k < j; k++ {
				parts = append(parts, words[k].text)
			}
			i = j
		}

		lines = append(lines, strings.Join(parts, " "))
	}

	return lines
}

// raggedDP runs the backward dynamic-programming pass for positions
// [start, end), filling cost, next, and hyph with the slack^2
// ragged-right cost model. Positions >= end keep their existing
// entries, which is what lets the reconstruction recompute a single
// position after a hyphen substitution.
func raggedDP(words []word, start, end, width int, penalty float64, cost []float64, next, hyph []int) {
	n := len(words)
	for i := end - 1; i >= start; i-- {
		bestCost := 1e18
		bestJ := i + 1
		bestHyph := -1
		lineLen := 0

		for j := i; j < n; j++ {
			wLen := runeLen(words[j].text)

			if j == i {
				lineLen = wLen
			} else {
				lineLen += 1 + wLen
			}

			if lineLen > width {
				if j == i && wLen > width {
					// Single word longer than width: break the line
					// after it (overflow). Hyphenation alone cannot help.
					bestJ = i + 1
					bestHyph = -1
					bestCost = cost[i+1]
					break
				}
				if len(words[j].points) > 0 && j > i {
					if hc, ok := tryHyphenAt(words[j], lineLen-wLen-1, width, penalty, cost[j]); ok && hc.cost < bestCost {
						bestCost = hc.cost
						bestJ = j
						bestHyph = hc.point
					}
				}
				break
			}

			slack := float64(width - lineLen)
			c := cost[j+1]
			if j+1 < n {
				c += slack * slack
			} else if slack > float64(width/2) {
				// Last line: only penalize if VERY short.
				c += slack * slack / 4
			}
			if c < bestCost {
				bestCost = c
				bestJ = j + 1
				bestHyph = -1
			}
		}

		cost[i] = bestCost
		next[i] = bestJ
		hyph[i] = bestHyph
	}
}

// hyphenChoice is the result of evaluating a hyphenation point.
type hyphenChoice struct {
	cost  float64
	point int // 1-based hyphen breakpoint index (>=1)
}

// tryHyphenAt evaluates whether word w can be broken to fit on
// the current line. spaceUsed is the rune count of the line so
// far excluding the gap before w; pass -1 if w is the first
// token on the line. penalty is the fixed cost of introducing a
// hyphen. Returns the chosen breakpoint and the total cost
// (slack^2 + penalty + tail cost) if a fit exists.
func tryHyphenAt(w word, spaceUsed, width int, penalty, tailCost float64) (hyphenChoice, bool) {
	for pi := len(w.points) - 1; pi >= 0; pi-- {
		partLen := w.hyphenPrefixLen(pi)
		var total int
		if spaceUsed < 0 {
			total = partLen
		} else {
			total = spaceUsed + 1 + partLen
		}
		if total <= width {
			slack := float64(width - total)
			return hyphenChoice{
				cost:  slack*slack + penalty + tailCost,
				point: pi + 1, // 1-based; consumed by hyphenParts as point-1
			}, true
		}
	}
	return hyphenChoice{}, false
}

// hyphenPenaltyJustify is the cost of a hyphen break in justified
// mode. Much smaller than the ragged-right penalty (100 in
// tryHyphenAt) because monospace justification distributes slack
// as whole extra spaces between words -- hyphenation creates an
// additional inter-word gap, spreading slack more evenly and
// reducing the maximum gap width.
const hyphenPenaltyJustify = 6

// justifyGapCost computes the visual cost of distributing slack
// extra spaces across a justified line containing words tokens.
// Each inter-word gap already has one natural space; the slack
// spaces are distributed as evenly as possible (some gaps get
// floor(slack/gaps) extra, the rest get ceil). The cost is the
// sum of squared extra-space counts, which penalises lines where
// some gaps are much wider than others.
//
// A single-token line (no gaps) receives a heavy penalty because
// it cannot be justified at all and the slack becomes trailing
// whitespace.
func justifyGapCost(slack, words int) float64 {
	if words <= 1 {
		return float64(slack * slack * 4)
	}
	gaps := words - 1
	base := slack / gaps
	extra := slack % gaps
	return float64(extra*(base+1)*(base+1) + (gaps-extra)*base*base)
}

// wrapParagraphJustify breaks a paragraph into lines optimised
// for monospace justification. It uses the same backward-DP
// structure as wrapParagraph but replaces the slack^2 cost with
// justifyGapCost, which models the actual gap widths after space
// distribution. It also attempts proactive hyphenation: even
// when a word fits on the current line, it evaluates whether
// splitting it would reduce gap widths on the justified result.
//
// During reconstruction, when a hyphenated suffix replaces the
// original word, the break decision at that position is
// recomputed so subsequent lines see the correct (shorter) token
// instead of the stale DP entry for the full word.
func wrapParagraphJustify(para string, width int) []string {
	tokens := strings.Fields(para)
	if len(tokens) == 0 {
		return nil
	}

	words := make([]word, len(tokens))
	for i, tok := range tokens {
		words[i] = word{
			text:   tok,
			points: defaultHyphenator.Hyphenate(tok),
		}
	}

	n := len(words)
	cost := make([]float64, n+1)
	next := make([]int, n)
	hyph := make([]int, n)

	cost[n] = 0

	justifyDP(words, 0, n, width, cost, next, hyph)

	// Reconstruct lines from the DP solution. When a hyphen
	// substitutes a suffix, recompute the break at that position
	// so the next line accounts for the shorter token.
	var lines []string
	i := 0
	for i < n {
		j := next[i]
		h := hyph[i]
		var parts []string

		if h > 0 {
			for k := i; k < j; k++ {
				parts = append(parts, words[k].text)
			}
			prefix, suffix := words[j].hyphenParts(h - 1)
			parts = append(parts, prefix)
			words[j] = word{
				text:   suffix,
				points: defaultHyphenator.Hyphenate(suffix),
			}
			recomputeBreakAt(words, j, n, width, cost, next, hyph)
			i = j
		} else {
			for k := i; k < j; k++ {
				parts = append(parts, words[k].text)
			}
			i = j
		}

		lines = append(lines, strings.Join(parts, " "))
	}

	return lines
}

// justifyDP runs the backward dynamic-programming pass for
// positions [start, n). It fills cost, next, and hyph for each
// position using the gap-aware justify cost model.
func justifyDP(words []word, start, n, width int, cost []float64, next, hyph []int) {
	for i := n - 1; i >= start; i-- {
		bestCost := 1e18
		bestJ := i + 1
		bestHyph := -1
		lineLen := 0

		for j := i; j < n; j++ {
			wLen := runeLen(words[j].text)
			if j == i {
				lineLen = wLen
			} else {
				lineLen += 1 + wLen
			}

			wordsOnLine := j - i + 1

			if lineLen > width {
				if j == i && wLen > width {
					bestJ = i + 1
					bestHyph = -1
					bestCost = cost[i+1]
					break
				}
				// Try hyphenating words[j] to fit.
				if len(words[j].points) > 0 {
					spaceUsed := lineLen - wLen - 1
					if j == i {
						spaceUsed = -1
					}
					if hc, ok := tryHyphenAtJustify(words[j], spaceUsed, width, wordsOnLine, cost[j]); ok && hc.cost < bestCost {
						bestCost = hc.cost
						bestJ = j
						bestHyph = hc.point
					}
				}
				break
			}

			// Line fits with words[j]. Non-hyphenated cost.
			slack := width - lineLen
			c := cost[j+1]
			if j+1 < n {
				c += justifyGapCost(slack, wordsOnLine)
			} else if slack > width-5 {
				// Last line is not justified; only penalise
				// orphan lines shorter than 5 characters.
				c += float64(slack*slack) / 4
			}
			if c < bestCost {
				bestCost = c
				bestJ = j + 1
				bestHyph = -1
			}

			// Proactive hyphenation: try splitting words[j]
			// even though it fits, to tighten the line for
			// justification. Skip on last lines (not justified).
			if j+1 < n && len(words[j].points) > 0 {
				spaceUsed := -1
				if j > i {
					spaceUsed = lineLen - wLen - 1
				}
				if hc, ok := tryHyphenAtJustify(words[j], spaceUsed, width, wordsOnLine, cost[j]); ok && hc.cost < bestCost {
					bestCost = hc.cost
					bestJ = j
					bestHyph = hc.point
				}
			}
		}

		cost[i] = bestCost
		next[i] = bestJ
		hyph[i] = bestHyph
	}
}

// recomputeBreakAt re-runs the justify DP from position pos
// after a hyphenation suffix has replaced the original word
// there. Only non-hyphenated breaks are evaluated; the
// pre-computed cost array is reused for subsequent positions
// (which still hold their original words).
func recomputeBreakAt(words []word, pos, n, width int, cost []float64, next, hyph []int) {
	bestCost := 1e18
	bestJ := pos + 1
	lineLen := 0

	for j := pos; j < n; j++ {
		wLen := runeLen(words[j].text)
		if j == pos {
			lineLen = wLen
		} else {
			lineLen += 1 + wLen
		}
		if lineLen > width {
			break
		}
		wordsOnLine := j - pos + 1
		slack := width - lineLen
		c := cost[j+1]
		if j+1 < n {
			c += justifyGapCost(slack, wordsOnLine)
		} else if slack > width-5 {
			c += float64(slack*slack) / 4
		}
		if c < bestCost {
			bestCost = c
			bestJ = j + 1
		}
	}

	cost[pos] = bestCost
	next[pos] = bestJ
	hyph[pos] = -1
}

// tryHyphenAtJustify evaluates all fitting hyphenation points of
// w and returns the one with lowest justified cost. Unlike
// tryHyphenAt (which returns the rightmost fit, optimal for
// ragged-right), this tries every point because the gap-aware
// cost is not monotonic in prefix length.
func tryHyphenAtJustify(w word, spaceUsed, width, wordCount int, tailCost float64) (hyphenChoice, bool) {
	best := hyphenChoice{}
	found := false
	for pi := len(w.points) - 1; pi >= 0; pi-- {
		partLen := w.hyphenPrefixLen(pi)
		var total int
		if spaceUsed < 0 {
			total = partLen
		} else {
			total = spaceUsed + 1 + partLen
		}
		if total > width {
			continue
		}
		slack := width - total
		c := justifyGapCost(slack, wordCount) + hyphenPenaltyJustify + tailCost
		if !found || c < best.cost {
			best = hyphenChoice{cost: c, point: pi + 1}
			found = true
		}
	}
	return best, found
}
