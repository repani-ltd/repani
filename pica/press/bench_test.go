package press

import (
	"os"
	"testing"

	"repani.com/pica"
)

func BenchmarkForm(b *testing.B) {
	doc, err := pica.Parse(formDoc)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := PDF(doc, false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPDF(b *testing.B) {
	src, err := os.ReadFile("../example/triptych.t")
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
		out, err := PDF(doc, false)
		if err != nil {
			b.Fatal(err)
		}
		n = len(out)
	}
	b.SetBytes(int64(n))
}
