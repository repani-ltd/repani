package wrap

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

func BenchmarkRagged(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Ragged(benchPara, 40, PenaltyProse, Mono)
	}
}

func BenchmarkJustifyParagraph(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		JustifyParagraph(benchPara, 40)
	}
}
