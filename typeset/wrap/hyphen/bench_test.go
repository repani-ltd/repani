package hyphen

import "testing"

func BenchmarkHyphenate(b *testing.B) {
	words := []string{"hyphenation", "thunderstorm", "temperature", "international",
		"extraordinarily", "φαρμακείο", "θερμοκρασία", "four-line", "reconsider"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, w := range words {
			Default.Hyphenate(w)
		}
	}
}
