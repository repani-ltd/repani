package wrap

import (
	"testing"

	"repani.com/typeset/wrap/hyphen"
)

const benchPara = "The international meteorological organisation announced that " +
	"temperatures across the southern hemisphere would remain unseasonably " +
	"warm throughout the forthcoming fortnight, with isolated thunderstorms " +
	"developing inland during the afternoons and occasionally reaching the " +
	"coastal settlements by nightfall. Hyphenation, justification and " +
	"four-line verses notwithstanding, the forecasters recommended that " +
	"travellers carry lightweight waterproof clothing and reconsider any " +
	"extraordinarily ambitious mountaineering expeditions."

func BenchmarkRagged(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Hyphenated(benchPara, 40, 40, hyphen.Default, PenaltyProse, Mono)
	}
}

func BenchmarkJustifyMono(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		JustifyMono(benchPara, 40, 40, hyphen.Default)
	}
}
