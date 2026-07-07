package typeset

import (
	"strings"
	"testing"
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

// --- Width utilities ---

func TestTruncLines(t *testing.T) {
	in := "short\nthis line is longer than ten runes"
	out := TruncLines(in, 10)
	if out != "short\nthis line " {
		t.Errorf("TruncLines = %q", out)
	}
}

func TestWidthPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Wrap with width 0 did not panic")
		}
	}()
	Wrap("hello", 0)
}

// --- Wrap ---

func TestWrapFitsAndPreserves(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow chasing butterflies.\n\nSecond paragraph follows after a blank line."
	out := Wrap(input, testWidth)
	for _, ln := range strings.Split(out, "\n") {
		if len([]rune(ln)) > testWidth {
			t.Errorf("wrapped line exceeds width: %q", ln)
		}
	}
	if !strings.Contains(out, "\n\n") {
		t.Error("paragraph separation lost")
	}
}

func TestWrapPreformattedPassthrough(t *testing.T) {
	input := "Normal paragraph here.\n\nKey  Value\nFoo  Bar"
	out := Wrap(input, testWidth)
	if !strings.Contains(out, "Key  Value") || !strings.Contains(out, "Foo  Bar") {
		t.Errorf("preformatted line modified: %q", out)
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
	out := Justify(input, testWidth)
	lines := strings.Split(out, "\n")
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
	out := Justify(input, testWidth)
	lines := strings.Split(out, "\n")
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
	out := Justify(input, testWidth)
	lines := strings.Split(out, "\n")
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
	ragged := Wrap(input, testWidth)
	justified := Justify(input, testWidth)
	if ragged == justified {
		t.Error("ragged and justified output should differ")
	}
}

func TestJustify_PreformattedPassthrough(t *testing.T) {
	// Lines with 2+ consecutive spaces are preformatted.
	input := "Normal paragraph here.\n\nKey  Value\nFoo  Bar"
	out := Justify(input, testWidth)
	if !strings.Contains(out, "Key  Value") {
		t.Errorf("preformatted line modified: %q", out)
	}
	if !strings.Contains(out, "Foo  Bar") {
		t.Errorf("preformatted line modified: %q", out)
	}
}
