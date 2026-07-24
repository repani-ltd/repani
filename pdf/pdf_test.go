package pdf

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

// buildTwoPageDoc exercises the full path: text in both fonts plus
// Greek, a line, two pages, compression on.
func buildTwoPageDoc(t *testing.T) []byte {
	t.Helper()
	var p1 Page
	p1.SetFont(Bold, 14)
	p1.Text(72, 780, "FRONT PAGE")
	p1.StrokeGray(0.5)
	p1.Line(72, 774, 300, 774, 0.6)
	p1.SetFont(Regular, 8)
	p1.Text(72, 760, "first line with Greek: αβγ")
	p1.Text(72, 750, "second line")

	var p2 Page
	p2.SetFont(Regular, 8)
	p2.Text(72, 780, "page two")

	doc := &Doc{Title: "test", Creator: "pdf_test", Compress: true}
	doc.Add(&p1)
	doc.Add(&p2)
	return doc.Bytes()
}

func TestDocStructure(t *testing.T) {
	out := buildTwoPageDoc(t)

	if !bytes.HasPrefix(out, []byte("%PDF-1.3")) {
		t.Fatal("missing PDF header")
	}
	if !bytes.HasSuffix(out, []byte("%%EOF\n")) {
		t.Fatal("missing EOF marker")
	}
	s := string(out)
	if got := strings.Count(s, "/Type /Page\n"); got != 2 {
		t.Errorf("page objects = %d, want 2", got)
	}
	if !strings.Contains(s, "/Count 2") {
		t.Error("pages tree count missing")
	}
	// Both fonts used -> both embedded, subset, with ToUnicode.
	for _, ps := range []string{"FiraMono-Regular", "FiraMono-Bold"} {
		if !strings.Contains(s, "/BaseFont /"+ps) {
			t.Errorf("missing embedded font %s", ps)
		}
	}
	if got := strings.Count(s, "/Adobe-Identity-UCS"); got != 2 {
		t.Errorf("ToUnicode CMaps = %d, want 2", got)
	}
	// The trailer's startxref must point at the xref table.
	idx := strings.LastIndex(s, "startxref\n")
	if idx < 0 {
		t.Fatal("no startxref")
	}
	rest := s[idx+len("startxref\n"):]
	pos, err := strconv.Atoi(rest[:strings.IndexByte(rest, '\n')])
	if err != nil {
		t.Fatalf("parse startxref: %v", err)
	}
	if !strings.HasPrefix(s[pos:], "xref\n") {
		t.Errorf("startxref %d does not point at xref table", pos)
	}
}

func TestUnusedFontSkipped(t *testing.T) {
	var p Page
	p.SetFont(Regular, 8)
	p.Text(72, 780, "regular only")
	doc := &Doc{}
	doc.Add(&p)
	s := string(doc.Bytes())
	if !strings.Contains(s, "/BaseFont /FiraMono-Regular") {
		t.Error("regular font missing")
	}
	if strings.Contains(s, "/BaseFont /FiraMono-Bold") {
		t.Error("unused bold font embedded")
	}
}

func TestWidthMonospace(t *testing.T) {
	// 10 runes at size 10: 10 * 600/1000 * 10 = 60pt, Greek included.
	if w := Width("abcdeαβγδε", Regular, 10); w != 60 {
		t.Errorf("Width = %v, want 60", w)
	}
}

func TestLinkAnnotation(t *testing.T) {
	var p Page
	p.SetFont(Regular, 8)
	p.Text(72, 700, "example")
	p.Link(72, 698, 120, 708, "https://x.example/a(b)")
	doc := &Doc{}
	doc.Add(&p)
	s := string(doc.Bytes())
	for _, want := range []string{
		"/Subtype /Link",
		"/A << /S /URI /URI (https://x.example/a\\(b\\)) >>",
		"/Annots [ ",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in PDF", want)
		}
	}
}

