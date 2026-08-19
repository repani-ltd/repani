package fact

import (
	"os"
	"testing"
)

// benchSrc is a real projection (pica's pkg.fact): ~500 lines, mostly
// str and list(str) values.
func benchSrc(b *testing.B) []byte {
	src, err := os.ReadFile("../pica/pkg.fact")
	if err != nil {
		b.Skip(err)
	}
	return src
}

func benchFacts(b *testing.B) []Fact {
	facts, errs := Load(benchSrc(b))
	if len(errs) > 0 {
		b.Fatal(errs[0])
	}
	return facts
}

func BenchmarkParse(b *testing.B) {
	src := benchSrc(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Parse(src)
	}
}

func BenchmarkValidate(b *testing.B) {
	facts := benchFacts(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Validate(facts)
	}
}

func BenchmarkBind(b *testing.B) {
	facts := benchFacts(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Bind(facts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonical(b *testing.B) {
	facts := benchFacts(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Canonical(facts)
	}
}
