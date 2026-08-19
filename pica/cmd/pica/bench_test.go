package main

import (
	"os"
	"testing"

	"repani.com/pica"
)

func BenchmarkBroadsheet(b *testing.B) {
	src, err := os.ReadFile("../../example/triptych.t")
	if err != nil {
		b.Fatal(err)
	}
	doc, err := pica.Parse(string(src))
	if err != nil {
		b.Fatal(err)
	}
	var n int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := broadsheet(doc)
		if err != nil {
			b.Fatal(err)
		}
		n = len(out)
	}
	b.SetBytes(int64(n))
}
