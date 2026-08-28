package press

import (
	"strings"
	"testing"

	"repani.com/pica"
	"repani.com/pica/pdf"
)

func TestExtractNums(t *testing.T) {
	// Grid: "Alpha      1,234.56 " — N column at [10,20), frac 3,
	// paren slot. The cell content lifts into a span anchored at
	// SepIndex and the mono text is blanked behind it.
	cols := []pica.NumCol{{Span: pica.Span{Start: 10, End: 20}, Frac: 3, Paren: true}}

	ln := extractNums(sline{text: "Alpha       1,234.56"}, cols)
	if len(ln.nums) != 1 {
		t.Fatalf("nums = %+v, want one span", ln.nums)
	}
	sp := ln.nums[0]
	if sp.intPart != "1,234" || sp.tail != ".56" || sp.sep != cols[0].SepIndex() {
		t.Errorf("span = %+v", sp)
	}
	if ln.text != "Alpha" {
		t.Errorf("blanked text = %q, want %q", ln.text, "Alpha")
	}

	// Non-numeric content stays on the mono grid, untouched.
	for _, s := range []string{"Client         Amount", "-------------- -----", "Epsilon    n/a"} {
		if got := extractNums(sline{text: s}, cols); len(got.nums) != 0 || got.text != s {
			t.Errorf("extractNums(%q) = %+v", s, got)
		}
	}

	// A line trimmed short of the cell is left alone.
	if got := extractNums(sline{text: "Alpha"}, cols); len(got.nums) != 0 || got.text != "Alpha" {
		t.Errorf("short line = %+v", got)
	}
}

func TestCompose_EvenRowPitch(t *testing.T) {
	// All notes fit one half-line: every data row is padded to the
	// same 3-unit pitch; the total row stays unpadded under its
	// rule.
	src := "T\n\n.table 6L 5N\nClient | Amt\nAlpha | 1.00\n.. custody |\nBeta | 2.00\n= Total | 3.00\n.end\n\n.width 30\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := compose(doc, typo{ps: 9, psMono: 9, lineH: 11})
	if err != nil {
		t.Fatal(err)
	}
	segs := blocks[0].segs
	// header(2 lines)=4, Alpha+note=3, Beta+pad=3, rule+Total=4.
	want := []int{4, 3, 3, 4}
	for i, s := range segs {
		if s.height() != want[i] {
			t.Errorf("seg %d height = %d, want %d (%+v)", i, s.height(), want[i], s.lines)
		}
	}

	// A wrapping note switches the table back to variable heights:
	// no padding anywhere.
	src = "T\n\n.table 6L 5N\nClient | Amt\nAlpha | 1.00\n.. a very long custody annotation that wraps |\nBeta | 2.00\n.end\n\n.width 30\n"
	doc, err = pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err = compose(doc, typo{ps: 9, psMono: 9, lineH: 11})
	if err != nil {
		t.Fatal(err)
	}
	segs = blocks[0].segs
	if n := segs[1].height(); n <= 4 {
		t.Errorf("wrapped note seg height = %d, want > 4", n)
	}
	if n := segs[2].height(); n != 2 {
		t.Errorf("plain row seg height = %d, want 2 (no pad)", n)
	}
}

func TestCompose_ProseCells(t *testing.T) {
	// In a sans document a P cell's measured lines attach to the
	// row's slines as positioned spans at the column's grid offset,
	// and the mono text reserves the space blank.
	src := "T\n\n.table 6L *P\nkey | meaning\nem | the point size squared, the unit of horizontal measure\n.end\n\n.width 30\n.font sans\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	units := 30 * pdf.AvgAdvance(pdf.Sans)
	blocks, err := compose(doc, typo{sans: true, ps: 9, psMono: 9, lineH: 11, units: units})
	if err != nil {
		t.Fatal(err)
	}
	segs := blocks[0].segs
	row := segs[1].lines // header seg first, then the row
	found := 0
	for _, ln := range row {
		for _, sp := range ln.prose {
			if sp.off != 7*runeUnits {
				t.Errorf("prose span off = %d, want %d", sp.off, 7*runeUnits)
			}
			if len(sp.words) == 0 {
				t.Error("empty prose span")
			}
			found++
		}
		if strings.Contains(ln.text, "point") {
			t.Errorf("prose content leaked into mono text: %q", ln.text)
		}
	}
	// The header row is labels: bold, blanked on the grid, drawn as
	// spans, over a per-column segmented rule.
	head := segs[0].lines
	if head[0].style != styleBold || len(head[0].prose) != 2 {
		t.Errorf("header line: style=%v spans=%d, want bold with 2 spans", head[0].style, len(head[0].prose))
	}
	last := head[len(head)-1]
	if last.style != styleRule || len(last.ruleSegs) != 2 {
		t.Errorf("header rule: style=%v segs=%v, want rule with 2 segments", last.style, last.ruleSegs)
	}
	if found == 0 {
		t.Fatal("no prose spans attached")
	}
}

