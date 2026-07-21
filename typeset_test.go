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

func TestWrapVsJustify_Differ(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow"
	ragged := strings.Join(wrapParagraph(input, testWidth), "\n")
	justified := strings.Join(JustifyParagraph(input, testWidth), "\n")
	if ragged == justified {
		t.Error("ragged and justified output should differ")
	}
}

