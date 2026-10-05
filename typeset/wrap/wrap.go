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

func (monoMeasurer) Width(s string) int { return utf8.RuneCountInString(s) }
func (monoMeasurer) Space() int         { return 1 }

// Mono measures text for monospace surfaces: every rune, including
// the interword space, is one unit wide.
var Mono Measurer = monoMeasurer{}

// Line is one wrapped line: its words in order plus the natural
// width (word widths and one space per gap) in measurer units.
// Renderers join words with single spaces (monospace) or spread the
// line's slack across the gaps (Gaps). A hyphenated break leaves
// the "-" on the line's last word. Emph, set only when a Token is
// emphasized, is parallel to Words: true for words the renderer
// sets in the emphasis face. It is nil everywhere else.
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

// Token is one word measured in its own face: M measures it, and
// Emph marks it for the renderer's emphasis face (Line.Emph). A
// caller that styles words -- pica's _emphasis_ -- tokenizes them
// itself, at breaking spaces as Fields does.
type Token struct {
	Text string
	M    Measurer
	Emph bool
}

// Tokens splits a paragraph into words (Fields), every one measured
// by m: the tokens of unstyled text.
func Tokens(para string, m Measurer) []Token {
	fs := Fields(para)
	toks := make([]Token, len(fs))
	for i, f := range fs {
		toks[i] = Token{Text: f, M: m}
	}
	return toks
}

// checkWidth guards the layout entry points: a non-positive width is
// a programmer error, not an input condition. So is a first line
// wider than the rest: a run-in lead only takes room from it, and the
// breaker relies on that when it sets a split word's remainder on
// the first line's measure.
func checkWidth(first, width int) {
	if first <= 0 || width <= 0 || first > width {
		panic(fmt.Sprintf("wrap: measures must be positive, the first no wider than the rest: got %d and %d", first, width))
	}
}

// PenaltyProse is the cost of a hyphen break in ragged paragraphs:
// high, because at prose widths a hyphen is rarely worth it. Narrow
// surfaces (table cells) use PenaltyCell, where the alternative --
// one word per line -- costs far more vertically.
const (
	PenaltyProse = 100
	PenaltyCell  = 25
)

// Ragged wraps ONE paragraph ragged-right under the measurer without
// hyphenation: the optimal breaks between words, and a word wider
// than the measure cut. It is the light path, for programs that
// import no patterns. Proportional writers consume the Lines
// directly; Flatten gives monospace text.
func Ragged(para string, width int, m Measurer) []Line {
	checkWidth(width, width)
	return ragged(m, 0).breakLines(words(Tokens(para, m), nil, width, width), width, width)
}

// Hyphenated wraps ONE paragraph ragged-right, ending a line inside
// a word at one of h's points where that is better, each hyphen
// costing penalty: PenaltyProse for paragraphs, PenaltyCell for
// table cells. The first line sets on the measure first and every
// later one on width -- equal but for a paragraph whose first line
// opens with a run-in lead (a label) that is not part of it: first
// is what the lead leaves of the line, the lead the caller's to
// place. Both measures must be positive.
func Hyphenated(para string, first, width int, h Hyphenator, penalty float64, m Measurer) []Line {
	checkWidth(first, width)
	return ragged(m, penalty).breakLines(words(Tokens(para, m), h, first, width), first, width)
}

