package pica

import "testing"

const benchPara = "The international meteorological organisation announced that " +
	"temperatures across the southern hemisphere would remain unseasonably " +
	"warm throughout the forthcoming fortnight, with isolated thunderstorms " +
	"developing inland during the afternoons and occasionally reaching the " +
	"coastal settlements by nightfall. Hyphenation, justification and " +
	"four-line verses notwithstanding, the forecasters recommended that " +
	"travellers carry lightweight waterproof clothing and reconsider any " +
	"extraordinarily ambitious mountaineering expeditions."

func BenchmarkHyphenate(b *testing.B) {
	words := []string{"hyphenation", "thunderstorm", "temperature", "international",
		"extraordinarily", "φαρμακείο", "θερμοκρασία", "four-line", "reconsider"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, w := range words {
			defaultHyphenator.Hyphenate(w)
		}
	}
}

func BenchmarkWrapLines(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		WrapLines(benchPara, 40, Mono)
	}
}

func BenchmarkJustifyParagraph(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		JustifyParagraph(benchPara, 40)
	}
}

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
