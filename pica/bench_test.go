package pica

import "testing"

// The breaker's benchmarks live with it, in typeset/wrap.

func BenchmarkTableLayout(b *testing.B) {
	tbl, err := NewTable("3L *L 8N 6R!")
	if err != nil {
		b.Fatal(err)
	}
	tbl.Header("Day", "Forecast", "Temp", "Wind")
	for i := 0; i < 12; i++ {
		tbl.Row("Mon", "Isolated thunderstorms inland, clearing by evening", "25.5", "NW 15")
		tbl.Note("", "Forecast confidence is moderate for the afternoon period", "", "")
	}
	tbl.Total("", "Average", "(23.25)", "")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := tbl.Layout(40); err != nil {
			b.Fatal(err)
		}
	}
}
