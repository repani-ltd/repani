package wrap

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"repani.com/typeset/wrap/hyphen"
)

const testWidth = 40

// ragged is the monospace text of a prose paragraph.
func prose(para string, width int) []string {
	return Flatten(Hyphenated(para, width, width, hyphen.Default, PenaltyProse, Mono))
}

func TestHyphenateCompoundHyphen(t *testing.T) {
	// An explicit hyphen in a compound is a break point in its own
	// right: the break lands AFTER the hyphen and reuses it, never
	// doubling it ("four--") or stranding it at the head of the
	// next line ("-line").
	for _, tt := range []struct{ compound, prefix, suffix string }{
		{"four-line", "four-", "line"},
		{"well-known", "well-", "known"},
		{"self-evident", "self-", "evident"},
	} {
		points := hyphen.Default.Hyphenate(tt.compound)
		if len(points) == 0 {
			t.Errorf("Hyphenate(%q): no points, want the compound break", tt.compound)
			continue
		}
		runes := []rune(tt.compound)
		found := false
		for _, p := range points {
			if runes[p] == '-' {
				t.Errorf("Hyphenate(%q): point %d breaks before the hyphen", tt.compound, p)
			}
			prefix, suffix := hyphened(runes, p), string(runes[p:])
			if strings.Contains(prefix, "--") {
				t.Errorf("hyphened(%q, %d) doubled the hyphen: %q", tt.compound, p, prefix)
			}
			if prefix == tt.prefix && suffix == tt.suffix {
				found = true
			}
		}
		if !found {
			t.Errorf("Hyphenate(%q): no point gives %q + %q (points %v)",
				tt.compound, tt.prefix, tt.suffix, points)
		}
	}
}

func TestHyphenateDoubleHyphenBreaksAfterRun(t *testing.T) {
	// "abcd--efgh...": the only explicit break is after the second
	// hyphen; breaking between them would head a line with "-".
	w := "abcd--efghijklmnop"
	for _, p := range hyphen.Default.Hyphenate(w) {
		if []rune(w)[p] == '-' {
			t.Errorf("Hyphenate(%q): point %d strands a hyphen", w, p)
		}
	}
	// At 7 the break after the run fits. (At 5 nothing fits and the
	// word is cut: a cut is raw, and may head a line with "-".)
	lines := Hyphenated(w, 7, 7, hyphen.Default, PenaltyProse, Mono)
	if lines[0].Words[0] != "abcd--" {
		t.Errorf("first line %q, want the break after the run", lines[0].Words)
	}
	for _, l := range lines {
		if len(l.Words) > 0 && strings.HasPrefix(l.Words[0], "-") {
			t.Errorf("line starts with hyphen: %q", l.Words)
		}
	}
}

// --- Width utilities ---

func TestWidthPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("JustifyMono with width 0 did not panic")
		}
	}()
	JustifyMono("hello", 0, 0, hyphen.Default)
}

// --- Wrap ---

func TestRaggedFits(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow chasing butterflies."
	for _, ln := range prose(input, testWidth) {
		if len([]rune(ln)) > testWidth {
			t.Errorf("wrapped line exceeds width: %q", ln)
		}
	}
}

// --- Cells ---

func TestCell(t *testing.T) {
	for _, tc := range []struct {
		text  string
		width int
		want  []string
	}{
		{"", 5, []string{""}},
		{"Athens", 13, []string{"Athens"}},
		{"Paphos via Heraklion", 13, []string{"Paphos via", "Heraklion"}},
		{"aaaaaaaaaaaaaaaaaaaa", 8, []string{"aaaaaaaa", "aaaaaaaa", "aaaa"}}, // no points: cut
	} {
		got := Cell(tc.text, tc.width, hyphen.Default)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("Cell(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
		}
	}
	// Every line fits, whatever the text, hyphenated or not.
	for _, h := range []Hyphenator{hyphen.Default, nil} {
		for _, w := range []int{1, 3, 7, 12} {
			for _, ln := range Cell("Isolated thunderstorms developing inland internationalization", w, h) {
				if utf8.RuneCountInString(ln) > w {
					t.Errorf("Cell at width %d: %q overflows", w, ln)
				}
			}
		}
	}
}

