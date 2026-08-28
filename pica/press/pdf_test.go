package press

import (
	"fmt"
	"strings"
	"testing"

	"repani.com/pica"
)

func TestPDFEndToEnd(t *testing.T) {
	var b strings.Builder
	b.WriteString("E2E TEST SHEET\n\n")
	for i := range 48 {
		fmt.Fprintf(&b, "# Section %d\n\n", i)
		b.WriteString(strings.Repeat("The quick brown fox jumps over the lazy dog and keeps running through the sunlit meadow. ", 3))
		b.WriteString("\n\n")
	}
	b.WriteString(".table 6L *L 4R\nDay | Conditions | Temp\nMon | Sunny with a strengthening westerly breeze | 25\nTue | Cloudy | 22\n.end\n")

	doc, err := pica.Parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	out, err := PDF(doc, false)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "%PDF-1.3") {
		t.Fatal("not a PDF")
	}
	if pages := strings.Count(s, "/Type /Page\n"); pages < 2 {
		t.Fatalf("pages = %d, want multi-page", pages)
	}

	// Deterministic bytes: rendering again is identical.
	out2, err := PDF(doc, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(out2) != s {
		t.Fatal("PDF bytes are not deterministic")
	}
}

func TestPDF_DerivedSizeFloor(t *testing.T) {
	src := "T\n\nbody text here\n\n.paper a5\n.cols 4\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PDF(doc, false); err == nil {
		t.Fatal("expected readability-floor error for a5/4col/width40")
	}
}

// TestFlow_RepeatTallerThanColumnTerminates is the regression for a
// non-progress loop: a repeated lead-in as tall as the column used to
// make rest() reconstruct the identical block forever.

func TestPDF_Sans(t *testing.T) {
	src := "The Daily Fable\n\n# Weather\n\n" +
		strings.Repeat("The quick brown fox jumps over the lazy dog and then runs swiftly across the sunlit meadow. ", 6) +
		"\n\n.table 10L 6R\nCity | Temp\nAthens | 31\nNicosia | 34\n.end\n\n.link https://example.com Example\n\n.width 40\n.cols 2\n.font sans\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	b1, err := PDF(doc, false)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := PDF(doc, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b1), "FiraSans-Regular") {
		t.Error("sans document does not embed Fira Sans")
	}
	if !strings.Contains(string(b1), "FiraMono-Regular") {
		t.Error("sans document with a table should still embed Fira Mono")
	}
	if string(b1) != string(b2) {
		t.Error("sans PDF is not deterministic")
	}
}

func TestPDF_NewBlocks(t *testing.T) {
	body := "\n\n.by A. Writer\n.date Today\n\n" +
		".quote\n" + strings.Repeat("The quick brown fox jumps over the lazy dog. ", 3) + "\n.attrib Aesop\n.end\n\n" +
		".item first item that runs long enough to wrap onto another line for sure\n.item second item\n\n" +
		strings.Repeat("Plain prose follows the list and fills the columns evenly. ", 4) + "\n"
	for _, trailer := range []string{"\n.width 40\n.cols 2\n", "\n.width 40\n.cols 2\n.font sans\n"} {
		doc, err := pica.Parse("The Daily Fable" + body + trailer)
		if err != nil {
			t.Fatal(err)
		}
		b1, err := PDF(doc, false)
		if err != nil {
			t.Fatal(err)
		}
		b2, err := PDF(doc, false)
		if err != nil {
			t.Fatal(err)
		}
		if string(b1) != string(b2) {
			t.Errorf("PDF with new blocks is not deterministic (%s)", strings.TrimSpace(trailer))
		}
	}
}

// TestDeriveTypo_FloorGuardsMonoSize: in a sans document the mono
// size (tables, verbatim) is the smaller of the two derived sizes;
// the readability floor must catch it even when the body size passes.
