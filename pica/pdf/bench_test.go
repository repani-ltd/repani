package pdf

import "testing"

func BenchmarkPDFRender(b *testing.B) {
	pages := make([]Page, 8)
	for i := range pages {
		p := &pages[i]
		p.SetFont(Bold, 14)
		p.Text(72, 780, "FRONT PAGE αβγ")
		p.Line(72, 774, 300, 774, 0.6)
		for j := 0; j < 60; j++ {
			p.SetFont(Regular, 8)
			p.Text(72, 760-float64(j)*10, "the quick brown fox jumps over the lazy dog 0123456789")
			p.SetFont(Sans, 8)
			p.Words(300, 760-float64(j)*10, []string{"AVID", "justified", "words", "here"}, []int{250, 260, 270})
		}
		p.Link(72, 100, 200, 110, "https://example.com/α")
	}
	var n int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc := &Doc{Title: "bench", Creator: "pdf_test", Compress: true}
		for j := range pages {
			doc.Add(&pages[j])
		}
		n = len(doc.Bytes())
	}
	b.SetBytes(int64(n))
}