func TestCellLongWord(t *testing.T) {
	// One unbreakable word of 50000 runes at width 3: 16667 pieces,
	// in linear time (the old cut was quadratic: seconds where this
	// takes milliseconds).
	got := Cell(strings.Repeat("λ", 50000), 3, hyphen.Default)
	if len(got) != 16667 || got[0] != "λλλ" || got[len(got)-1] != "λλ" {
		t.Errorf("%d pieces, first %q, last %q", len(got), got[0], got[len(got)-1])
	}
}

func TestLongHyphenatedWordIsLinear(t *testing.T) {
	// A word of 10400 letters with points throughout, at width 6:
	// every piece breaks at the word's own points, found once.
	// Re-hyphenating and re-measuring each remainder took 17 s.
	s := strings.Repeat("international", 800)
	start := time.Now()
	got := Cell(s, 6, hyphen.Default)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v", d)
	}
	var b strings.Builder
	for _, ln := range got {
		if utf8.RuneCountInString(ln) > 6 {
			t.Fatalf("%q overflows", ln)
		}
		b.WriteString(strings.TrimSuffix(ln, "-"))
	}
	if b.String() != s {
		t.Error("the pieces do not rejoin to the word")
	}
}

func TestRaggedPlain(t *testing.T) {
	// Without a hyphenator, lines end only between words, and a word
	// wider than the measure is cut with no hyphen added.
	got := Flatten(Ragged("Isolated thunderstorms developing inland", 10, Mono))
	want := []string{"Isolated", "thundersto", "rms", "developing", "inland"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
	// A cut remainder shares its line with the words after it.
	got = Flatten(Ragged("abcdefghijk lm no", 8, Mono))
	want = []string{"abcdefgh", "ijk lm", "no"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCutProportional(t *testing.T) {
	// Under a proportional measurer the cut is the longest prefix
	// that fits; a measure narrower than one rune still advances,
	// a rune a line.
	m := wideMeasurer{} // 10 units a rune
	for _, ln := range Ragged("abcdefghij", 35, m) {
		if ln.Width > 35 || ln.Width != m.Width(ln.Words[0]) {
			t.Errorf("line %q width %d", ln.Words, ln.Width)
		}
	}
	got := Flatten(Ragged("abc", 5, m))
	if strings.Join(got, "|") != "a|b|c" {
		t.Errorf("narrower than a rune: %q", got)
	}
}

// --- Justify gap cost ---

func TestJustifyGapCost(t *testing.T) {
	cases := []struct {
		slack, words int
		want         float64
	}{
		{0, 5, 0},    // perfect fit, no extra spaces
		{4, 5, 4},    // 4 gaps, base=1, extra=0: 4*1 = 4
		{6, 3, 18},   // 2 gaps, base=3, extra=0: 2*9 = 18
		{5, 3, 13},   // 2 gaps, base=2, extra=1: 1*9+1*4 = 13
		{0, 1, 0},    // single word, no slack
		{5, 1, 100},  // single word, heavy: 5*5*4
		{1, 2, 1},    // 1 gap, base=1: 1*1 = 1
		{10, 2, 100}, // 1 gap, base=10: 1*100 = 100
	}
	for _, c := range cases {
		got := monoGapCost(c.slack, c.words)
		if got != c.want {
			t.Errorf("monoGapCost(%d, %d) = %v, want %v",
				c.slack, c.words, got, c.want)
		}
	}
}

// --- Justified output properties ---

func TestJustify_FlushLines(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow chasing butterflies"
	lines := JustifyMono(input, testWidth, testWidth, hyphen.Default)
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}
	for i, ln := range lines[:len(lines)-1] {
		if utf8.RuneCountInString(ln) != testWidth {
			t.Errorf("line %d: %d runes, want %d: %q",
				i, utf8.RuneCountInString(ln), testWidth, ln)
		}
	}
	// Last line must not exceed width.
	last := lines[len(lines)-1]
	if utf8.RuneCountInString(last) > testWidth {
		t.Errorf("last line exceeds width: %q", last)
	}
}

func maxConsecutiveSpaces(s string) int {
	best, cur := 0, 0
	for _, r := range s {
		if r == ' ' {
			cur++
			if cur > best {
				best = cur
			}
		} else {
			cur = 0
		}
	}
	return best
}

func TestJustify_MaxGap(t *testing.T) {
	// With enough words, no justified gap should exceed 3 spaces.
	input := "The unprecedented international collaboration has fundamentally transformed the interconnected Mediterranean communities over the past several decades of cooperation"
	lines := JustifyMono(input, testWidth, testWidth, hyphen.Default)
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}
	for i, ln := range lines[:len(lines)-1] {
		gap := maxConsecutiveSpaces(ln)
		if gap > 3 {
			t.Errorf("line %d has %d-space gap (want <=3): %q",
				i, gap, ln)
		}
	}
}

func TestJustify_PrefersHyphenOverWideGaps(t *testing.T) {
	// Long words that would leave huge slack on a 40-col line
	// without hyphenation. The algorithm should hyphenate to keep
	// inter-word gaps narrow.
	input := "Transformation internationally recognized and comprehensive collaboration"
	lines := JustifyMono(input, testWidth, testWidth, hyphen.Default)
	for i, ln := range lines[:len(lines)-1] {
		gap := maxConsecutiveSpaces(ln)
		if gap > 3 {
			t.Errorf("line %d: gap=%d, expected hyphenation to reduce it: %q",
				i, gap, ln)
		}
	}
}

func TestGaps(t *testing.T) {
	for _, c := range []struct {
		line  string
		width int
		last  bool
		want  []int
	}{
		{"aa bb cc", 14, false, []int{4, 4}},       // slack 6 over 2 gaps, on their 1 each
		{"aa bb cc dd", 15, false, []int{3, 2, 2}}, // slack 4: the leftmost gap takes the odd one
		{"aa bb cc", 14, true, []int{1, 1}},        // the last line sets natural
		{"ab cd ef", 8, false, []int{1, 1}},        // already at its measure
		{"hello", 10, false, nil},                  // no gaps
	} {
		if got := Gaps(LineOf(strings.Fields(c.line), Mono), c.width, Mono, c.last); !slices.Equal(got, c.want) {
			t.Errorf("Gaps(%q, %d, %v) = %v, want %v", c.line, c.width, c.last, got, c.want)
		}
	}
	// A proportional line within the shrink allowance compresses; a
	// line ending in "-" counts the hyphen's hang.
	m := wideMeasurer{}                                                                                    // 10 a rune, 9 a space
	if got := Gaps(LineOf([]string{"aa", "bb", "cc"}, m), 74, m, false); !slices.Equal(got, []int{7, 7}) { // 78 wide: 4 less
		t.Errorf("shrink: %v", got)
	}
	if got := Gaps(LineOf([]string{"aa", "bb-"}, m), 59, m, false); !slices.Equal(got, []int{16}) {
		t.Errorf("hang: %v", got)
	}
}

// --- Measured (proportional) wrapping ---

// fakeMeasurer caricatures a proportional font in tenth-of-character
// units: narrow i/l/t, wide m/w, everything else 10.
type fakeMeasurer struct{}

func (fakeMeasurer) Width(s string) int {
	total := 0
	for _, r := range s {
		switch r {
		case 'i', 'l', 'j', 't', 'f':
			total += 4
		case 'm', 'w', 'M', 'W':
			total += 15
		default:
			total += 10
		}
	}
	return total
}
func (fakeMeasurer) Space() int { return 5 }

func TestRagged_MeasuredFits(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow chasing illuminated butterflies"
	m := fakeMeasurer{}
	lines := Hyphenated(input, 300, 300, hyphen.Default, PenaltyProse, m)
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}
	for i, ln := range lines {
		if len(ln.Words) == 0 {
			t.Errorf("line %d has no words", i)
		}
		if ln.Width > 300 {
			t.Errorf("line %d: width %d exceeds 300: %v", i, ln.Width, ln.Words)
		}
		// Width must equal the re-measured natural width.
		w := 0
		for j, wd := range ln.Words {
			if j > 0 {
				w += m.Space()
			}
			w += m.Width(wd)
		}
		if w != ln.Width {
			t.Errorf("line %d: Width %d, re-measured %d", i, ln.Width, w)
		}
	}
}