// Cell wraps a table cell's text to width runes in monospace,
// hyphenating at h's points (nil: none) with the cell-tuned penalty:
// in a narrow column "Isolated thunder-" / "storms inland" beats one
// word per line. Every line fits; empty text is one empty line.
func Cell(s string, width int, h Hyphenator) []string {
	out := Flatten(Hyphenated(s, width, width, h, PenaltyCell, Mono))
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// Justify chooses justified line breaks for tokens -- each measured
// in its own face, so justification stays exact when the renderer
// switches faces -- hyphenating at h's points (nil: none). The first
// line sets on first, every later line on width, as for Hyphenated.
// It returns lines at natural spacing, with the parallel Emph flags
// when any token is emphasized: the caller spreads each non-final
// line's slack across its gaps (Gaps). With a proportional measurer
// the slack may be negative -- a line may exceed its measure by up
// to a third of a space per gap -- and the gaps compress by that
// amount. Interword spaces, and the hyphen's hang, are the body
// measurer m's.
func Justify(toks []Token, first, width int, h Hyphenator, m Measurer) []Line {
	checkWidth(first, width)
	return justified(m).breakLines(words(toks, h, first, width), first, width)
}

// JustifyMono justifies ONE paragraph in monospace -- Justify under
// Mono -- and flushes every line but the last to its measure with
// whole spaces (Gaps): the first line to first, the rest to width.
func JustifyMono(para string, first, width int, h Hyphenator) []string {
	lines := Justify(Tokens(para, Mono), first, width, h, Mono)
	out := make([]string, len(lines))
	for i, ln := range lines {
		w := width
		if i == 0 {
			w = first
		}
		var b strings.Builder
		for k, g := range Gaps(ln, w, Mono, i == len(lines)-1) {
			b.WriteString(ln.Words[k])
			b.WriteString(strings.Repeat(" ", g))
		}
		b.WriteString(ln.Words[len(ln.Words)-1])
		out[i] = b.String()
	}
	return out
}

// Gaps returns the advances between a line's words set to width
// under m: natural spaces on a paragraph's last line (last) and on a
// line already at its measure; otherwise the slack spread evenly,
// leftmost gaps taking the remainder, so the line fills the measure
// exactly -- in integers, keeping a renderer deterministic. Negative
// slack (a proportional line within the breaker's shrink allowance)
// compresses the gaps the same way. A justified line ending in "-"
// targets width plus the hyphen's hang, as the breaker does, so the
// hyphen protrudes into the margin and the flush edge stays
// optically straight.
func Gaps(ln Line, width int, m Measurer, last bool) []int {
	k := len(ln.Words) - 1
	if k <= 0 {
		return nil
	}
	gaps := make([]int, k)
	sp := m.Space()
	for i := range gaps {
		gaps[i] = sp
	}
	slack := width - ln.Width
	if !last && strings.HasSuffix(ln.Words[k], "-") {
		slack += hyphenHang(m)
	}
	if last || slack == 0 {
		return gaps
	}
	sign := 1
	if slack < 0 {
		sign, slack = -1, -slack
	}
	base, extra := slack/k, slack%k
	for i := range gaps {
		d := base
		if i < extra {
			d++
		}
		gaps[i] += sign * d
	}
	return gaps
}

// hyphenHang is the width a line-final hyphen protrudes into the
// right margin (optical margin alignment): 70% of the hyphen's
// advance under the measurer. The justified breaker extends a line's
// measure by it for a line ending in "-", and Gaps does too, so the
// flush edge runs through the hyphen instead of jogging left of it.
// Integer math disables it for the monospace character grid, which
// cannot protrude a fraction of a cell.
func hyphenHang(m Measurer) int { return m.Width("-") * 7 / 10 }

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
	case ' ', ' ', ' ':
		return false
	}
	return unicode.IsSpace(r)
}

// word holds a token, its measured width, and its hyphenation
// breakpoints (rune indices into text) with the measured width of
// each hyphenated prefix (trailing "-" included), so the DP probes
// never re-measure. Prefixes are measured in order only while they
// stay within the word's limit, the widest a line can take, so
// prefix may be shorter than points: a point past it never fits. m
// is the measurer that owns the token's face (a substituted suffix
// is measured with it); emph marks tokens set in the emphasis face,
// carried into Line.Emph.
type word struct {
	text   string
	width  int
	points []int
	prefix []int // the first len(prefix) of points
	m      Measurer
	limit  int
	emph   bool
}

// words measures tokens for a paragraph whose lines set on first and
// width, hyphenating at h's points.
func words(toks []Token, h Hyphenator, first, width int) []word {
	// The widest prefix worth measuring: twice the wider measure,
	// beyond any hyphen hang and justified shrink.
	limit := 2 * max(first, width)
	ws := make([]word, len(toks))
	for i, t := range toks {
		ws[i] = newWord(t.Text, t.M, h, limit)
		ws[i].emph = t.Emph
	}
	return ws
}

// newWord measures one word under m, with its hyphenation points
// from h (none when nil) and the widths the breakers compare.
func newWord(text string, m Measurer, h Hyphenator, limit int) word {
	w := word{text: text, m: m, limit: limit}
	if h != nil {
		w.points = h.Hyphenate(text)
	}
	w.measure()
	return w
}

