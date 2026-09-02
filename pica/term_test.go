package pica

import (
	"errors"
	"strings"
	"testing"
)

func TestTermBlock(t *testing.T) {
	// .term LABEL takes its text from the unmarked lines beneath,
	// filling as an item does; consecutive terms are tight; the
	// label is verbatim (an underscore in it is a character).
	d := mustParse(t, "T\n\n.term FUEL\n06:00-14:00,\nsouth quay\n.term _odd_ name\nsecond\n\n# H\n")
	if len(d.Blocks) != 3 || d.Blocks[0].Kind != Term || d.Blocks[1].Kind != Term {
		t.Fatalf("Blocks = %+v", d.Blocks)
	}
	if got := d.Blocks[0]; got.Label != "FUEL" || got.Text != "06:00-14:00, south quay" {
		t.Errorf("first term = %q / %q", got.Label, got.Text)
	}
	if got := d.Blocks[1]; got.Label != "_odd_ name" || got.Text != "second" || !got.Tight {
		t.Errorf("second term = %+v", got)
	}
	if d.Blocks[2].Tight {
		t.Error("heading after a blank line should not be tight")
	}
}

func TestTermErrors(t *testing.T) {
	for _, src := range []string{
		"T\n\n.term\ntext\n",                 // no label
		"T\n\n.term LABEL\n\nprose.\n",       // no text: the blank line ended it
		"T\n\n.term LABEL\n# heading\n",      // no text: a marked line ended it
		"T\n\n.term LABEL\nan _open span\n", // emphasis lives in the text
	} {
		_, err := Parse(src)
		if err == nil {
			t.Errorf("Parse(%q) accepted", src)
			continue
		}
		if !errors.Is(err, ErrBadAttr) && !errors.Is(err, ErrUnclosedEmph) {
			t.Errorf("Parse(%q) err = %v", src, err)
		}
	}
}

func TestTermText(t *testing.T) {
	// Run in: label, two spaces, text; turnovers hang ItemIndent.
	// A label leaving less than half the width stands alone.
	src := strings.Join([]string{
		"T",
		"",
		".term PORT POLICE",
		"VHF 12 · 22880 21344",
		".term SHOWERS",
		"06:00-22:00, port office, and a turnover line",
		".term A LABEL THAT IS TOO LONG",
		"stands alone",
		"",
		".width 34",
	}, "\n") + "\n"
	d := mustParse(t, src)
	out, err := d.Text()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"T",
		"",
		"PORT POLICE  VHF 12 · 22880 21344",
		"SHOWERS  06:00-22:00, port office,",
		"  and a turnover line",
		"A LABEL THAT IS TOO LONG",
		"  stands alone",
	}, "\n") + "\n"
	if out != want {
		t.Errorf("Text =\n%s\nwant\n%s", out, want)
	}
	for i, ln := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if runeLen(ln) > 34 {
			t.Errorf("line %d exceeds width: %q", i+1, ln)
		}
	}
	// The run-in rule, at the boundary: a label leaving exactly
	// half the width runs in; one rune longer stands alone.
	if first, ok := TermRunIn(strings.Repeat("x", 15), 34); !ok || first != 17 {
		t.Errorf("TermRunIn(15 runes, 34) = %d, %v; want 17, true", first, ok)
	}
	if _, ok := TermRunIn(strings.Repeat("x", 16), 34); ok {
		t.Error("TermRunIn(16 runes, 34) should stand alone")
	}
}

func TestTermRunInWrap(t *testing.T) {
	// The first line sets on what the lead leaves, later lines on
	// the full measure: no line of either exceeds its measure.
	para := strings.Repeat("alpha beta gamma delta ", 6)
	lines := WrapLinesRunIn(para, 10, 30, Mono)
	if len(lines) < 3 {
		t.Fatalf("lines = %d", len(lines))
	}
	if lines[0].Width > 10 {
		t.Errorf("first line width %d > 10: %v", lines[0].Width, lines[0].Words)
	}
	for i, ln := range lines[1:] {
		if ln.Width > 30 {
			t.Errorf("line %d width %d > 30", i+2, ln.Width)
		}
	}
	just := JustifyParagraphRunIn(para, 10, 30)
	if runeLen(just[0]) != 10 {
		t.Errorf("justified first line %q not flushed to 10", just[0])
	}
	for i, ln := range just[1 : len(just)-1] {
		if runeLen(ln) != 30 {
			t.Errorf("justified line %d %q not flushed to 30", i+2, ln)
		}
	}
}

func TestTermHTML(t *testing.T) {
	d := mustParse(t, "T\n\n.term a <b>\nsee _x_\n.term c\nd\n\nprose\n\n.item i\n")
	out := d.HTML()
	want := "<dl>\n<dt>a &lt;b&gt;</dt>\n<dd>see <em>x</em></dd>\n<dt>c</dt>\n<dd>d</dd>\n</dl>\n<p>prose</p>\n<ul>\n<li>i</li>\n</ul>\n"
	if !strings.Contains(out, want) {
		t.Errorf("HTML =\n%s\nwant to contain\n%s", out, want)
	}
}