func TestJustify_MeasuredSlack(t *testing.T) {
	input := "The unprecedented international collaboration has fundamentally transformed the interconnected communities over several decades"
	m := fakeMeasurer{}
	width := 300
	lines := Justify(Tokens(input, m), width, width, hyphen.Default, m)
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}
	for i, ln := range lines[:len(lines)-1] {
		// A justified line may exceed width only by the shrink
		// allowance (a third of a space per gap) plus, when it ends
		// in a hyphen, the hang protrusion.
		allow := (len(ln.Words) - 1) * (m.Space() / 3)
		if strings.HasSuffix(ln.Words[len(ln.Words)-1], "-") {
			allow += hyphenHang(m)
		}
		if ln.Width > width+allow {
			t.Errorf("line %d overfull: %d > %d+%d", i, ln.Width, width, allow)
		}
		// Sanity bound only: the caricature widths make tight gap
		// bounds meaningless, but distributed slack should never
		// approach pathological (many-space) gaps.
		if gaps := len(ln.Words) - 1; gaps > 0 {
			perGap := float64(width-ln.Width) / float64(gaps)
			if perGap > 8*float64(m.Space()) {
				t.Errorf("line %d: per-gap slack %.1f exceeds 8 spaces: %v",
					i, perGap, ln.Words)
			}
		}
	}
}

