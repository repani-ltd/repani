package typeset

import (
	"strings"
	"testing"
	"unicode"
)

const testWidth = 40

// --- Hyphenation ---

func TestHyphenateEnglish(t *testing.T) {
	tests := []struct {
		word   string
		expect bool
	}{
		{"hyphenation", true},
		{"thunderstorm", true},
		{"temperature", true},
		{"international", true},
		{"cat", false},
		{"the", false},
		{"wind", false},
	}

	for _, tt := range tests {
		points := defaultHyphenator.Hyphenate(tt.word)
		got := len(points) > 0
		if got != tt.expect {
			t.Errorf("Hyphenate(%q): got points=%v, want hasPoints=%v", tt.word, points, tt.expect)
		}
	}
}

func TestHyphenateGreek(t *testing.T) {
	tests := []struct {
		word   string
		expect bool
	}{
		{"φαρμακείο", true},
		{"θερμοκρασία", true},
		{"πληροφορίες", true},
		{"ναι", false},
	}

	for _, tt := range tests {
		points := defaultHyphenator.Hyphenate(tt.word)
		got := len(points) > 0
		if got != tt.expect {
			t.Errorf("Hyphenate(%q): got points=%v, want hasPoints=%v", tt.word, points, tt.expect)
		}
	}
}

func TestHyphenateAttachedPunctuation(t *testing.T) {
	// Punctuation attached to a token must not count as letters:
	// "judgment." once broke as "judgmen-" / "t.", stranding a
	// single letter, and edge punctuation shifted the pattern
	// word boundaries. Points are indices into the full token,
	// with >= 2 letters on each side of every break.
	for _, word := range []string{"judgment.", "(judgment", "judgment,»", "μέρα.", "philosophy;"} {
		runes := []rune(word)
		core := strings.TrimFunc(word, func(r rune) bool { return !unicode.IsLetter(r) })
		bare := defaultHyphenator.Hyphenate(core)
		start := strings.Index(word, core)
		startRunes := len([]rune(word[:max(start, 0)]))
		for _, p := range defaultHyphenator.Hyphenate(word) {
			letters := 0
			for _, r := range runes[p:] {
				if unicode.IsLetter(r) {
					letters++
				}
			}
			if p-startRunes < 2 || letters < 2 {
				t.Errorf("Hyphenate(%q): point %d leaves <2 letters on a side", word, p)
			}
		}
		got := defaultHyphenator.Hyphenate(word)
		if len(got) != len(bare) {
			t.Errorf("Hyphenate(%q): %d points, want %d (same as bare %q)", word, len(got), len(bare), core)
			continue
		}
		for i := range got {
			if got[i] != bare[i]+startRunes {
				t.Errorf("Hyphenate(%q): point %d, want bare point %d shifted by %d", word, got[i], bare[i], startRunes)
			}
		}
	}
}

// --- Width utilities ---

func TestWidthPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("JustifyParagraph with width 0 did not panic")
		}
	}()
	JustifyParagraph("hello", 0)
}

// --- Wrap ---

func TestWrapParagraphFits(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow chasing butterflies."
	for _, ln := range wrapParagraph(input, testWidth) {
		if len([]rune(ln)) > testWidth {
			t.Errorf("wrapped line exceeds width: %q", ln)
		}
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
		got := justifyGapCost(c.slack, c.words)
		if got != c.want {
			t.Errorf("justifyGapCost(%d, %d) = %v, want %v",
				c.slack, c.words, got, c.want)
		}
	}
}

// --- Justified output properties ---

func TestJustify_FlushLines(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow chasing butterflies"
	lines := JustifyParagraph(input, testWidth)
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}
	for i, ln := range lines[:len(lines)-1] {
		if runeLen(ln) != testWidth {
			t.Errorf("line %d: %d runes, want %d: %q",
				i, runeLen(ln), testWidth, ln)
		}
	}
	// Last line must not exceed width.
	last := lines[len(lines)-1]
	if runeLen(last) > testWidth {
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
	lines := JustifyParagraph(input, testWidth)
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
	lines := JustifyParagraph(input, testWidth)
	for i, ln := range lines[:len(lines)-1] {
		gap := maxConsecutiveSpaces(ln)
		if gap > 3 {
			t.Errorf("line %d: gap=%d, expected hyphenation to reduce it: %q",
				i, gap, ln)
		}
	}
}

