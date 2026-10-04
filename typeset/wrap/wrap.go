// Package wrap breaks text into lines: Knuth-Plass optimal line
// breaking, ragged and justified, for paragraphs and for table
// cells. Pica's writers and the table language (typeset/tbl) share
// it, so a paragraph or a cell breaks the same wherever it is set.
//
// Hyphenation is optional and lives apart, in typeset/wrap/hyphen:
// a breaker given a Hyphenator may also end a line inside a word at
// one of its points; given nil it breaks only between words. A
// program that never hyphenates imports this package alone and
// links no patterns.
//
// Every line a breaker returns fits its measure, but one: a single
// rune wider than the measure, which sets alone. A word wider than
// the measure is broken across lines: at its hyphenation points
// when one fits, otherwise CUT at the longest prefix that fits, with
// no hyphen added -- one rune at the least, so a measure narrower
// than a rune still advances. Nothing is dropped; truncating is the
// caller's.
//
// All widths flow through a Measurer, which reports advances in
// abstract integer units. The monospace measurer (Mono) counts one
// unit per rune, reproducing classic fixed-width behavior; the PDF
// writer supplies a font-metric measurer in thousandths of an em.
// The cost constants are calibrated in characters squared, so every
// slack-derived cost is normalized by the measurer's space width
// before it meets them.
package wrap