// wideMeasurer is a proportional caricature with a wide space, so
// the shrink allowance (space/3) is meaningful in integer units.
type wideMeasurer struct{}

func (wideMeasurer) Width(s string) int { return 10 * utf8.RuneCountInString(s) }
func (wideMeasurer) Space() int         { return 9 }

func TestJustify_ShrinkAbsorbsWord(t *testing.T) {
	// Seven words of 40 units, gaps of 9. Five words measure 236:
	// at width 230 they overflow by 6, within the shrink allowance
	// 4*(9/3) = 12, costing far less than the loose four-word
	// alternative. The optimum is a shrunk five-word first line and
	// a natural two-word last line.
	m := wideMeasurer{}
	lines := Justify(Tokens("aaaa bbbb cccc dddd eeee ffff gggg", m), 230, 230, hyphen.Default, m)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if got := len(lines[0].Words); got != 5 {
		t.Fatalf("first line has %d words, want 5 (shrink should absorb the fifth): %v",
			got, lines[0].Words)
	}
	if lines[0].Width != 236 {
		t.Errorf("first line width = %d, want 236", lines[0].Width)
	}
	// The last line always renders at natural spacing and must
	// never rely on shrink.
	if last := lines[1]; last.Width > 230 {
		t.Errorf("last line overfull: %d > 230: %v", last.Width, last.Words)
	}
}

