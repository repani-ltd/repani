package pica

import (
	"strings"
	"testing"

	"repani.com/typeset/wrap"
)

// The breaker's tests -- hyphenation, ragged and justified wrapping,
// measured slack -- live with it, in typeset/wrap.

// wideMeasurer is a proportional caricature with a wide space: ten
// units a rune, nine a space.
type wideMeasurer struct{}

func (wideMeasurer) Width(s string) int { return 10 * runeLen(s) }
func (wideMeasurer) Space() int         { return 9 }

func TestSpecEmbedsLanguageReference(t *testing.T) {
	// Spec is doc.go's comment body: the language sections must be
	// present and the Go comment/package furniture stripped.
	s := Spec()
	for _, want := range []string{"# The language", "# Emphasis", "# Layout trailer", "# Tables", "# Wrapping"} {
		if !strings.Contains(s, want) {
			t.Errorf("Spec() missing section %q", want)
		}
	}
	// Maintainer doctrine lives in DESIGN.t, not the spec.
	for _, gone := range []string{"# Growing the vocabulary", "# Non-goals"} {
		if strings.Contains(s, gone) {
			t.Errorf("Spec() carries maintainer section %q", gone)
		}
	}
	if strings.Contains(s, "package pica") || strings.Contains(s, "/*") {
		t.Error("Spec() leaks source furniture")
	}
}

func TestWrapLinesIsWrapRagged(t *testing.T) {
	// Pica's names are the shared breaker's: a paragraph breaks the
	// same through either.
	const p = "Isolated thunderstorms developing inland during the afternoons"
	got := strings.Join(wrapParagraph(p, 20), "|")
	want := strings.Join(wrap.Flatten(wrap.Ragged(p, 20, wrap.PenaltyProse, wrap.Mono)), "|")
	if got != want {
		t.Errorf("wrapParagraph = %q, wrap.Ragged = %q", got, want)
	}
}
