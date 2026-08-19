//go:build fontcheck

package ttf

// On-demand glyph-coverage probe for the embedded faces, kept out of
// the default test run by the fontcheck build tag:
//
//	go test -tags fontcheck -run TestGlyphCoverage -v ./pica/pdf/ttf
//
// It answers "can this face draw block/quadrant/braille cells?" -- the
// question behind wire image rendering -- and reports rather than
// asserts, because coverage is a property of the shipped fonts, not
// of this package.

import (
	"fmt"
	"os"
	"testing"
)

func TestGlyphCoverage(t *testing.T) {
	ranges := []struct {
		name  string
		runes []rune
	}{
		{"half blocks (U+2580 U+2584 U+2588)", []rune{0x2580, 0x2584, 0x2588}},
		{"quadrants (U+2596..259F)", span(0x2596, 0x259F, 1)},
		{"braille (U+2800..28FF)", span(0x2800, 0x28FF, 1)},
		{"box drawing (U+2500..257F)", span(0x2500, 0x257F, 1)},
	}
	for _, name := range []string{"FiraMono-Regular.ttf", "FiraMono-Bold.ttf", "FiraSans-Regular.ttf", "FiraSans-Bold.ttf"} {
		raw, err := os.ReadFile("../fonts/" + name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := Parse(raw)
		if err != nil {
			t.Fatalf("Parse(%s): %v", name, err)
		}
		for _, r := range ranges {
			have := 0
			for _, c := range r.runes {
				if _, ok := f.CharToGID[int(c)]; ok {
					have++
				}
			}
			t.Log(fmt.Sprintf("%-22s %-36s %3d/%d", name, r.name, have, len(r.runes)))
		}
	}
}

func span(lo, hi rune, step int) []rune {
	var out []rune
	for r := lo; r <= hi; r += rune(step) {
		out = append(out, r)
	}
	return out
}