func TestHyphenHang(t *testing.T) {
	// Monospace: a cell cannot protrude fractionally.
	if got := hyphenHang(Mono); got != 0 {
		t.Errorf("hyphenHang(Mono) = %d, want 0", got)
	}
	// wideMeasurer: hyphen is 10 units, 70% hangs.
	if got := hyphenHang(wideMeasurer{}); got != 7 {
		t.Errorf("hyphenHang(wideMeasurer) = %d, want 7", got)
	}
}

func TestTryHyphenAtJustify_HangExtendsTarget(t *testing.T) {
	// wideMeasurer: runes 10, space 9, shrink 9/3=3, hang 7. The
	// prefix "abc-" is 40 wide; with 60 used the line totals
	// 60+9+40 = 109. For a two-word line the plain window is
	// width+shrink = 108, so only the hang (target 112, window 115)
	// admits the break.
	m := wideMeasurer{}
	w := word{text: "abcdef", width: 60, points: []int{3}, prefix: []int{40}}
	if _, _, ok := justified(m).hyphen(w, 60, 105, 2, 0); !ok {
		t.Errorf("hyphen break rejected at width 105: hang should extend the target")
	}
	if _, _, ok := justified(m).hyphen(w, 60, 97, 2, 0); ok {
		t.Errorf("hyphen break accepted at width 97: outside hang+shrink window")
	}
}

func TestJustify_MonoNeverShrinks(t *testing.T) {
	// The monospace measurer has no sub-character shrink: every
	// line must fit within width at natural spacing.
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow"
	for _, ln := range Justify(Tokens(input, Mono), testWidth, testWidth, hyphen.Default, Mono) {
		if ln.Width > testWidth {
			t.Errorf("mono line overfull: %d > %d: %v", ln.Width, testWidth, ln.Words)
		}
	}
}

func TestJustify_MonoMatchesJustifyMono(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow"
	lines := Justify(Tokens(input, Mono), testWidth, testWidth, hyphen.Default, Mono)
	flat := Flatten(lines)
	want := JustifyMono(input, testWidth, testWidth, hyphen.Default)
	if len(flat) != len(want) {
		t.Fatalf("line count %d != %d", len(flat), len(want))
	}
	// Same breaks: collapsing justified spacing must recover the
	// natural-spaced lines.
	for i := range want {
		collapsed := strings.Join(strings.Fields(want[i]), " ")
		if collapsed != flat[i] {
			t.Errorf("line %d: %q != %q", i, collapsed, flat[i])
		}
	}
}

func TestJustify_TokensMatchTokens(t *testing.T) {
	// Unstyled tokens on the body measurer break exactly as the
	// paragraph does, with no Emph flags.
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow"
	var toks []Token
	for _, f := range Fields(input) {
		toks = append(toks, Token{Text: f, M: fakeMeasurer{}})
	}
	got := Justify(toks, 300, 300, hyphen.Default, fakeMeasurer{})
	want := Justify(Tokens(input, fakeMeasurer{}), 300, 300, hyphen.Default, fakeMeasurer{})
	if len(got) != len(want) {
		t.Fatalf("line count %d != %d", len(got), len(want))
	}
	for i := range want {
		if strings.Join(got[i].Words, " ") != strings.Join(want[i].Words, " ") || got[i].Emph != nil {
			t.Errorf("line %d: %v (emph %v), want %v", i, got[i].Words, got[i].Emph, want[i].Words)
		}
	}
}

func TestRaggedVsJustify_Differ(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow"
	r := strings.Join(prose(input, testWidth), "\n")
	justified := strings.Join(JustifyMono(input, testWidth, testWidth, hyphen.Default), "\n")
	if r == justified {
		t.Error("ragged and justified output should differ")
	}
}

