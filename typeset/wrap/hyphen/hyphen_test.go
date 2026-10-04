package hyphen

import (
	"strings"
	"testing"
	"unicode"
)

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
		points := Default.Hyphenate(tt.word)
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
		points := Default.Hyphenate(tt.word)
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
		bare := Default.Hyphenate(core)
		start := strings.Index(word, core)
		startRunes := len([]rune(word[:max(start, 0)]))
		for _, p := range Default.Hyphenate(word) {
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
		got := Default.Hyphenate(word)
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

func TestMergedPatternsCoverGreek(t *testing.T) {
	// The merged all-sets hyphenator must find points in Greek
	// (disjoint scripts: the sets cannot mis-hyphenate each other).
	const word = "θερμοκρασία"
	if pts := Default.Hyphenate(word); len(pts) == 0 {
		t.Error("merged pattern sets found no points in a Greek word")
	}
}

func TestHyphenateCompoundShortPrefix(t *testing.T) {
	// The explicit-hyphen break keeps the 2-letter guard on the
	// prefix: "e-mail" must not break as "e-" | "mail".
	for _, w := range []string{"e-mail", "a--bcdefghij"} {
		runes := []rune(w)
		for _, p := range Default.Hyphenate(w) {
			if p < 3 {
				t.Errorf("Hyphenate(%q): point %d strands %q", w, p, string(runes[:p]))
			}
		}
	}
}