// from is w's suffix from rune cut on, set on a line of its own after
// a break inside w: the same word, hyphenated once as TeX does -- its
// points past cut, shifted, but none that would leave fewer than two
// runes before a hyphen, as Hyphenate leaves none at a word's start.
func (w word) from(cut int) word {
	r := []rune(w.text)
	s := word{text: string(r[cut:]), m: w.m, limit: w.limit, emph: w.emph}
	for _, p := range w.points {
		if p-cut >= 2 {
			s.points = append(s.points, p-cut)
		}
	}
	s.measure()
	return s
}

// measure sets w's width and the widths of its hyphenated prefixes.
func (w *word) measure() {
	w.width, w.prefix = w.m.Width(w.text), nil
	if len(w.points) == 0 {
		return
	}
	r := []rune(w.text)
	for _, p := range w.points {
		pw := w.m.Width(hyphened(r, p))
		if pw > w.limit {
			break // prefixes only grow: no later one fits a line
		}
		w.prefix = append(w.prefix, pw)
	}
}

// hyphened is the line end a break at rune cut of r sets: the prefix
// and the hyphen the break adds -- none after an explicit hyphen
// ("four-line"), which the break reuses instead of doubling.
func hyphened(r []rune, cut int) string {
	if r[cut-1] == '-' {
		return string(r[:cut])
	}
	return string(r[:cut]) + "-"
}