func TestOverlongWordHyphenates(t *testing.T) {
	// A word wider than the measure must set a hyphenated prefix
	// instead of overflowing, in both breakers.
	const word = "internationalization" // 20 runes
	const width = 12
	for name, lines := range map[string][]Line{
		"ragged":  Hyphenated(word, width, width, hyphen.Default, PenaltyProse, Mono),
		"justify": Justify(Tokens(word, Mono), width, width, hyphen.Default, Mono),
	} {
		if len(lines) < 2 {
			t.Errorf("%s: %q at width %d stayed on one line", name, word, width)
			continue
		}
		for i, ln := range lines {
			if ln.Width > width {
				t.Errorf("%s: line %d overflows: %v (width %d > %d)", name, i, ln.Words, ln.Width, width)
			}
		}
		last := lines[0].Words[len(lines[0].Words)-1]
		if !strings.HasSuffix(last, "-") {
			t.Errorf("%s: first line does not end in a hyphen: %v", name, lines[0].Words)
		}
	}
}

func TestOverlongWordWithoutPointsIsCut(t *testing.T) {
	// No hyphenation point fits: the word is cut at the measure, in
	// both breakers, with no hyphen added.
	const word = "aaaaaaaaaaaaaaaaaaaa" // no valid Liang points
	for name, lines := range map[string][]Line{
		"ragged":  Hyphenated(word, 12, 12, hyphen.Default, PenaltyProse, Mono),
		"justify": Justify(Tokens(word, Mono), 12, 12, hyphen.Default, Mono),
	} {
		if got := strings.Join(Flatten(lines), "|"); got != "aaaaaaaaaaaa|aaaaaaaa" {
			t.Errorf("%s: %q", name, got)
		}
	}
}

func TestJustifyKeepsAFittingFirstWord(t *testing.T) {
	// "responsibility" fits 17: the justified breaker once split it
	// anyway, costing its suffix at a cost not yet computed.
	p := "responsibility character typesetting hyphenation he government is my me justification by"
	if got := JustifyMono(p, 17, 17, hyphen.Default)[0]; strings.TrimSpace(got) != "responsibility" {
		t.Errorf("first line %q", got)
	}
}

// wideW is a proportional face with one wide glyph: W is 1000 units,
// every other rune 100, the space 250, so the hyphen hangs 70.
type wideW struct{}

func (wideW) Width(s string) int {
	n := 0
	for _, r := range s {
		if r == 'W' {
			n += 1000
		} else {
			n += 100
		}
	}
	return n
}
func (wideW) Space() int { return 250 }

func TestJustifySingleRuneInTheHang(t *testing.T) {
	// W is wider than 950 but within the hang: it sets alone, and the
	// lines before it keep their words.
	got := Flatten(Justify(Tokens("aa bb W cc dd", wideW{}), 950, 950, nil, wideW{}))
	if strings.Join(got, "|") != "aa bb|W|cc dd" {
		t.Errorf("got %q", got)
	}
}

func TestSuffixKeepsTheWordsPoints(t *testing.T) {
	// A word is hyphenated once: after a break, its suffix breaks at
	// the word's own points, shifted, not at a fresh hyphenation of
	// the suffix -- and never two runes or fewer from its start.
	w := newWord("responsibility", Mono, hyphen.Default, 40)
	s := w.from(w.points[0])
	var want []int
	for _, p := range w.points[1:] {
		if p-w.points[0] >= 2 {
			want = append(want, p-w.points[0])
		}
	}
	if !slices.Equal(s.points, want) || s.text != string([]rune(w.text)[w.points[0]:]) {
		t.Errorf("suffix %q points %v, want %v (word points %v)", s.text, s.points, want, w.points)
	}
	if s.width != 14-w.points[0] || len(s.prefix) != len(s.points) {
		t.Errorf("suffix measured %d wide, %d prefixes", s.width, len(s.prefix))
	}
}

func TestSingleRuneOverflows(t *testing.T) {
	// One rune wider than the measure cannot be broken: it sets
	// alone, and the words around it keep their lines.
	got := Flatten(Ragged("ab W cd", 2, wideMeasurer{}))
	if strings.Join(got, "|") != "a|b|W|c|d" {
		t.Errorf("got %q", got)
	}
}
