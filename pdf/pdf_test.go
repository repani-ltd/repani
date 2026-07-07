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
