// Knuth-Plass optimal line breaking with hyphenation: the ragged
// and justified paragraph breakers used by the writers. Documented
// in doc.go.
//
// All widths flow through a Measurer, which reports advances in
// abstract integer units. The monospace measurer (Mono) counts one
// unit per rune, reproducing classic fixed-width behavior; the PDF
// writer supplies a font-metric measurer in thousandths of an em.
// The cost constants are calibrated in characters squared, so every
// slack-derived cost is normalized by the measurer's space width
// before it meets them.
package pica

import (
	"fmt"
	"math"
	"strings"
)

// Measurer reports advance widths in abstract integer units. Any
// consistent unit works: the breakers only compare widths against
// the wrap width and normalize costs by Space(). The breakers always
// measure whole tokens (a line is the sum of its tokens' widths plus
// spaces), so Width may include intra-string effects like kerning;
// it need not be additive over concatenation.
type Measurer interface {
	Width(s string) int // advance width of s
	Space() int         // natural interword space width
}

// monoMeasurer is the fixed-width measurer: one unit per rune.
type monoMeasurer struct{}

func (monoMeasurer) Width(s string) int { return runeLen(s) }
func (monoMeasurer) Space() int         { return 1 }

// Mono measures text for monospace surfaces: every rune, including
// the interword space, is one unit wide.
var Mono Measurer = monoMeasurer{}

// Line is one wrapped line: its words in order plus the natural
// width (word widths and one space per gap) in measurer units.
// Renderers join words with single spaces (monospace) or spread the
// line's slack across the gaps (justified proportional text). A
// hyphenated break leaves the "-" on the line's last word.
type Line struct {
	Words []string
	Width int
}

// LineOf assembles a Line from words, measuring the natural width
// under m (word widths plus one space per gap).
func LineOf(parts []string, m Measurer) Line {
	w := 0
	for i, p := range parts {
		if i > 0 {
			w += m.Space()
		}
		w += m.Width(p)
	}
	return Line{Words: parts, Width: w}
}

// flattenLines joins each line's words with single spaces: the
// monospace natural-spacing rendering.
func flattenLines(lines []Line) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = strings.Join(ln.Words, " ")
	}
	return out
}

// checkWidth guards the layout entry points: a non-positive width is
// a programmer error, not an input condition.
func checkWidth(width int) {
	if width <= 0 {
		panic(fmt.Sprintf("pica: width must be positive, got %d", width))
	}
}

// WrapLines wraps ONE paragraph ragged-right under the measurer,
// with the prose hyphen penalty. This is the structured primitive
// behind wrapParagraph; proportional writers consume the Lines
// directly. Hyphenation uses every embedded pattern set (the
// embedded scripts are disjoint, so the merged set is correct for
// each).
func WrapLines(para string, width int, m Measurer) []Line {
	checkWidth(width)
	return wrapRagged(para, width, hyphenPenaltyProse, m, defaultHyphenator)
}

// JustifyLines chooses justified line breaks under the measurer,
// returning lines at natural spacing: the caller distributes each
// non-final line's slack (width minus Line.Width) across its gaps.
// With a proportional measurer the slack may be negative -- a line
// may exceed width by up to a third of a space per gap -- and the
// caller compresses the gaps by that amount.
func JustifyLines(para string, width int, m Measurer) []Line {
	checkWidth(width)
	return justifyWrap(para, width, m, defaultHyphenator)
}

