package pica

import (
	"strings"
	"testing"
)

// The breaker's benchmarks live with it, in typeset/wrap.

func BenchmarkTableLayout(b *testing.B) {
	src := []string{"T", "", ".table 3L *L 8N 6R!", "^Day | Forecast | Temp | Wind"}
	for range 12 {
		src = append(src, "Mon | Isolated thunderstorms inland, clearing by evening | 25.5 | NW 15",
			".. | Forecast confidence is moderate for the afternoon period | |")
	}
	src = append(src, "= | Average | (23.25) |", ".end", "", ".width 40")
	doc, err := Parse(strings.Join(src, "\n") + "\n")
	if err != nil {
		b.Fatal(err)
	}
	tb := doc.Blocks[0].Table
	b.ReportAllocs()
	for b.Loop() {
		if _, err := tb.Layout(40); err != nil {
			b.Fatal(err)
		}
	}
}