func TestMeasureProportional(t *testing.T) {
	m := Measure(Sans)
	if m.Width("iii") >= m.Width("mmm") {
		t.Errorf("sans iii (%d) should be narrower than mmm (%d)",
			m.Width("iii"), m.Width("mmm"))
	}
	if m.Space() <= 0 {
		t.Errorf("sans space width = %d", m.Space())
	}
	mono := Measure(Regular)
	if w := mono.Width("iii"); w != 3*fontByID(Regular).DefaultWidth {
		t.Errorf("mono iii = %d, want uniform advances", w)
	}
	if a := AvgAdvance(Regular); a != fontByID(Regular).DefaultWidth {
		t.Errorf("mono AvgAdvance = %d, want %d", a, fontByID(Regular).DefaultWidth)
	}
	if a := AvgAdvance(Sans); a <= 0 || a >= 1000 {
		t.Errorf("sans AvgAdvance = %d, want a plausible sub-em advance", a)
	}
}

func TestKernedMeasurement(t *testing.T) {
	// Fira Sans kerns AV by -45/1000 em; measurement must include it.
	m := Measure(Sans)
	if got, want := m.Width("AV"), m.Width("A")+m.Width("V")-45; got != want {
		t.Errorf("kerned Width(AV) = %d, want %d", got, want)
	}
	// Width (points) agrees with the measurer.
	if got, want := Width("AV", Sans, 10), float64(m.Width("AV"))/100; got != want {
		t.Errorf("Width(AV) = %v, want %v", got, want)
	}
	// Monospace has no kerning: strictly additive.
	mono := Measure(Regular)
	if got, want := mono.Width("AV"), mono.Width("A")+mono.Width("V"); got != want {
		t.Errorf("mono Width(AV) = %d, want %d", got, want)
	}
}

func TestKernedTJOutput(t *testing.T) {
	// Text: the AV pair splits the hex run with a +45 adjustment
	// (TJ subtracts, kern is -45).
	var p Page
	p.SetFont(Sans, 10)
	p.Text(72, 700, "AVID")
	s := string(p.Bytes())
	if !strings.Contains(s, "<0041> 45 <005600490044>") {
		t.Errorf("Text did not emit the AV kern adjustment:\n%s", s)
	}
	// Words: kerning applies inside words, gaps between them stay
	// exactly as given.
	var p2 Page
	p2.SetFont(Sans, 10)
	p2.Words(72, 700, []string{"AV", "id"}, []int{300})
	s2 := string(p2.Bytes())
	if !strings.Contains(s2, "<0041> 45 <0056>") {
		t.Errorf("Words did not kern inside a word:\n%s", s2)
	}
	if !strings.Contains(s2, " -300 ") {
		t.Errorf("Words lost the explicit gap:\n%s", s2)
	}
	// Unkerned monospace text stays a single run.
	var p3 Page
	p3.SetFont(Regular, 10)
	p3.Text(72, 700, "AVID")
	if s3 := string(p3.Bytes()); !strings.Contains(s3, "[ <0041005600490044> ] TJ") {
		t.Errorf("mono Text should be one unsplit run:\n%s", s3)
	}
}

func TestWordsTJ(t *testing.T) {
	var p Page
	p.SetFont(Sans, 10)
	p.Words(72, 700, []string{"Hi", "yo"}, []int{300})
	s := string(p.Bytes())
	if !strings.Contains(s, "] TJ") {
		t.Errorf("Words did not emit a TJ array:\n%s", s)
	}
	if !strings.Contains(s, " -300 ") {
		t.Errorf("Words did not emit the gap adjustment:\n%s", s)
	}
	// Single word: no adjustments.
	var p2 Page
	p2.SetFont(Sans, 10)
	p2.Words(72, 700, []string{"solo"}, nil)
	if s2 := string(p2.Bytes()); !strings.Contains(s2, "TJ") {
		t.Errorf("single-word Words did not draw:\n%s", s2)
	}
}

func TestInfoStringsEscaped(t *testing.T) {
	var p Page
	p.SetFont(Regular, 8)
	p.Text(72, 700, "body")
	doc := &Doc{Title: `a(b)c\d`, Creator: "pica", Compress: false}
	doc.Add(&p)
	s := string(doc.Bytes())
	if !strings.Contains(s, `/Title (a\(b\)c\\d)`) {
		t.Errorf("title not escaped in Info dictionary")
	}
}
