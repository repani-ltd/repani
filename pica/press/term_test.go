package press

import (
	"bytes"
	"strings"
	"testing"

	"repani.com/pica"
	"repani.com/pica/pdf"
)

func TestComposeTermMono(t *testing.T) {
	// The mono composition is the text page cell for cell: the
	// label runs in and is recorded as the line's lead; an
	// emphasis span in the text lands on its drawn cells even
	// though the label holds underscores of its own.
	src := "T\n\n.term _x\nan _y_ z\n.term A LABEL THAT IS TOO LONG\nstands _alone_\n\n.width 34\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := compose(doc, typo{ps: 9, psMono: 9, lineH: 11})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(blocks))
	}
	ln := blocks[0].segs[0].lines[0]
	if ln.text != "_x  an  y  z" || ln.lead != "_x" {
		t.Errorf("run-in line = %q lead %q", ln.text, ln.lead)
	}
	if len(ln.uline) != 1 || ln.uline[0] != (pica.Span{Start: 7, End: 10}) {
		t.Errorf("uline = %v, want [{7 10}]", ln.uline)
	}
	standing := blocks[1]
	if len(standing.segs) != 2 {
		t.Fatalf("standing term segs = %d, want label + one text line", len(standing.segs))
	}
	if l0 := standing.segs[0].lines[0]; l0.text != "A LABEL THAT IS TOO LONG" || l0.lead != l0.text {
		t.Errorf("standing label line = %+v", l0)
	}
	if l1 := standing.segs[1].lines[0]; l1.text != "  stands  alone " || l1.lead != "" || len(l1.uline) != 1 {
		t.Errorf("standing text line = %q lead %q uline %v", l1.text, l1.lead, l1.uline)
	}
	// Line counts agree with the text writer (three lines, six
	// half-line units), plus the run's half-line spacer after the
	// first entry, since the second turns over -- the item-run
	// policy, which the text page does not have.
	text, _ := doc.Text()
	if got, want := blocks[0].height()+blocks[1].height(), 2*3+1; got != want {
		t.Errorf("composed height %d units, want %d (text page:\n%s)", got, want, text)
	}
}

func TestComposeTermSans(t *testing.T) {
	// Proportional: the first line's words start past the label
	// (measured bold) plus the gap; turnovers hang; a standing
	// label is a lead with no words.
	src := "T\n\n.term FUEL\n" + strings.Repeat("open from six until two on the south quay ", 3) +
		"\n.term A LABEL THAT IS VERY MUCH TOO LONG TO RUN IN\ntext\n\n.width 34\n.font sans\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := compose(doc, typo{ps: 9, psMono: 9, lineH: 11, sans: true, units: 34 * 500})
	if err != nil {
		t.Fatal(err)
	}
	m, mb := pdf.Measure(pdf.Sans), pdf.Measure(pdf.SansBold)
	first := blocks[0].segs[0].lines[0]
	if first.lead != "FUEL" || first.indent != mb.Width("FUEL")+2*m.Space() || len(first.words) == 0 {
		t.Errorf("run-in line = %+v", first)
	}
	if len(blocks[0].segs) < 2 {
		t.Fatal("expected turnovers")
	}
	if second := blocks[0].segs[1].lines[0]; second.lead != "" || second.indent != pica.ItemIndent*m.Space() {
		t.Errorf("turnover line = %+v", second)
	}
	standing := blocks[1].segs[0].lines[0]
	if standing.lead == "" || len(standing.words) != 0 {
		t.Errorf("standing label = %+v", standing)
	}
}

func TestPDFTermSmoke(t *testing.T) {
	// Both presentations draw a document of terms without error.
	src := "T\n\n.term FUEL\n06:00-14:00, _south_ quay\n.term A LABEL THAT IS TOO LONG\nstands alone\n\n.width 34\n"
	for _, trailer := range []string{"", ".font sans\n"} {
		doc, err := pica.Parse(src + trailer)
		if err != nil {
			t.Fatal(err)
		}
		out, err := PDF(doc, false)
		if err != nil {
			t.Fatalf("PDF (%q): %v", trailer, err)
		}
		if !bytes.HasPrefix(out, []byte("%PDF")) {
			t.Errorf("not a PDF (%q)", trailer)
		}
	}
}
