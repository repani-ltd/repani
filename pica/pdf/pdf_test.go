package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
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
	// Both fonts used -> both embedded, subset (tagged), with ToUnicode.
	for _, ps := range []string{"FiraMono-Regular", "FiraMono-Bold"} {
		if !regexp.MustCompile(`/BaseFont /[A-Z]{6}\+` + ps).MatchString(s) {
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
	if !strings.Contains(s, "+FiraMono-Regular") {
		t.Error("regular font missing")
	}
	if strings.Contains(s, "+FiraMono-Bold") {
		t.Error("unused bold font embedded")
	}
}

// Embedded subsets carry a six-uppercase-letter tag (ISO 32000
// 9.6.4) on every name: deterministic for one rune set, different
// for another.
func TestSubsetTag(t *testing.T) {
	build := func(text string) string {
		var p Page
		p.SetFont(Regular, 8)
		p.Text(72, 700, text)
		doc := &Doc{}
		doc.Add(&p)
		return string(doc.Bytes())
	}
	re := regexp.MustCompile(`/(BaseFont|FontName) /([A-Z]{6})\+FiraMono-Regular\n`)
	a := re.FindAllStringSubmatch(build("abc"), -1)
	if len(a) != 3 { // Type0 BaseFont, CIDFont BaseFont, descriptor FontName
		t.Fatalf("tagged names = %d, want 3", len(a))
	}
	for _, m := range a[1:] {
		if m[2] != a[0][2] {
			t.Errorf("tags differ within one document: %s vs %s", m[2], a[0][2])
		}
	}
	if b := re.FindStringSubmatch(build("abc")); b[2] != a[0][2] {
		t.Errorf("tag not stable: %s vs %s", b[2], a[0][2])
	}
	if c := re.FindStringSubmatch(build("abd")); c[2] == a[0][2] {
		t.Errorf("tag %s does not depend on the rune set", c[2])
	}
}

// A bfrange may differ only in its last byte: runs break at xxFF.
func TestToUnicodeRangeBoundary(t *testing.T) {
	cmap := buildToUnicodeCMap(map[rune]bool{0xFE: true, 0xFF: true, 0x100: true, 0x101: true})
	for _, want := range []string{
		"2 beginbfrange\n",
		"<00FE> <00FF> <00FE>\n",
		"<0100> <0101> <0100>\n",
	} {
		if !strings.Contains(cmap, want) {
			t.Errorf("missing %q in:\n%s", want, cmap)
		}
	}
	if strings.Contains(cmap, "<00FE> <0101>") {
		t.Error("bfrange crosses the low-byte boundary")
	}
}

// Runes above the BMP are drawn as U+FFFD and must be measured as
// such, so measured and drawn widths agree.
func TestAstralMeasuredAsDrawn(t *testing.T) {
	for _, f := range []Font{Regular, Sans} {
		if got, want := Width("a\U0001F600b", f, 10), Width("a\uFFFDb", f, 10); got != want {
			t.Errorf("%s: Width(astral) = %v, want %v", f, got, want)
		}
		m := Measure(f)
		if got, want := m.Width("A\U0001F600V"), m.Width("A\uFFFDV"); got != want {
			t.Errorf("%s: Measurer.Width(astral) = %d, want %d", f, got, want)
		}
	}
	var p Page
	p.SetFont(Regular, 10)
	p.Text(72, 700, "\U0001F600")
	if s := string(p.Bytes()); !strings.Contains(s, "<FFFD>") {
		t.Errorf("astral rune not drawn as U+FFFD:\n%s", s)
	}
}

// A compressed content stream inflates back to the page content.
func TestCompressedStreamInflates(t *testing.T) {
	var p Page
	p.SetFont(Regular, 8)
	p.Text(72, 700, "inflate me")
	doc := &Doc{Compress: true}
	doc.Add(&p)
	out := doc.Bytes()

	// The page stream is the object with /Filter [ /FlateDecode ]
	// (font streams use the bare name form).
	marker := []byte("/Filter [ /FlateDecode ]\n>>\nstream\n")
	i := bytes.Index(out, marker)
	if i < 0 {
		t.Fatal("no compressed content stream")
	}
	hdr := out[bytes.LastIndex(out[:i], []byte(" 0 obj\n")):i]
	var n int
	if _, err := fmt.Sscanf(string(hdr[bytes.Index(hdr, []byte("/Length ")):]), "/Length %d", &n); err != nil {
		t.Fatalf("parse /Length: %v", err)
	}
	body := out[i+len(marker) : i+len(marker)+n]
	r, err := zlib.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, p.Bytes()) {
		t.Errorf("inflated stream differs from page content:\n%s", got)
	}
	for _, op := range []string{"BT\n", "/R 8 Tf\n", "72 700 Td\n", "] TJ\n", "ET\n"} {
		if !bytes.Contains(got, []byte(op)) {
			t.Errorf("inflated stream lacks %q", op)
		}
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
	p.Link(72, 688, 120, 698, "https://x.example/α β")
	doc := &Doc{}
	doc.Add(&p)
	s := string(doc.Bytes())
	for _, want := range []string{
		"/Subtype /Link",
		"/A << /S /URI /URI (https://x.example/a\\(b\\)) >>",
		// URIs are 7-bit ASCII: non-ASCII bytes percent-encoded.
		"/A << /S /URI /URI (https://x.example/%CE%B1 %CE%B2) >>",
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
	doc := &Doc{Title: `a(b)c\d`, Creator: "pica\nv1", Compress: false}
	doc.Add(&p)
	s := string(doc.Bytes())
	if !strings.Contains(s, `/Title (a\(b\)c\\d)`) {
		t.Errorf("title not escaped in Info dictionary")
	}
	if !strings.Contains(s, `/Creator (pica\nv1)`) {
		t.Errorf("newline not escaped in Info dictionary")
	}
	// Author, Producer, and CreationDate appear when set...
	doc = &Doc{
		Author:   "P. Christoforou",
		Producer: "repani.com/pica",
		Created:  time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC),
	}
	doc.Add(&p)
	s = string(doc.Bytes())
	for _, want := range []string{
		"/Author (P. Christoforou)",
		"/Producer (repani.com/pica)",
		"/CreationDate (D:20260826)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Info dictionary missing %s", want)
		}
	}
	// ...and are omitted, not emitted empty, when not.
	doc = &Doc{Title: "bare"}
	doc.Add(&p)
	s = string(doc.Bytes())
	for _, absent := range []string{"/Author", "/Producer", "/CreationDate"} {
		if strings.Contains(s, absent) {
			t.Errorf("unset %s emitted in Info dictionary", absent)
		}
	}

	// Non-ASCII text strings are UTF-16BE with BOM, in hex.
	doc = &Doc{Title: "Ελλάς"}
	doc.Add(&p)
	if s := string(doc.Bytes()); !strings.Contains(s, "/Title <FEFF039503BB03BB03AC03C2>") {
		t.Errorf("Greek title not UTF-16BE hex encoded")
	}
}

func TestFormXObject(t *testing.T) {
	var p Page
	p.SetFont(Regular, 10)
	p.Text(72, 700, "with a form")
	p.Form("Mark", 400, 700, 0.5)
	d := &Doc{Title: "form"}
	d.AddForm("Mark", 64, 70, "0 0 m 64 70 l S\n")
	d.Add(&p)
	out := string(d.Bytes())
	for _, want := range []string{
		"/Subtype /Form", "/BBox [ 0 0 64 70 ]", "/XObject << /Mark ",
		"q 0.5 0 0 0.5 400 700 cm /Mark Do Q", "0 0 m 64 70 l S",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	// Without forms no XObject dictionary is emitted.
	var q Page
	q.SetFont(Regular, 10)
	q.Text(72, 700, "plain")
	d2 := &Doc{Title: "plain"}
	d2.Add(&q)
	if strings.Contains(string(d2.Bytes()), "/XObject") {
		t.Error("XObject dictionary emitted with no forms")
	}
}