// JustifyParagraph wraps ONE paragraph of prose with the gap-aware
// breaker and flushes every non-final line, returning the lines.
// It is exactly JustifyLines under Mono, flattened, with each
// non-final line's slack distributed as whole spaces: the
// paragraph-level convenience for writers that already hold parsed
// Para blocks (the pica gazette).
func JustifyParagraph(para string, width int) []string {
	lines := flattenLines(JustifyLines(para, width, Mono))
	for i := 0; i < len(lines)-1; i++ {
		lines[i] = justifyLine(lines[i], width)
	}
	return lines
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

// word holds a token plus its hyphenation breakpoints (rune
// indices into text).
type word struct {
	text   string
	points []int
}

// hyphenParts splits w.text at the breakpoints up to (and
// including) point index pi (zero-based). Returns the prefix
// (suffixed with "-") and the remaining suffix. A break after an
// explicit compound hyphen ("four-line") reuses it instead of
// doubling it.
func (w word) hyphenParts(pi int) (prefix, suffix string) {
	if pi < 0 || pi >= len(w.points) {
		return w.text, ""
	}
	runes := []rune(w.text)
	cut := w.points[pi]
	prefix = string(runes[:cut])
	if runes[cut-1] != '-' {
		prefix += "-"
	}
	return prefix, string(runes[cut:])
}

// hyphenPrefixWidth returns the measured width of the prefix
// (including the trailing "-") for breakpoint pi.
func (w word) hyphenPrefixWidth(pi int, m Measurer) int {
	if pi < 0 || pi >= len(w.points) {
		return m.Width(w.text)
	}
	prefix, _ := w.hyphenParts(pi)
	return m.Width(prefix)
}

// hyphenPenaltyProse is the cost of a hyphen break in ragged
// paragraphs: high, because at prose widths a hyphen is rarely worth
// it. Narrow surfaces (table cells) use hyphenPenaltyCell, where the
// alternative -- one word per line -- costs far more vertically.
const (
	hyphenPenaltyProse = 100
	hyphenPenaltyCell  = 25
)

func wrapParagraph(para string, width int, h *hyphenator) []string {
	return flattenLines(wrapRagged(para, width, hyphenPenaltyProse, Mono, h))
}

// wrapRagged is the ragged-right Knuth-Plass breaker with the
// hyphen penalty as a parameter.
func wrapRagged(para string, width int, penalty float64, m Measurer, h *hyphenator) []Line {
	return breakLines(para, m, h, func(words []word, start, end int, cost []float64, next, hyph []int) {
		raggedDP(words, start, end, width, penalty, m, cost, next, hyph)
	})
}

// breakLines tokenizes a paragraph, runs the DP pass over all of it,
// and reconstructs the chosen lines. When a hyphen substitutes a
// suffix, the break at that position is recomputed so the next line
// accounts for the shorter token instead of the stale DP entry for
// the full word.
func breakLines(para string, m Measurer, h *hyphenator, dp func(words []word, start, end int, cost []float64, next, hyph []int)) []Line {
	tokens := strings.Fields(para)
	if len(tokens) == 0 {
		return nil
	}

	words := make([]word, len(tokens))
	for i, tok := range tokens {
		words[i] = word{
			text:   tok,
			points: h.Hyphenate(tok),
		}
	}

	n := len(words)
	cost := make([]float64, n+1)
	next := make([]int, n)
	hyph := make([]int, n)
	dp(words, 0, n, cost, next, hyph)

	var lines []Line
	for i := 0; i < n; {
		j, hp := next[i], hyph[i]
		var parts []string
		for k := i; k < j; k++ {
			parts = append(parts, words[k].text)
		}
		if hp > 0 {
			// The line ends inside words[j]: emit the hyphenated
			// prefix, substitute the suffix, and recompute the break
			// at j. j may equal i (an overlong word sets a line of
			// just its prefix); the shrinking suffix guarantees
			// progress.
			prefix, suffix := words[j].hyphenParts(hp - 1)
			parts = append(parts, prefix)
			words[j] = word{
				text:   suffix,
				points: h.Hyphenate(suffix),
			}
			dp(words, j, j+1, cost, next, hyph)
		}
		i = j
		lines = append(lines, LineOf(parts, m))
	}

	return lines
}

// raggedDP runs the backward dynamic-programming pass for positions
// [start, end), filling cost, next, and hyph with the slack^2
// ragged-right cost model. Positions >= end keep their existing
// entries, which is what lets the reconstruction recompute a single
// position after a hyphen substitution.
func raggedDP(words []word, start, end, width int, penalty float64, m Measurer, cost []float64, next, hyph []int) {
	n := len(words)
	sp := m.Space()
	spsp := float64(sp) * float64(sp)
	for i := end - 1; i >= start; i-- {
		bestCost := math.Inf(1)
		bestJ := i + 1
		bestHyph := -1
		lineLen := 0

		for j := i; j < n; j++ {
			wLen := m.Width(words[j].text)

			if j == i {
				lineLen = wLen
			} else {
				lineLen += sp + wLen
			}

			if lineLen > width {
				if j == i {
					// A word wider than the measure: set a hyphenated
					// prefix on this line if any point fits (the tail
					// cost approximates the suffix by cost[i+1]; the
					// reconstruction recomputes it exactly), else let
					// the word overflow.
					if hc, ok := tryHyphenAt(words[j], -1, width, penalty, cost[i+1], m); ok {
						bestCost, bestJ, bestHyph = hc.cost, i, hc.point
					} else {
						bestCost, bestJ, bestHyph = cost[i+1], i+1, -1
					}
					break
				}
				if len(words[j].points) > 0 {
					if hc, ok := tryHyphenAt(words[j], lineLen-wLen-sp, width, penalty, cost[j], m); ok && hc.cost < bestCost {
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
				c += slack * slack / spsp
			} else if slack > float64(width/2) {
				// Last line: only penalize if VERY short.
				c += slack * slack / 4 / spsp
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
// the current line. spaceUsed is the measured width of the line so
// far excluding the gap before w; pass -1 if w is the first
// token on the line. penalty is the fixed cost of introducing a
// hyphen. Returns the chosen breakpoint and the total cost
// (slack^2 + penalty + tail cost) if a fit exists.
func tryHyphenAt(w word, spaceUsed, width int, penalty, tailCost float64, m Measurer) (hyphenChoice, bool) {
	sp := m.Space()
	spsp := float64(sp) * float64(sp)
	for pi := len(w.points) - 1; pi >= 0; pi-- {
		partLen := w.hyphenPrefixWidth(pi, m)
		var total int
		if spaceUsed < 0 {
			total = partLen
		} else {
			total = spaceUsed + sp + partLen
		}
		if total <= width {
			slack := float64(width - total)
			return hyphenChoice{
				cost:  slack*slack/spsp + penalty + tailCost,
				point: pi + 1, // 1-based; consumed by hyphenParts as point-1
			}, true
		}
	}
	return hyphenChoice{}, false
}

// hyphenPenaltyJustify is the cost of a hyphen break in justified
// mode. Much smaller than the ragged-right penalties
// (hyphenPenaltyProse, hyphenPenaltyCell) because justification
// spreads slack across the
// inter-word gaps -- hyphenation creates an additional gap,
// spreading slack more evenly and reducing the maximum gap width.
const hyphenPenaltyJustify = 6

// finalHyphenPenalty is the extra cost of a hyphen whose suffix
// begins the paragraph's last line (TeX's \finalhyphendemerits):
// the paragraph then trails off in a bare word fragment. Steep but
// not prohibitive -- a fragment still beats a grotesquely loose
// line.
const finalHyphenPenalty = 40

// shrinkPerGap is the maximum a justified gap may compress below the
// natural space: a third of a space, TeX's interword shrinkability.
// Integer division makes it zero for the monospace measurer, which
// cannot shrink a character cell.
func shrinkPerGap(m Measurer) int { return m.Space() / 3 }

// HangHyphen is the width a line-final hyphen protrudes into the
// right margin (optical margin alignment): 70% of the hyphen's
// advance under the measurer. The justified breaker extends the wrap
// width by this amount for lines ending in "-", and renderers add it
// when distributing slack on such lines, so the flush edge runs
// through the hyphen instead of jogging left of it. Integer math
// disables it for the monospace character grid, which cannot
// protrude a fraction of a cell.
func HangHyphen(m Measurer) int { return m.Width("-") * 7 / 10 }

// gapCost is the justify cost of distributing slack over a line of
// words tokens under the measurer. The monospace measurer models
// whole extra spaces (justifyGapCost); proportional measurers
// spread slack continuously, so the cost is the squared per-gap
// widening in space-width units. Negative slack means the gaps
// compress below natural (never past the shrink allowance, which
// callers enforce); normalizing shrink by the allowance rather than
// the space width mirrors TeX's badness, making a full shrink cost
// as much as a three-space stretch.
func gapCost(slack, words int, m Measurer) float64 {
	sp := m.Space()
	if sp == 1 {
		return justifyGapCost(slack, words)
	}
	spsp := float64(sp) * float64(sp)
	if words <= 1 {
		return float64(slack) * float64(slack) / spsp * 4
	}
	s := float64(slack)
	if slack < 0 {
		return 9 * s * s / float64(words-1) / spsp
	}
	return s * s / float64(words-1) / spsp
}

// justifyGapCost computes the visual cost of distributing slack
// extra spaces across a monospace justified line containing words
// tokens. Each inter-word gap already has one natural space; the
// slack spaces are distributed as evenly as possible (some gaps get
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

// justifyWrap breaks a paragraph into lines optimised for
// justification. It uses the same backward-DP structure as
// wrapRagged but replaces the slack^2 cost with gapCost, which
// models the actual gap widths after slack distribution. It also
// attempts proactive hyphenation: even when a word fits on the
// current line, it evaluates whether splitting it would reduce gap
// widths on the justified result.
//
// Reconstruction is shared with the ragged breaker: see breakLines.
func justifyWrap(para string, width int, m Measurer, h *hyphenator) []Line {
	return breakLines(para, m, h, func(words []word, start, end int, cost []float64, next, hyph []int) {
		justifyDP(words, start, end, width, m, cost, next, hyph)
	})
}

// justifyDP runs the backward dynamic-programming pass for
// positions [start, end), filling cost, next, and hyph with the
// gap-aware justify cost model. Positions >= end keep their
// existing entries, which is what lets the reconstruction recompute
// a single position after a hyphen substitution.
func justifyDP(words []word, start, end, width int, m Measurer, cost []float64, next, hyph []int) {
	n := len(words)
	sp := m.Space()
	spsp := float64(sp) * float64(sp)
	shrink := shrinkPerGap(m)
	hang := HangHyphen(m)
	for i := end - 1; i >= start; i-- {
		bestCost := math.Inf(1)
		bestJ := i + 1
		bestHyph := -1
		lineLen := 0

		for j := i; j < n; j++ {
			wLen := m.Width(words[j].text)
			if j == i {
				lineLen = wLen
			} else {
				lineLen += sp + wLen
			}

			wordsOnLine := j - i + 1
			allow := (wordsOnLine - 1) * shrink

			if lineLen > width+hang+allow {
				if j == i {
					// A word wider than the measure: set a hyphenated
					// prefix on this line if any point fits (the tail
					// cost approximates the suffix by cost[i+1]; the
					// reconstruction recomputes it exactly), else let
					// the word overflow.
					if hc, ok := tryHyphenAtJustify(words[j], -1, width, 1, cost[i+1], m); ok {
						bestCost, bestJ, bestHyph = hc.cost, i, hc.point
					} else {
						bestCost, bestJ, bestHyph = cost[i+1], i+1, -1
					}
					break
				}
				// Try hyphenating words[j] to fit.
				if len(words[j].points) > 0 {
					if hc, ok := tryHyphenAtJustify(words[j], lineLen-wLen-sp, width, wordsOnLine, cost[j], m); ok {
						if next[j] == n {
							hc.cost += finalHyphenPenalty
						}
						if hc.cost < bestCost {
							bestCost = hc.cost
							bestJ = j
							bestHyph = hc.point
						}
					}
				}
				break
			}

			// Natural candidate: line ends after words[j],
			// stretched or (proportional measurers) shrunk within
			// the allowance. A dash-final word hangs its hyphen
			// into the margin, extending the target; the last line
			// renders at natural spacing and gets neither hang nor
			// shrink.
			target := width
			if j+1 < n && strings.HasSuffix(words[j].text, "-") {
				target += hang
			}
			slack := target - lineLen
			ok := slack >= -allow
			if j+1 == n && slack < 0 {
				ok = false
			}
			if ok {
				c := cost[j+1]
				if j+1 < n {
					c += gapCost(slack, wordsOnLine, m)
				} else if slack > width-5*sp {
					// Last line is not justified; only penalise
					// orphan lines shorter than 5 characters.
					c += float64(slack) * float64(slack) / 4 / spsp
				}
				if c < bestCost {
					bestCost = c
					bestJ = j + 1
					bestHyph = -1
				}
			}

			// Proactive hyphenation: try splitting words[j]
			// even though it fits, to tighten the line for
			// justification. Skip on last lines (not justified).
			if j+1 < n && len(words[j].points) > 0 {
				spaceUsed := -1
				if j > i {
					spaceUsed = lineLen - wLen - sp
				}
				if hc, ok := tryHyphenAtJustify(words[j], spaceUsed, width, wordsOnLine, cost[j], m); ok {
					if j > i && next[j] == n {
						hc.cost += finalHyphenPenalty
					}
					if hc.cost < bestCost {
						bestCost = hc.cost
						bestJ = j
						bestHyph = hc.point
					}
				}
			}
		}

		cost[i] = bestCost
		next[i] = bestJ
		hyph[i] = bestHyph
	}
}

// tryHyphenAtJustify evaluates all fitting hyphenation points of
// w and returns the one with lowest justified cost. Unlike
// tryHyphenAt (which returns the rightmost fit, optimal for
// ragged-right), this tries every point because the gap-aware
// cost is not monotonic in prefix length.
func tryHyphenAtJustify(w word, spaceUsed, width, wordCount int, tailCost float64, m Measurer) (hyphenChoice, bool) {
	sp := m.Space()
	shrink := shrinkPerGap(m)
	// The prefix ends in "-", which hangs into the margin.
	target := width + HangHyphen(m)
	best := hyphenChoice{}
	found := false
	for pi := len(w.points) - 1; pi >= 0; pi-- {
		partLen := w.hyphenPrefixWidth(pi, m)
		var total int
		if spaceUsed < 0 {
			total = partLen
		} else {
			total = spaceUsed + sp + partLen
		}
		if total > target+(wordCount-1)*shrink {
			continue
		}
		slack := target - total
		c := gapCost(slack, wordCount, m) + hyphenPenaltyJustify + tailCost
		if !found || c < best.cost {
			best = hyphenChoice{cost: c, point: pi + 1}
			found = true
		}
	}
	return best, found
}
