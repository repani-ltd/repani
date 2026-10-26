package press

import (
	"testing"

	"repani.com/pica"
	"repani.com/pica/pdf"
)

func TestComposeLinkMono(t *testing.T) {
	// A titled link sets the mark and title, then the URL (scheme
	// stripped) indented under the title, both lines clickable; an
	// untitled link is its URL after the mark, one line.
	src := "T\n\n.link https://repani.com/pica The pica reference\n.link http://example.com/bare\n\n.width 30\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := compose(doc, typo{ps: 9, psMono: 9, lineH: 11})
	if err != nil {
		t.Fatal(err)
	}
	titled := blocks[0].segs[0].lines
	if len(titled) != 2 || titled[0].text != "↗ The pica reference" || titled[1].text != "  repani.com/pica" {
		t.Errorf("titled link = %+v", titled)
	}
	for _, ln := range titled {
		if ln.href != "https://repani.com/pica" {
			t.Errorf("href = %q", ln.href)
		}
	}
	if bare := blocks[1].segs[0].lines; len(bare) != 1 || bare[0].text != "↗ http://example.com/bare" {
		t.Errorf("untitled link = %+v", bare)
	}
}

func TestComposeLinkSans(t *testing.T) {
	// Proportional: the URL line starts at the mark's measured
	// width, under the title's first letter, and is cut to fit the
	// measure that remains.
	src := "T\n\n.link https://example.com/" + "a/very/long/path/that/cannot/possibly/fit/the/measure/at/all" + " Long\n\n.width 30\n.font sans\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	units := 30 * 500
	blocks, err := compose(doc, typo{ps: 9, psMono: 9, lineH: 11, sans: true, units: units})
	if err != nil {
		t.Fatal(err)
	}
	m := pdf.Measure(pdf.Sans)
	lines := blocks[0].segs[0].lines
	if len(lines) != 2 || lines[0].words[0] != "↗ Long" {
		t.Fatalf("lines = %+v", lines)
	}
	addr := lines[1]
	if addr.indent != m.Width(linkMark) || addr.indent+m.Width(addr.words[0]) > units {
		t.Errorf("URL line = %+v", addr)
	}
}