// split breaks w, wider than its line, into the pieces that each fill
// a line, the first on lw and the rest on width, until what is left
// fits or is one rune: each piece ends at the rightmost hyphenation
// point whose prefix, hyphen included, fits the line and its hang,
// or failing one is cut at the longest prefix that fits, one rune at
// the least. It returns the pieces and the rune where what is left
// begins. The work is linear in the pieces' length, however long w
// is: nothing measures what is left until it may fit.
func (w word) split(lw, width, hang int) (pieces []string, rest int) {
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
			if p := hyphened(r[o:], w.points[k]-o); w.m.Width(p) <= lw+hang {
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
	return pieces, o
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

// model is a breaker's cost model; the one dynamic program and its
// reconstruction run under it. sp is the interword space, hang a
// line-final hyphen's protrusion and shrink the most a justified gap
// may compress, in measurer units. penalty is a hyphen's cost and
// final the extra cost of one whose suffix starts the paragraph's
// last line. proactive tries a hyphen even in a word that fits, to
// tighten a justified line. line costs a line that is not the
// paragraph's last, at its slack and word count; last costs the last
// line, which sets at natural spacing.
type model struct {
	sp, hang, shrink int
	penalty, final   float64
	proactive        bool
	line             func(slack, words int) float64
	last             func(slack, width int) float64
}

// ragged is the ragged-right model: a line costs its slack squared,
// and the last line only when it is shorter than half the measure.
func ragged(m Measurer, penalty float64) *model {
	sp := m.Space()
	spsp := float64(sp) * float64(sp)
	return &model{
		sp:      sp,
		penalty: penalty,
		line:    func(slack, _ int) float64 { s := float64(slack); return s * s / spsp },
		last: func(slack, width int) float64 {
			if slack > width/2 {
				s := float64(slack)
				return s * s / 4 / spsp
			}
			return 0
		},
	}
}

// justified is the justified model: a line costs the widening (or
// shrinking) of its gaps (gapCost), a hyphen costs little, since it
// adds a gap to spread slack over, and hyphens are tried even in
// words that fit; the last line costs only when it is an orphan
// shorter than five characters.
func justified(m Measurer) *model {
	sp := m.Space()
	spsp := float64(sp) * float64(sp)
	return &model{
		sp:        sp,
		hang:      hyphenHang(m),
		shrink:    sp / 3,
		penalty:   hyphenPenaltyJustify,
		final:     finalHyphenPenalty,
		proactive: true,
		line:      func(slack, words int) float64 { return gapCost(slack, words, sp) },
		last: func(slack, width int) float64 {
			if slack > width-5*sp {
				s := float64(slack)
				return s * s / 4 / spsp
			}
			return 0
		},
	}
}

// hyphenPenaltyJustify is the cost of a hyphen break in justified
// mode. Much smaller than the ragged-right penalties (PenaltyProse,
// PenaltyCell) because justification spreads slack across the
// inter-word gaps -- hyphenation creates an additional gap,
// spreading slack more evenly and reducing the maximum gap width.
const hyphenPenaltyJustify = 6

// finalHyphenPenalty is the extra cost of a hyphen whose suffix
// begins the paragraph's last line (TeX's \finalhyphendemerits):
// the paragraph then trails off in a bare word fragment. Steep but
// not prohibitive -- a fragment still beats a grotesquely loose
// line.
const finalHyphenPenalty = 40

// gapCost is the justify cost of distributing slack over a line of
// words tokens under a measurer whose space width is sp. The
// monospace measurer models whole extra spaces (monoGapCost);
// proportional measurers spread slack continuously, so the cost is
// the squared per-gap widening in space-width units. Negative slack
// means the gaps compress below natural (never past the shrink
// allowance, which the DP enforces); normalizing shrink by the
// allowance rather than the space width mirrors TeX's badness,
// making a full shrink cost as much as a three-space stretch.
func gapCost(slack, words int, sp int) float64 {
	if sp == 1 {
		return monoGapCost(slack, words)
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

// monoGapCost is the visual cost of distributing slack extra spaces
// across a monospace justified line of words tokens. Each gap already
// has one natural space; the slack spaces go as evenly as whole
// spaces can (some gaps floor(slack/gaps) extra, the rest ceil), and
// the cost is the sum of the squared extras, which penalises lines
// where some gaps are much wider than others. A single-token line
// cannot be justified at all, its slack trailing white space: a
// heavy penalty.
func monoGapCost(slack, words int) float64 {
	if words <= 1 {
		return float64(slack * slack * 4)
	}
	gaps := words - 1
	base := slack / gaps
	extra := slack % gaps
	return float64(extra*(base+1)*(base+1) + (gaps-extra)*base*base)
}

// The marks in a DP position's hyph entry besides a point index.
const (
	noHyphen = -1 // the line ends after a whole word
	overlong = -2 // the line starts with a word wider than it
)

// breakLines runs the DP over pre-measured words and reconstructs
// the chosen lines; the first line sets on first, every later line
// on width. When a hyphen substitutes a suffix, the break at that
// position is recomputed so the next line accounts for the shorter
// token instead of the stale DP entry for the full word; an overlong
// word is split, its last piece taking the word's place the same way.
func (md *model) breakLines(words []word, first, width int) []Line {
	n := len(words)
	if n == 0 {
		return nil
	}
	styled := false
	for _, w := range words {
		styled = styled || w.emph
	}
	cost := make([]float64, n+1)
	next := make([]int, n)
	hyph := make([]int, n)
	md.dp(words, 0, n, first, width, cost, next, hyph)
	// rest puts the suffix of words[k] from rune cut in its place and
	// recomputes the break at k.
	rest := func(k, cut int) {
		words[k] = words[k].from(cut)
		md.dp(words, k, k+1, first, width, cost, next, hyph)
	}

	var lines []Line
	for i := 0; i < n; {
		j, hp := next[i], hyph[i]
		if hp == overlong {
			// words[i] alone is wider than its line. Each piece that
			// fills a line becomes one; what is left fits and takes
			// the word's place. (Under a run-in, a remainder at word
			// 0 is set on the first line's measure again: narrower,
			// still fits.)
			lw := width
			if len(lines) == 0 {
				lw = first
			}
			w := words[i]
			pieces, cut := w.split(lw, width, md.hang)
			for _, p := range pieces {
				ln := Line{Words: []string{p}, Width: w.m.Width(p)}
				if styled {
					ln.Emph = []bool{w.emph}
				}
				lines = append(lines, ln)
			}
			rest(i, cut)
			continue
		}
		ln := Line{Words: make([]string, 0, j-i+1)}
		if styled {
			ln.Emph = make([]bool, 0, j-i+1)
		}
		natural := 0
		add := func(text string, width int, emph bool) {
			ln.Words = append(ln.Words, text)
			if styled {
				ln.Emph = append(ln.Emph, emph)
			}
			natural += width + md.sp
		}
		for k := i; k < j; k++ {
			add(words[k].text, words[k].width, words[k].emph)
		}
		if hp >= 0 {
			// The line ends inside words[j], at point hp: set its
			// prefix, and the suffix takes the word's place.
			w := words[j]
			add(hyphened([]rune(w.text), w.points[hp]), w.prefix[hp], w.emph)
			rest(j, w.points[hp])
		}
		ln.Width = natural - md.sp
		lines = append(lines, ln)
		i = j
	}
	return lines
}

// dp runs the backward dynamic-programming pass for positions
// [start, end) under the model, filling cost, next and hyph: the
// cost of setting words from each position on, where its line ends
// (the position after its last whole word, or of the word it
// hyphenates), and the point it hyphenates at (or noHyphen, or
// overlong). Positions >= end keep their entries, which is what lets
// the reconstruction recompute a single position after a
// substitution.
func (md *model) dp(words []word, start, end, first, measure int, cost []float64, next, hyph []int) {
	n := len(words)
	for i := end - 1; i >= start; i-- {
		// The paragraph's first line (the one starting at word 0)
		// sets on its own measure: a run-in lead may occupy part of
		// it; every other line sets on measure.
		width := measure
		if i == 0 {
			width = first
		}
		bestCost, bestJ, bestHyph := math.Inf(1), i+1, noHyphen
		take := func(c float64, j, h int) {
			if c < bestCost {
				bestCost, bestJ, bestHyph = c, j, h
			}
		}
		lineLen := 0
		for j := i; j < n; j++ {
			w := words[j]
			if j == i {
				lineLen = w.width
			} else {
				lineLen += md.sp + w.width
			}
			wordsOnLine := j - i + 1
			allow := (wordsOnLine - 1) * md.shrink
			// The measure a line ending here fills: plus the hang when
			// it ends in a dash and does not end the paragraph.
			target := width
			if j+1 < n && strings.HasSuffix(w.text, "-") {
				target += md.hang
			}
			if j == i && lineLen > width && !multiRune(w.text) {
				// A single rune wider than the measure sets alone and
				// overflows: nothing can break it.
				bestCost, bestJ, bestHyph = cost[i+1], i+1, noHyphen
				break
			}
			if j == i && lineLen > target {
				// A word wider than its line is split by the
				// reconstruction (word.split). Its cost is a hyphenated
				// first piece's if a point fits, the tail approximated
				// by cost[i+1], else the tail alone.
				bestCost, bestJ, bestHyph = cost[i+1], i, overlong
				if c, _, ok := md.hyphen(w, -1, width, 1, cost[i+1]); ok {
					bestCost = c
				}
				break
			}
			if lineLen > width+md.hang+allow {
				// The line overflows in words[j]: end it at one of the
				// word's points, if one fits.
				if c, p, ok := md.hyphen(w, lineLen-w.width-md.sp, width, wordsOnLine, cost[j]); ok {
					if next[j] == n {
						c += md.final
					}
					take(c, j, p)
				}
				break
			}
			// The line ends after words[j], stretched or (proportional
			// measures, justified) shrunk within the allowance; the
			// last line sets at natural spacing and may not shrink.
			if slack := target - lineLen; slack >= -allow && (j+1 < n || slack >= 0) {
				if j+1 < n {
					take(cost[j+1]+md.line(slack, wordsOnLine), j+1, noHyphen)
				} else {
					take(cost[j+1]+md.last(slack, width), j+1, noHyphen)
				}
			}
			// Proactive hyphenation: end the line inside words[j] even
			// though it fits, to tighten a justified line -- but not
			// on the last line (not justified), nor in a line's first
			// word, whose suffix would start the same line again at a
			// cost, cost[i], this pass has not computed yet.
			if md.proactive && j+1 < n && j > i {
				if c, p, ok := md.hyphen(w, lineLen-w.width-md.sp, width, wordsOnLine, cost[j]); ok {
					if next[j] == n {
						c += md.final
					}
					take(c, j, p)
				}
			}
		}
		cost[i], next[i], hyph[i] = bestCost, bestJ, bestHyph
	}
}

// hyphen finds the cheapest of w's points to end a line at: used is
// the line's width before w (its gap excluded), or -1 when w starts
// the line, and words the line's word count with w. The prefix ends
// in "-", which hangs; the line may shrink by its allowance. It
// returns the cost -- the line's, the penalty and tail, the cost of
// what follows -- and the point's index, or ok false when no prefix
// fits. Under the ragged model the cheapest is the rightmost that
// fits; under the justified one the gap cost need not fall as the
// prefix grows, so every point is tried.
func (md *model) hyphen(w word, used, width, words int, tail float64) (cost float64, point int, ok bool) {
	target := width + md.hang
	for pi := len(w.prefix) - 1; pi >= 0; pi-- {
		total := w.prefix[pi]
		if used >= 0 {
			total += used + md.sp
		}
		if total > target+(words-1)*md.shrink {
			continue
		}
		c := md.line(target-total, words) + md.penalty + tail
		if !ok || c < cost {
			cost, point, ok = c, pi, true
		}
	}
	return cost, point, ok
}
