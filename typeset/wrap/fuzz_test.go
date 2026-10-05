package wrap

import (
	"strings"
	"testing"
	"unicode/utf8"

	"repani.com/typeset/wrap/hyphen"
)

// FuzzBreakers checks the promise every breaker makes: each line fits
// its measure unless it is one rune wider than it, and the lines give
// back the paragraph's text -- nothing dropped, nothing invented but
// the hyphens at line ends. Text is UTF-8: a broken word comes back
// as runes, so an invalid byte in it becomes U+FFFD.
func FuzzBreakers(f *testing.F) {
	for _, s := range []string{"Isolated thunderstorms developing inland", "headerless row", "abcd--efghijklmnop", "λλλλλλλλ x", "a-b-c-d-e-f", ""} {
		f.Add(s, 6, true)
	}
	f.Fuzz(func(t *testing.T, para string, width int, hyph bool) {
		if width < 1 || width > 80 || !utf8.ValidString(para) {
			return
		}
		var h Hyphenator
		if hyph {
			h = hyphen.Default
		}
		want := strings.Join(Fields(para), "")
		for name, lines := range map[string][]Line{
			"ragged":  Hyphenated(para, width, width, h, PenaltyCell, Mono),
			"justify": Justify(Tokens(para, Mono), width, width, h, Mono),
			"wide":    Hyphenated(para, 10*width, 10*width, h, PenaltyProse, wideMeasurer{}),
		} {
			measure := width
			var m Measurer = Mono
			if name == "wide" {
				measure, m = 10*width, wideMeasurer{}
			}
			var got strings.Builder
			for _, ln := range lines {
				if ln.Width > measure && (len(ln.Words) != 1 || multiRune(ln.Words[0])) {
					t.Fatalf("%s %q at %d: line %q is %d wide", name, para, width, ln.Words, ln.Width)
				}
				if ln.Width != LineOf(ln.Words, m).Width {
					t.Fatalf("%s %q at %d: line %q claims width %d", name, para, width, ln.Words, ln.Width)
				}
				for _, w := range ln.Words {
					got.WriteString(w)
				}
			}
			if !rejoins(got.String(), want) {
				t.Fatalf("%s %q at %d: lines %q do not give back the text", name, para, width, Flatten(lines))
			}
		}
	})
}

// rejoins reports whether got is want with hyphens added: a breaker
// adds a "-" at a hyphenated line end and nothing else.
func rejoins(got, want string) bool {
	for want != "" {
		if strings.HasPrefix(got, want[:1]) {
			got, want = got[1:], want[1:]
		} else if strings.HasPrefix(got, "-") {
			got = got[1:]
		} else {
			return false
		}
	}
	return strings.Trim(got, "-") == ""
}