func TestCompose_ItemRunGaps(t *testing.T) {
	// A tight run containing a turnover gets a half-line gap after
	// every item but the last, glued into each item's final seg; an
	// all single-line run stays tight.
	src := "T\n\n.item alpha\n.item a rather long item that certainly wraps here\n.item omega\n\n.item one\n.item two\n\n.width 20\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := compose(doc, typo{ps: 9, psMono: 9, lineH: 11})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 5 {
		t.Fatalf("blocks = %d, want 5", len(blocks))
	}
	endsHalf := func(fb fblock) bool {
		s := fb.segs[len(fb.segs)-1]
		return s.lines[len(s.lines)-1].role == roleHalf
	}
	for i, want := range []bool{true, true, false, false, false} {
		if got := endsHalf(blocks[i]); got != want {
			t.Errorf("block %d trailing half spacer = %v, want %v", i, got, want)
		}
	}
	if blocks[1].segs[0].height() != 2 || blocks[0].height() != 3 {
		t.Errorf("unexpected heights: first item %d units, wrapped item first seg %d",
			blocks[0].height(), blocks[1].segs[0].height())
	}
}

func TestCompose_HeadingRoles(t *testing.T) {
	// "#" composes at the display role (4 units), "##" at the
	// heading role (3 units), in both font modes.
	src := "T\n\n# Section\n\nprose\n\n## Subsection\n\nmore\n\n.width 30\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []typo{
		{ps: 9, psMono: 9, lineH: 11},
		{sans: true, ps: 9, psMono: 9, lineH: 11, units: 30 * pdf.AvgAdvance(pdf.Sans)},
	} {
		blocks, err := compose(doc, typ)
		if err != nil {
			t.Fatal(err)
		}
		sec, sub := blocks[0].segs[0], blocks[2].segs[0]
		if r := sec.lines[0].role; r != roleDisplay || sec.height() != 4 {
			t.Errorf("sans=%v: section role=%v height=%d, want display/4", typ.sans, r, sec.height())
		}
		if r := sub.lines[0].role; r != roleHeading || sub.height() != 3 {
			t.Errorf("sans=%v: subsection role=%v height=%d, want heading/3", typ.sans, r, sub.height())
		}
	}
}

func TestSpread_NegativeSlackCompressesGaps(t *testing.T) {
	// A shrunk line (Width > units) must compress its gaps so the
	// drawn line fills units exactly: gap sum = units - word widths.
	m := pdf.Measure(pdf.Sans)
	words := []string{"one", "two", "three"}
	wsum := 0
	for _, w := range words {
		wsum += m.Width(w)
	}
	ln := pica.Line{Words: words, Width: wsum + 2*m.Space()}
	units := ln.Width - 100 // wrap width 100 units narrower than natural
	gaps := spread(ln, units, m, false)
	total := wsum
	for _, g := range gaps {
		total += g
	}
	if total != units {
		t.Errorf("compressed line fills %d, want %d (gaps %v)", total, units, gaps)
	}
	for _, g := range gaps {
		if g >= m.Space() {
			t.Errorf("gap %d not compressed below natural %d", g, m.Space())
		}
	}
	// Final lines stay at natural spacing regardless of slack.
	for _, g := range spread(ln, units, m, true) {
		if g != m.Space() {
			t.Errorf("final line gap %d, want natural %d", g, m.Space())
		}
	}
}

