package main

import (
	"bytes"
	"strings"
	"testing"

	"repani.com/pica"
	"repani.com/pica/pdf"
)

func TestComposeMonoEmphasisUnderline(t *testing.T) {
	// One-line paragraph: the marker underscores blank to spaces
	// and the underline covers exactly their cells and the span
	// between -- the grid, and the text-page identity, unmoved.
	doc, err := pica.Parse("T\n\nsay _two words_ now.\n\n.width 30\n")
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := compose(doc, typo{ps: 9, psMono: 9, lineH: 11})
	if err != nil {
		t.Fatal(err)
	}
	ln := blocks[0].segs[0].lines[0]
	if ln.text != "say  two words  now." {
		t.Errorf("text = %q, want markers blanked in place", ln.text)
	}
	if len(ln.uline) != 1 || ln.uline[0] != (pica.Span{Start: 4, End: 15}) {
		t.Errorf("uline = %v, want [{4 15}]", ln.uline)
	}
}

func TestComposeMonoEmphasisAcrossLines(t *testing.T) {
	// A span broken across wrapped lines: the state carries, every
	// marker is blanked, and both fragments carry underline spans.
	doc, err := pica.Parse("T\n\nsay _two words again_ now more.\n\n.width 12\n")
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := compose(doc, typo{ps: 9, psMono: 9, lineH: 11})
	if err != nil {
		t.Fatal(err)
	}
	underlined := 0
	for _, s := range blocks[0].segs {
		for _, ln := range s.lines {
			if strings.Contains(ln.text, "_") {
				t.Errorf("marker survived in %q", ln.text)
			}
			for _, sp := range ln.uline {
				if sp.Start >= sp.End || sp.End > len([]rune(ln.text)) {
					t.Errorf("span %v out of bounds in %q", sp, ln.text)
				}
				underlined++
			}
		}
	}
	if underlined < 2 {
		t.Errorf("expected the span to underline on at least two lines, got %d span(s)", underlined)
	}
}

func TestComposeItemEmphasisOffsets(t *testing.T) {
	// The scan runs on the final line (bullet baked in), so the
	// underline cells are the drawn cells.
	doc, err := pica.Parse("T\n\n.item an _x_ y\n\n.width 30\n")
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := compose(doc, typo{ps: 9, psMono: 9, lineH: 11})
	if err != nil {
		t.Fatal(err)
	}
	ln := blocks[0].segs[0].lines[0]
	if ln.text != "• an  x  y" {
		t.Errorf("text = %q", ln.text)
	}
	if len(ln.uline) != 1 || ln.uline[0] != (pica.Span{Start: 5, End: 8}) {
		t.Errorf("uline = %v, want [{5 8}]", ln.uline)
	}
}

func TestComposeSansEmphasisFlags(t *testing.T) {
	doc, err := pica.Parse("T\n\nsay _two words_ now.\n\n.width 30\n.font sans\n")
	if err != nil {
		t.Fatal(err)
	}
	units := 30 * pdf.AvgAdvance(pdf.Sans)
	blocks, err := compose(doc, typo{sans: true, ps: 9, psMono: 9, lineH: 11, units: units})
	if err != nil {
		t.Fatal(err)
	}
	ln := blocks[0].segs[0].lines[0]
	if ln.emph == nil || len(ln.emph) != len(ln.words) {
		t.Fatalf("emph = %v for words %v", ln.emph, ln.words)
	}
	var flagged []string
	for i, w := range ln.words {
		if strings.Contains(w, "_") {
			t.Errorf("marker survived in word %q", w)
		}
		if ln.emph[i] {
			flagged = append(flagged, w)
		}
	}
	if strings.Join(flagged, " ") != "two words" {
		t.Errorf("emphasized words = %v, want [two words]", flagged)
	}
}

func TestPDFEmphasisSmoke(t *testing.T) {
	// Full renders through both faces: the sans document must embed
	// the italic subset; the mono document must not (its emphasis
	// is a stroked rule, not a face).
	sans, err := pica.Parse("T\n\nsay _two words_ now.\n\n.font sans\n")
	if err != nil {
		t.Fatal(err)
	}
	out, err := report(sans, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("FiraSans-Italic")) {
		t.Error("sans render with emphasis does not embed the italic face")
	}
	mono, err := pica.Parse("T\n\nsay _two words_ now.\n")
	if err != nil {
		t.Fatal(err)
	}
	out, err = report(mono, false)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("FiraSans-Italic")) {
		t.Error("mono render embeds the italic face it never draws")
	}
	if bytes.Contains(out, []byte("_")) {
		// Content streams are compressed; this only guards the
		// metadata, but a failure here would be loud.
		t.Log("note: underscore bytes present in mono PDF metadata")
	}
}