import (
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Hyphenator finds where a word may be hyphenated: ascending rune
// indices into word, each strictly inside it, at which a line may
// end with the word's prefix and a hyphen. A point just after an
// explicit hyphen ("four-|line") reuses that hyphen instead of
// adding one. hyphen.Default is the implementation; a nil
// Hyphenator hyphenates nothing.
type Hyphenator interface {
	Hyphenate(word string) []int
}

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
// Emph is set only by JustifyTokens when a token is emphasized:
// parallel to Words, true for words the renderer sets in the
// emphasis face. It is nil everywhere else.
type Line struct {
	Words []string
	Width int
	Emph  []bool
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

// Flatten joins each line's words with single spaces: the monospace
// natural-spacing rendering.
func Flatten(lines []Line) []string {
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
		panic(fmt.Sprintf("wrap: width must be positive, got %d", width))
	}
}

// Ragged wraps ONE paragraph ragged-right under the measurer without
// hyphenation: the optimal breaks between words, and a word wider
// than the measure cut. It is the light path, for programs that
// import no patterns. Proportional writers consume the Lines
// directly; Flatten gives monospace text.
func Ragged(para string, width int, m Measurer) []Line {
	checkWidth(width)
	return wrapRagged(para, width, width, nil, 0, m)
}

// Hyphenated is Ragged that may also end a line inside a word at one
// of h's points, each hyphen costing penalty: PenaltyProse for
// paragraphs, PenaltyCell for table cells.
func Hyphenated(para string, width int, h Hyphenator, penalty float64, m Measurer) []Line {
	checkWidth(width)
	return wrapRagged(para, width, width, h, penalty, m)
}

// Justify chooses justified line breaks under the measurer,
// hyphenating at h's points (nil: none), returning lines at natural
// spacing: the caller distributes each non-final line's slack (width
// minus Line.Width) across its gaps. With a proportional measurer
// the slack may be negative -- a line may exceed width by up to a
// third of a space per gap -- and the caller compresses the gaps by
// that amount.
func Justify(para string, width int, h Hyphenator, m Measurer) []Line {
	checkWidth(width)
	return justifyWrap(para, width, h, m)
}

// Cell wraps a table cell's text to width runes in monospace,
// hyphenating at h's points (nil: none) with the cell-tuned penalty:
// in a narrow column "Isolated thunder-" / "storms inland" beats one
// word per line. Every line fits; empty text is one empty line.
func Cell(s string, width int, h Hyphenator) []string {
	checkWidth(width)
	out := Flatten(wrapRagged(s, width, width, h, PenaltyCell, Mono))
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// JustifyParagraph wraps ONE paragraph of prose with the gap-aware
// breaker and flushes every non-final line, returning the lines.
// It is exactly Justify under Mono, flattened, with each
// non-final line's slack distributed as whole spaces: the
// paragraph-level convenience for writers that already hold parsed
// Para blocks (the pica press).
func JustifyParagraph(para string, width int, h Hyphenator) []string {
	lines := Justify(para, width, h, Mono)
	out := make([]string, len(lines))
	for i, ln := range lines {
		if i < len(lines)-1 {
			out[i] = JustifyLine(ln, width)
		} else {
			out[i] = strings.Join(ln.Words, " ")
		}
	}
	return out
}

// JustifyLine distributes extra spaces between a mono line's words
// so the line fills exactly width runes. Single-word lines and lines
// already at or over width are joined at natural spacing.
func JustifyLine(ln Line, width int) string {
	gaps := len(ln.Words) - 1
	totalSpaces := width - (ln.Width - gaps) // width minus the word runes
	if gaps < 1 || totalSpaces <= gaps {
		return strings.Join(ln.Words, " ")
	}
	base := totalSpaces / gaps
	extra := totalSpaces % gaps
	var b strings.Builder
	n := totalSpaces
	for _, w := range ln.Words {
		n += len(w)
	}
	b.Grow(n)
	for i, w := range ln.Words {
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

// word holds a token, its measured width, and its hyphenation
// breakpoints (rune indices into text) with the measured width of
// each hyphenated prefix (trailing "-" included), so the DP probes
// never re-measure. Prefixes are measured in order only while they
// stay within the word's limit, the widest a line can take, so
// prefix may be shorter than points: a point past it never fits. m
// is the measurer that owns the token's face (hyphen substitution
// re-measures the suffix with it); h is the hyphenator that found
// the points, or nil; emph marks tokens measured with the emphasis
// face, carried into Line.Emph.
type word struct {
	text   string
	width  int
	points []int
	prefix []int // the first len(prefix) of points
	m      Measurer
	h      Hyphenator
	limit  int
	emph   bool
}

// newWord tokenizes one word under m: hyphenation points from h
// (none when nil) and the widths the breakers compare.
func newWord(text string, m Measurer, h Hyphenator, limit int) word {
	w := word{text: text, width: m.Width(text), m: m, h: h, limit: limit}
	if h != nil {
		w.points = h.Hyphenate(text)
	}
	if len(w.points) > 0 {
		// points are ascending rune indices inside text: walk the
		// bytes once, measuring each prefix where its rune starts,
		// until one is wider than any line.
		k, ri := 0, 0
		for bi := range text {
			if k < len(w.points) && ri == w.points[k] {
				prefix := text[:bi]
				if text[bi-1] != '-' {
					prefix += "-"
				}
				pw := m.Width(prefix)
				if pw > limit {
					break
				}
				w.prefix = append(w.prefix, pw)
				k++
			}
			ri++
		}
	}
	return w
}

// lineLimit is the widest prefix worth measuring on a paragraph
// whose lines set on first and width: twice the wider measure,
// beyond any hyphen hang and justified shrink.
func lineLimit(first, width int) int { return 2 * max(first, width) }

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

// split breaks w, wider than its line, into the pieces that each fill
// a line, the first on lw and the rest on width, until what is left
// fits or is one rune: each piece ends at the rightmost hyphenation
// point whose prefix, hyphen included, fits the line and its hang,
// or failing one is cut at the longest prefix that fits, one rune at
// the least. Pieces after the first break at w's own points, shifted,
// not at a fresh hyphenation of the remainder. The work is linear in
// the pieces' length, however long w is: nothing measures what is
// left until it may fit.
func (w word) split(lw, width, hang int) (pieces []string, rest string) {
	r := []rune(w.text)
	o, pi := 0, 0 // runes consumed; the first point past o
	for len(r)-o > 1 {
		n := fitRunes(w.m, r[o:], lw)
		if n == len(r)-o {
			break
		}
		for pi < len(w.points) && w.points[pi] <= o {
			pi++
		}
		reach := n
		if hang > 0 {
			reach = fitRunes(w.m, r[o:], lw+hang)
		}
		piece, cut := "", 0
		for k := pi; k < len(w.points) && w.points[k]-o <= reach; k++ {
			p := string(r[o:w.points[k]])
			if r[w.points[k]-1] != '-' {
				p += "-"
			}
			if w.m.Width(p) <= lw+hang {
				piece, cut = p, w.points[k]
			}
		}
		if piece == "" {
			cut = o + max(n, 1)
			piece = string(r[o:cut])
		}
		pieces = append(pieces, piece)
		o, lw = cut, width
	}
	return pieces, string(r[o:])
}

// fitRunes is the length of the longest prefix of r no wider than lw
// under m. It gallops to bracket the answer and then bisects, so it
// measures prefixes near the answer's length, never all of r.
func fitRunes(m Measurer, r []rune, lw int) int {
	lo, hi := 0, 1 // lo fits; hi is untested
	for hi <= len(r) && m.Width(string(r[:hi])) <= lw {
		lo, hi = hi, 2*hi
	}
	hi = min(hi, len(r)+1) // now hi does not fit, or is past the end
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if m.Width(string(r[:mid])) <= lw {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

// multiRune reports whether s holds more than one rune: only such a
// word can be split.
func multiRune(s string) bool {
	_, size := utf8.DecodeRuneInString(s)
	return len(s) > size
}

// runeLen returns the number of runes in a string.
func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}

// PenaltyProse is the cost of a hyphen break in ragged paragraphs:
// high, because at prose widths a hyphen is rarely worth it. Narrow
// surfaces (table cells) use PenaltyCell, where the alternative --
// one word per line -- costs far more vertically.
const (
	PenaltyProse = 100
	PenaltyCell  = 25
)

// wrapRagged is the ragged-right Knuth-Plass breaker, the first line
// on its own measure (see HyphenatedRunIn), hyphenating at h's
// points with the given penalty.
func wrapRagged(para string, first, width int, h Hyphenator, penalty float64, m Measurer) []Line {
	sp := m.Space()
	words := monoWords(para, m, h, lineLimit(first, width))
	return breakLines(words, first, width, sp, 0, func(words []word, start, end int, cost []float64, next, hyph []int) {
		raggedDP(words, start, end, first, width, penalty, sp, cost, next, hyph)
	})
}

// HyphenatedRunIn is Hyphenated for a paragraph whose first line
// opens with a run-in lead (a label) that is NOT part of the
// paragraph: the first line sets on the measure first -- what the
// lead leaves of the line -- and every later line on width. The lead
// itself is the caller's to place; the lines returned are the
// paragraph's own words. Both measures must be positive.
func HyphenatedRunIn(para string, first, width int, h Hyphenator, penalty float64, m Measurer) []Line {
	checkWidth(first)
	checkWidth(width)
	return wrapRagged(para, first, width, h, penalty, m)
}

// monoWords tokenizes a paragraph with every token on one measurer:
// the unstyled path behind Ragged and Justify.
func monoWords(para string, m Measurer, h Hyphenator, limit int) []word {
	tokens := Fields(para)
	words := make([]word, len(tokens))
	for i, tok := range tokens {
		words[i] = newWord(tok, m, h, limit)
	}
	return words
}

// overlong marks, in hyph, a line that starts with a word wider than
// its measure: the reconstruction breaks the word into pieces.
const overlong = -2

// breakLines runs the DP pass over pre-measured words and
// reconstructs the chosen lines; the first line sets on first, every
// later line on width, sp is the interword space width and hang the
// hyphen's hang, in measurer units. When a hyphen substitutes a
// suffix, the break at that position is recomputed so the next line
// accounts for the shorter token instead of the stale DP entry for
// the full word; an overlong word is split the same way, its last
// piece taking the word's place.
func breakLines(words []word, first, width, sp, hang int, dp func(words []word, start, end int, cost []float64, next, hyph []int)) []Line {
	if len(words) == 0 {
		return nil
	}
	styled := false
	for _, w := range words {
		if w.emph {
			styled = true
			break
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
		if hp == overlong {
			// words[i] alone is wider than its line. Each piece that
			// fills a line becomes one; what is left fits and takes
			// the word's place, and the break at i is recomputed for
			// it. (Under a run-in, a remainder at word 0 is set on
			// the first line's measure again: narrower, still fits.)
			lw := width
			if len(lines) == 0 {
				lw = first
			}
			was := words[i]
			pieces, rest := was.split(lw, width, hang)
			for _, p := range pieces {
				ln := Line{Words: []string{p}, Width: was.m.Width(p)}
				if styled {
					ln.Emph = []bool{was.emph}
				}
				lines = append(lines, ln)
			}
			words[i] = newWord(rest, was.m, was.h, was.limit)
			words[i].emph = was.emph
			dp(words, i, i+1, cost, next, hyph)
			continue
		}
		parts := make([]string, 0, j-i+1)
		var emph []bool
		if styled {
			emph = make([]bool, 0, j-i+1)
		}
		natural := 0
		for k := i; k < j; k++ {
			parts = append(parts, words[k].text)
			if styled {
				emph = append(emph, words[k].emph)
			}
			natural += words[k].width + sp
		}
		if hp > 0 {
			// The line ends inside words[j]: emit the hyphenated
			// prefix, substitute the suffix, and recompute the break
			// at j. The suffix keeps its token's face and flag.
			prefix, suffix := words[j].hyphenParts(hp - 1)
			parts = append(parts, prefix)
			if styled {
				emph = append(emph, words[j].emph)
			}
			natural += words[j].prefix[hp-1] + sp
			was := words[j]
			words[j] = newWord(suffix, was.m, was.h, was.limit)
			words[j].emph = was.emph
			dp(words, j, j+1, cost, next, hyph)
		}
		i = j
		lines = append(lines, Line{Words: parts, Width: natural - sp, Emph: emph})
	}

	return lines
}

// raggedDP runs the backward dynamic-programming pass for positions
// [start, end), filling cost, next, and hyph with the slack^2
// ragged-right cost model. Positions >= end keep their existing
// entries, which is what lets the reconstruction recompute a single
// position after a hyphen substitution.
func raggedDP(words []word, start, end, first, measure int, penalty float64, sp int, cost []float64, next, hyph []int) {
	n := len(words)
	spsp := float64(sp) * float64(sp)
	for i := end - 1; i >= start; i-- {
		// The paragraph's first line (the one starting at word 0)
		// sets on its own measure: a run-in lead may occupy part
		// of it (HyphenatedRunIn); every other line sets on measure.
		width := measure
		if i == 0 {
			width = first
		}
		bestCost := math.Inf(1)
		bestJ := i + 1
		bestHyph := -1
		lineLen := 0

		for j := i; j < n; j++ {
			wLen := words[j].width

			if j == i {
				lineLen = wLen
			} else {
				lineLen += sp + wLen
			}

			if lineLen > width {
				if j == i {
					// A word wider than the measure: the
					// reconstruction splits it (word.split). Its cost
					// is a hyphenated first piece's if a point fits,
					// the tail approximated by cost[i+1], else the
					// tail alone. A single rune cannot split: it
					// overflows.
					if !multiRune(words[j].text) {
						bestCost, bestJ, bestHyph = cost[i+1], i+1, -1
					} else if hc, ok := tryHyphenAt(words[j], -1, width, penalty, cost[i+1], sp); ok {
						bestCost, bestJ, bestHyph = hc.cost, i, overlong
					} else {
						bestCost, bestJ, bestHyph = cost[i+1], i, overlong
					}
					break
				}
				if len(words[j].prefix) > 0 {
					if hc, ok := tryHyphenAt(words[j], lineLen-wLen-sp, width, penalty, cost[j], sp); ok && hc.cost < bestCost {
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
// hyphen; sp is the measurer's space width. Returns the chosen
// breakpoint and the total cost (slack^2 + penalty + tail cost) if a
// fit exists.
func tryHyphenAt(w word, spaceUsed, width int, penalty, tailCost float64, sp int) (hyphenChoice, bool) {
	spsp := float64(sp) * float64(sp)
	for pi := len(w.prefix) - 1; pi >= 0; pi-- {
		partLen := w.prefix[pi]
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
// (PenaltyProse, PenaltyCell) because justification
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
// natural space sp: a third of a space, TeX's interword
// shrinkability. Integer division makes it zero for the monospace
// measurer, which cannot shrink a character cell.
func shrinkPerGap(sp int) int { return sp / 3 }

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
// words tokens under a measurer whose space width is sp. The
// monospace measurer models
// whole extra spaces (justifyGapCost); proportional measurers
// spread slack continuously, so the cost is the squared per-gap
// widening in space-width units. Negative slack means the gaps
// compress below natural (never past the shrink allowance, which
// callers enforce); normalizing shrink by the allowance rather than
// the space width mirrors TeX's badness, making a full shrink cost
// as much as a three-space stretch.
func gapCost(slack, words int, sp int) float64 {
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
func justifyWrap(para string, width int, h Hyphenator, m Measurer) []Line {
	return justifyBreak(monoWords(para, m, h, lineLimit(width, width)), width, width, m.Space(), HangHyphen(m))
}

// justifyBreak runs the justified breaker over pre-measured words:
// the shared tail of Justify and JustifyTokens. The first line sets
// on first, every later line on width (equal except under a run-in
// lead; see HyphenatedRunIn).
func justifyBreak(words []word, first, width, sp, hang int) []Line {
	return breakLines(words, first, width, sp, hang, func(words []word, start, end int, cost []float64, next, hyph []int) {
		justifyDP(words, start, end, first, width, sp, hang, cost, next, hyph)
	})
}

// JustifyParagraphRunIn is JustifyParagraph with the first line on
// the measure first (a run-in lead occupies the rest of it, see
// HyphenatedRunIn): the first line flushes to first, every later
// non-final line to width.
func JustifyParagraphRunIn(para string, first, width int, h Hyphenator) []string {
	checkWidth(first)
	checkWidth(width)
	lines := justifyBreak(monoWords(para, Mono, h, lineLimit(first, width)), first, width, 1, 0)
	out := make([]string, len(lines))
	for i, ln := range lines {
		switch {
		case i == len(lines)-1:
			out[i] = strings.Join(ln.Words, " ")
		case i == 0:
			out[i] = JustifyLine(ln, first)
		default:
			out[i] = JustifyLine(ln, width)
		}
	}
	return out
}

// Token is one word measured in its own face: M measures it, and
// Emph marks it for the renderer's emphasis face (Line.Emph). A
// caller that styles words -- pica's _emphasis_ -- tokenizes them
// itself, at breaking spaces as Fields does.
type Token struct {
	Text string
	M    Measurer
	Emph bool
}

// JustifyTokens is Justify over pre-split tokens, each measured with
// its own measurer, so justification stays exact when the renderer
// switches faces; every returned Line carries the parallel Emph
// flags when any token is emphasized. The first line sets on first,
// every later line on width, hyphenating at h's points (nil: none).
// Interword spaces, and the hyphen hang, stay on the body measurer m.
func JustifyTokens(toks []Token, first, width int, h Hyphenator, m Measurer) []Line {
	checkWidth(first)
	checkWidth(width)
	words := make([]word, len(toks))
	for i, t := range toks {
		words[i] = newWord(t.Text, t.M, h, lineLimit(first, width))
		words[i].emph = t.Emph
	}
	return justifyBreak(words, first, width, m.Space(), HangHyphen(m))
}

// justifyDP runs the backward dynamic-programming pass for
// positions [start, end), filling cost, next, and hyph with the
// gap-aware justify cost model. Positions >= end keep their
// existing entries, which is what lets the reconstruction recompute
// a single position after a hyphen substitution.
func justifyDP(words []word, start, end, first, measure, sp, hang int, cost []float64, next, hyph []int) {
	n := len(words)
	spsp := float64(sp) * float64(sp)
	shrink := shrinkPerGap(sp)
	for i := end - 1; i >= start; i-- {
		// First line on its own measure, as in raggedDP.
		width := measure
		if i == 0 {
			width = first
		}
		bestCost := math.Inf(1)
		bestJ := i + 1
		bestHyph := -1
		lineLen := 0

		for j := i; j < n; j++ {
			wLen := words[j].width
			if j == i {
				lineLen = wLen
			} else {
				lineLen += sp + wLen
			}

			wordsOnLine := j - i + 1
			allow := (wordsOnLine - 1) * shrink

			if j == i && multiRune(words[j].text) {
				// A word wider than the measure -- a dash-final one
				// may hang its dash -- is split by the reconstruction
				// (word.split). Its cost is a hyphenated first
				// piece's if a point fits, the tail approximated by
				// cost[i+1], else the tail alone.
				alone := width
				if j+1 < n && strings.HasSuffix(words[j].text, "-") {
					alone += hang
				}
				if lineLen > alone {
					if hc, ok := tryHyphenAtJustify(words[j], -1, width, 1, cost[i+1], sp, hang); ok {
						bestCost, bestJ, bestHyph = hc.cost, i, overlong
					} else {
						bestCost, bestJ, bestHyph = cost[i+1], i, overlong
					}
					break
				}
			}
			if lineLen > width+hang+allow {
				if j == i {
					// A single rune wider than the measure overflows.
					bestCost, bestJ, bestHyph = cost[i+1], i+1, -1
					break
				}
				// Try hyphenating words[j] to fit.
				if len(words[j].prefix) > 0 {
					if hc, ok := tryHyphenAtJustify(words[j], lineLen-wLen-sp, width, wordsOnLine, cost[j], sp, hang); ok {
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
					c += gapCost(slack, wordsOnLine, sp)
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
			if j+1 < n && len(words[j].prefix) > 0 {
				spaceUsed := -1
				if j > i {
					spaceUsed = lineLen - wLen - sp
				}
				if hc, ok := tryHyphenAtJustify(words[j], spaceUsed, width, wordsOnLine, cost[j], sp, hang); ok {
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
// cost is not monotonic in prefix length. sp and hang are m's
// Space() and HangHyphen, hoisted by the caller.
func tryHyphenAtJustify(w word, spaceUsed, width, wordCount int, tailCost float64, sp, hang int) (hyphenChoice, bool) {
	shrink := shrinkPerGap(sp)
	// The prefix ends in "-", which hangs into the margin.
	target := width + hang
	best := hyphenChoice{}
	found := false
	for pi := len(w.prefix) - 1; pi >= 0; pi-- {
		partLen := w.prefix[pi]
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
		c := gapCost(slack, wordCount, sp) + hyphenPenaltyJustify + tailCost
		if !found || c < best.cost {
			best = hyphenChoice{cost: c, point: pi + 1}
			found = true
		}
	}
	return best, found
}

// Fields splits prose into words at breaking whitespace: the ASCII
// blanks and every Unicode space EXCEPT the no-break ones (U+00A0,
// U+2007 figure space, U+202F narrow no-break space), which an
// author writes precisely so that two tokens stay on one line.
func Fields(s string) []string {
	return strings.FieldsFunc(s, IsBreakingSpace)
}

// IsBreakingSpace is Fields' split rule as a predicate, for callers
// that tokenize styled text themselves, so every path breaks tokens
// identically.
func IsBreakingSpace(r rune) bool {
	switch r {
	case '\u00A0', '\u2007', '\u202F':
		return false
	}
	return unicode.IsSpace(r)
}