func TestSpread_DashFinalLineHangsHyphen(t *testing.T) {
	// A justified line ending in "-" targets units plus the hyphen
	// hang: the drawn line runs hang units past the wrap width, so
	// the hyphen protrudes into the margin.
	m := pdf.Measure(pdf.Sans)
	words := []string{"aaa", "bbb", "ccc-"}
	wsum := 0
	for _, w := range words {
		wsum += m.Width(w)
	}
	ln := pica.Line{Words: words, Width: wsum + 2*m.Space()}
	units := ln.Width // pretend the line naturally fills the width
	hang := pica.HangHyphen(m)
	if hang <= 0 {
		t.Fatal("sans hyphen hang should be positive")
	}
	gaps := spread(ln, units, m, false)
	total := wsum
	for _, g := range gaps {
		total += g
	}
	if total != units+hang {
		t.Errorf("dash-final line fills %d, want %d (units %d + hang %d)",
			total, units+hang, units, hang)
	}
	// The same line as a paragraph's last line stays natural.
	for _, g := range spread(ln, units, m, true) {
		if g != m.Space() {
			t.Errorf("final line gap %d, want natural %d", g, m.Space())
		}
	}
}

// TestDeriveTypo_FloorGuardsMonoSize: in a sans document the mono
// size (tables, verbatim) is the smaller of the two derived sizes;
// the readability floor must catch it even when the body size passes.
func TestDeriveTypo_FloorGuardsMonoSize(t *testing.T) {
	doc, err := pica.Parse("T\n\nbody\n\n.width 40\n.font sans\n")
	if err != nil {
		t.Fatal(err)
	}
	// A column where the mono size lands just under the floor.
	colW := emWidth * 40 * (minPs - 0.1)
	sansPs := colW * 1000 / float64(40*pdf.AvgAdvance(pdf.Sans))
	if sansPs < minPs {
		t.Fatalf("premise: sans body size %.2f must clear the floor %.1f", sansPs, minPs)
	}
	if _, err := deriveTypo(doc, colW); err == nil {
		t.Fatal("expected floor error for the mono size")
	}
	doc.Layout.Font = "mono"
	if _, err := deriveTypo(doc, colW); err == nil {
		t.Fatal("expected floor error in mono mode too")
	}
}

// TestCompose_HeadingWraps: a heading longer than its shrunken
// measure wraps in both modes (mono used to truncate silently).

// TestCompose_HeadingWraps: a heading longer than its shrunken
// measure wraps in both modes (mono used to truncate silently).
func TestCompose_HeadingWraps(t *testing.T) {
	long := "A very long section heading that cannot fit on one line"
	doc, err := pica.Parse("T\n\n# " + long + "\n\nprose\n\n.width 30\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []typo{
		{ps: 9, psMono: 9, lineH: 11},
		{sans: true, ps: 9, psMono: 9, lineH: 11, units: 30 * pdf.AvgAdvance(pdf.Sans)},
	} {
		blocks, err := compose(doc, typ)
		if err != nil {
			t.Fatal(err)
		}
		h := blocks[0]
		if len(h.segs) < 2 {
			t.Fatalf("sans=%v: heading composed as %d line(s), want a wrap", typ.sans, len(h.segs))
		}
		var words []string
		for _, sg := range h.segs {
			ln := sg.lines[0]
			if ln.role != roleDisplay || ln.style != styleBold {
				t.Errorf("sans=%v: wrapped heading line lost role/style", typ.sans)
			}
			if typ.sans {
				words = append(words, ln.words...)
			} else {
				if n := len([]rune(ln.text)); n > 30*2/3 {
					t.Errorf("mono heading line %q is %d runes, budget %d", ln.text, n, 30*2/3)
				}
				words = append(words, strings.Fields(ln.text)...)
			}
		}
		if got := strings.Join(words, " "); got != long {
			t.Errorf("sans=%v: heading text %q, want %q (nothing truncated)", typ.sans, got, long)
		}
	}
}