func TestJustifyLine(t *testing.T) {
	cases := []struct {
		line  string
		width int
		want  string
	}{
		// 3 words (6 chars), width 14, 8 total spaces across 2 gaps = 4+4
		{"aa bb cc", 14, "aa    bb    cc"},
		// single word unchanged
		{"hello", 10, "hello"},
		// already at width
		{"ab cd ef", 8, "ab cd ef"},
	}
	for _, c := range cases {
		got := justifyLine(c.line, c.width)
		if got != c.want {
			t.Errorf("justifyLine(%q, %d) = %q, want %q",
				c.line, c.width, got, c.want)
		}
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

func TestWrapLines_MeasuredFits(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow chasing illuminated butterflies"
	m := fakeMeasurer{}
	lines := WrapLines(input, 300, m)
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

func TestJustifyLines_MeasuredSlack(t *testing.T) {
	input := "The unprecedented international collaboration has fundamentally transformed the interconnected communities over several decades"
	m := fakeMeasurer{}
	width := 300
	lines := JustifyLines(input, width, m)
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}
	for i, ln := range lines[:len(lines)-1] {
		// A justified line may exceed width only by the shrink
		// allowance (a third of a space per gap) plus, when it ends
		// in a hyphen, the hang protrusion.
		allow := (len(ln.Words) - 1) * (m.Space() / 3)
		if strings.HasSuffix(ln.Words[len(ln.Words)-1], "-") {
			allow += HangHyphen(m)
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

func (wideMeasurer) Width(s string) int { return 10 * runeLen(s) }
func (wideMeasurer) Space() int         { return 9 }

func TestJustifyLines_ShrinkAbsorbsWord(t *testing.T) {
	// Seven words of 40 units, gaps of 9. Five words measure 236:
	// at width 230 they overflow by 6, within the shrink allowance
	// 4*(9/3) = 12, costing far less than the loose four-word
	// alternative. The optimum is a shrunk five-word first line and
	// a natural two-word last line.
	m := wideMeasurer{}
	lines := JustifyLines("aaaa bbbb cccc dddd eeee ffff gggg", 230, m)
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

func TestHangHyphen(t *testing.T) {
	// Monospace: a cell cannot protrude fractionally.
	if got := HangHyphen(Mono); got != 0 {
		t.Errorf("HangHyphen(Mono) = %d, want 0", got)
	}
	// wideMeasurer: hyphen is 10 units, 70% hangs.
	if got := HangHyphen(wideMeasurer{}); got != 7 {
		t.Errorf("HangHyphen(wideMeasurer) = %d, want 7", got)
	}
}

func TestTryHyphenAtJustify_HangExtendsTarget(t *testing.T) {
	// wideMeasurer: runes 10, space 9, shrink 9/3=3, hang 7. The
	// prefix "abc-" is 40 wide; with 60 used the line totals
	// 60+9+40 = 109. For a two-word line the plain window is
	// width+shrink = 108, so only the hang (target 112, window 115)
	// admits the break.
	m := wideMeasurer{}
	w := word{text: "abcdef", points: []int{3}}
	if _, ok := tryHyphenAtJustify(w, 60, 105, 2, 0, m); !ok {
		t.Errorf("hyphen break rejected at width 105: hang should extend the target")
	}
	if _, ok := tryHyphenAtJustify(w, 60, 97, 2, 0, m); ok {
		t.Errorf("hyphen break accepted at width 97: outside hang+shrink window")
	}
}

func TestJustifyLines_MonoNeverShrinks(t *testing.T) {
	// The monospace measurer has no sub-character shrink: every
	// line must fit within width at natural spacing.
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow"
	for _, ln := range JustifyLines(input, testWidth, Mono) {
		if ln.Width > testWidth {
			t.Errorf("mono line overfull: %d > %d: %v", ln.Width, testWidth, ln.Words)
		}
	}
}

func TestJustifyLines_MonoMatchesJustifyParagraph(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow"
	lines := JustifyLines(input, testWidth, Mono)
	flat := flattenLines(lines)
	want := JustifyParagraph(input, testWidth)
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

func TestWrapVsJustify_Differ(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow"
	ragged := strings.Join(wrapParagraph(input, testWidth), "\n")
	justified := strings.Join(JustifyParagraph(input, testWidth), "\n")
	if ragged == justified {
		t.Error("ragged and justified output should differ")
	}
}

