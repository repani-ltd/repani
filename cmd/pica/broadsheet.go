// The pdf subcommand: render a typeset source document as an
// N-column newspaper. Geometry comes entirely from the document's
// layout trailer (self-contained: same source, same PDF bytes); the
// body point size is derived from column width and .width.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/pavlos/typeset"
	"github.com/pavlos/typeset/pdf"
)

// Writer identity constants (points). These are the gazette's
// typography, not document attributes.
const (
	sheetMargin = 40.0
	sheetGutter = 20.0
	lineSpacing = 1.25 // line height in ems
	minPs       = 4.5  // readability floor for the derived body size
	mastRuleGap = 4.0  // extra white on each side of the masthead rule
)

// emWidth is the body font's advance per rune in ems -- a metric of
// the embedded font, not a style choice.
var emWidth = pdf.EmWidth(pdf.Regular)

// minKeep is the orphan/widow threshold: a split never leaves fewer
// than minKeep segments of a block on either side of a column break.
const minKeep = 2

// bullet is the .item marker, matching the text writer's (U+2022,
// covered by all four embedded faces).
const bullet = "•"

func pdfCmd(args []string) int {
	fs := flag.NewFlagSet("pdf", flag.ExitOnError)
	out := fs.String("o", "", "output file (default stdout)")
	pos := parseMixed(fs, args)
	if len(pos) > 1 {
		fmt.Fprintln(os.Stderr, "pica pdf: at most one input file (default stdin)")
		return 1
	}

	src, err := readInput(pos)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica pdf: %v\n", err)
		return 1
	}
	doc, err := typeset.Parse(string(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica pdf: %v\n", err)
		return 1
	}
	bytes, err := broadsheet(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica pdf: %v\n", err)
		return 1
	}
	return writeOutput(*out, bytes)
}

// ── Styled lines and flow blocks ────────────────────────────────────

type style byte

const (
	styleBody style = iota
	styleBold       // headings
	styleGray       // link metadata
	styleRule       // drawn as a hairline, occupies one line slot
)

// sline is one composed output line with its drawing style. Mono
// lines carry pre-padded text (indents baked in as spaces);
// proportional lines carry words plus the inter-word advances
// (thousandths of an em) that justify them, and indent shifts their
// start (quote insets, item hangs, attrib right-alignment). In a
// sans document a non-empty text field only ever holds verbatim or
// table content, which stays monospace by design.
type sline struct {
	text   string
	words  []string // proportional: words drawn with gaps
	gaps   []int    // len(words)-1 advances between them
	indent int      // proportional: leading offset in em-thousandths
	style  style
	href   string // non-empty: the line is a clickable link target
	half   bool   // table note line: half size on half the leading
}

// typo is the writer's resolved typography for one document: body
// size, the size at which .width monospace runes fill the column,
// leading, and (proportional mode) the wrap width in thousandths
// of an em.
type typo struct {
	sans   bool
	ps     float64 // body point size
	psMono float64 // pre/table point size; equals ps in mono mode
	lineH  float64
	units  int // sans: wrap width in thousandths of an em
}

// spread returns the inter-word advances for one composed line in
// thousandths of an em: natural spaces on ragged and final lines;
// on justified lines the slack is distributed evenly, leftmost
// gaps taking the remainder, so the line fills the wrap width
// exactly -- in integers, keeping the PDF deterministic. Negative
// slack (the breaker's shrink allowance) compresses gaps the same
// way. A dash-final justified line targets units plus the hyphen
// hang, mirroring the breaker, so the hyphen protrudes into the
// margin and the flush edge stays optically straight.
func spread(ln typeset.Line, units int, m pdf.Measurer, last bool) []int {
	k := len(ln.Words) - 1
	if k <= 0 {
		return nil
	}
	gaps := make([]int, k)
	sp := m.Space()
	for i := range gaps {
		gaps[i] = sp
	}
	slack := units - ln.Width
	if !last && strings.HasSuffix(ln.Words[k], "-") {
		slack += typeset.HangHyphen(m)
	}
	if last || slack == 0 {
		return gaps
	}
	if slack < 0 {
		neg := -slack
		base, extra := neg/k, neg%k
		for i := range gaps {
			gaps[i] -= base
			if i < extra {
				gaps[i]--
			}
		}
		return gaps
	}
	base, extra := slack/k, slack%k
	for i := range gaps {
		gaps[i] += base
		if i < extra {
			gaps[i]++
		}
	}
	return gaps
}

// seg is an atomic run of lines: a paragraph line, a table row (all
// its wrapped lines plus its note lines), a whole .pre block, ...
type seg struct {
	lines []sline
}

// height is in half-line units: a body line is 2, a note line 1.
// The half-line is the flow grid's quantum (DESIGN.md §6); blocks
// snap back to whole body lines at placement, so only table rows
// with notes ever produce odd heights.
func (s seg) height() int {
	h := 0
	for _, ln := range s.lines {
		h += lineUnits(ln)
	}
	return h
}

func lineUnits(ln sline) int {
	if ln.half {
		return 1
	}
	return 2
}

// fblock is a flowable block: segments that may be split between
// (never inside), with optional repeated lead-in after a split.
type fblock struct {
	segs     []seg
	repeat   int  // leading segments repeated after a split (table header, .pre N)
	atomic   bool // never split unless taller than a whole column
	keepNext bool // heading: keep with the following block
	tight    bool // no blank separator before this block
}

func (b fblock) height() int {
	h := 0
	for _, s := range b.segs {
		h += s.height()
	}
	return h
}

// compose renders the document's blocks into flow blocks at the
// document width. This is where writer identity applies: justified
// paragraphs, bold headings without their marker, gray links.
// Proportional (sans) documents compose prose as measured word
// lines; verbatim blocks and tables keep monospace layout in both
// modes.
func compose(doc *typeset.Doc, t typo) ([]fblock, error) {
	width := doc.Layout.Width
	lang := doc.Layout.Lang
	var out []fblock
	for _, blk := range doc.Blocks {
		fb := fblock{tight: blk.Tight}
		switch blk.Kind {
		case typeset.Para:
			if t.sans {
				m := pdf.Measure(pdf.Sans)
				lines := typeset.JustifyLines(blk.Text, t.units, m, lang)
				for i, ln := range lines {
					last := i == len(lines)-1
					sl := sline{words: ln.Words, gaps: spread(ln, t.units, m, last)}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
			} else {
				for _, ln := range typeset.JustifyParagraph(blk.Text, width, lang) {
					fb.segs = append(fb.segs, seg{lines: []sline{{text: ln}}})
				}
			}

		case typeset.Quote:
			// Inset two spaces on both sides; the attribution line is
			// right-aligned to the quote's right margin. Mirrors the
			// text writer's geometry (see typeset doc.go).
			if t.sans {
				m := pdf.Measure(pdf.Sans)
				qi := 2 * m.Space()
				measure := t.units - 2*qi
				lines := typeset.JustifyLines(blk.Text, measure, m, lang)
				for i, ln := range lines {
					last := i == len(lines)-1
					sl := sline{words: ln.Words, gaps: spread(ln, measure, m, last), indent: qi}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
				if blk.Attrib != "" {
					ln := lineFor(strings.Fields("-- "+blk.Attrib), m)
					sl := sline{words: ln.Words, gaps: spread(ln, measure, m, true),
						indent: qi + max(0, measure-ln.Width)}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
			} else {
				for _, ln := range typeset.JustifyParagraph(blk.Text, width-4, lang) {
					fb.segs = append(fb.segs, seg{lines: []sline{{text: "  " + ln}}})
				}
				if blk.Attrib != "" {
					s := trunc("-- "+blk.Attrib, width-4)
					s = strings.Repeat(" ", width-2-len([]rune(s))) + s
					fb.segs = append(fb.segs, seg{lines: []sline{{text: s}}})
				}
			}

		case typeset.Item:
			// A bullet with a hanging indent for continuation lines.
			if t.sans {
				m := pdf.Measure(pdf.Sans)
				ii := m.Width(bullet) + m.Space()
				measure := t.units - ii
				lines := typeset.JustifyLines(blk.Text, measure, m, lang)
				for i, ln := range lines {
					last := i == len(lines)-1
					sl := sline{words: ln.Words, gaps: spread(ln, measure, m, last), indent: ii}
					if i == 0 {
						sl.words = append([]string{bullet}, ln.Words...)
						sl.gaps = append([]int{m.Space()}, sl.gaps...)
						sl.indent = 0
					}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
			} else {
				for i, ln := range typeset.JustifyParagraph(blk.Text, width-2, lang) {
					pre := "  "
					if i == 0 {
						pre = bullet + " "
					}
					fb.segs = append(fb.segs, seg{lines: []sline{{text: pre + ln}}})
				}
			}

		case typeset.Heading:
			if t.sans {
				m := pdf.Measure(pdf.SansBold)
				for _, ln := range typeset.WrapLines(blk.Text, t.units, m, lang) {
					sl := sline{words: ln.Words, gaps: spread(ln, t.units, m, true), style: styleBold}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
			} else {
				fb.segs = []seg{{lines: []sline{{text: trunc(blk.Text, width), style: styleBold}}}}
			}
			fb.keepNext = true // flow guards the no-next-block case
			fb.atomic = true

		case typeset.RuleBlk:
			fb.segs = []seg{{lines: []sline{{style: styleRule}}}}
			fb.atomic = true

		case typeset.LinkBlk:
			url, title, _ := strings.Cut(blk.Text, " ")
			label := title
			if label == "" {
				label = url
			}
			if t.sans {
				m := pdf.Measure(pdf.Sans)
				for m.Width(label) > t.units && len([]rune(label)) > 1 {
					r := []rune(label)
					label = string(r[:len(r)-1])
				}
				fb.segs = []seg{{lines: []sline{{words: []string{label}, style: styleGray, href: url}}}}
			} else {
				fb.segs = []seg{{lines: []sline{{text: trunc(label, width), style: styleGray, href: url}}}}
			}
			fb.atomic = true

		case typeset.TableBlk:
			tl, err := blk.Table.Layout(blk.TableWidth(width))
			if err != nil {
				return nil, err
			}
			if len(tl.Header) > 0 {
				lines := toSlines(tl.Header)
				lines = append(lines, halfSlines(tl.HeaderNotes)...)
				lines = append(lines, sline{text: tl.Sep})
				fb.segs = append(fb.segs, seg{lines: lines})
				fb.repeat = 1
			}
			for j, row := range tl.Rows {
				lines := toSlines(row)
				lines = append(lines, halfSlines(tl.RowNotes[j])...)
				fb.segs = append(fb.segs, seg{lines: lines})
			}

		case typeset.Pre:
			lines := make([]sline, len(blk.Lines))
			for j, ln := range blk.Lines {
				lines[j] = sline{text: trunc(ln, width)}
			}
			if blk.Repeat > 0 && blk.Repeat < len(lines) {
				// Repeated lead-in becomes its own segment; the rest
				// split line-wise.
				fb.segs = append(fb.segs, seg{lines: lines[:blk.Repeat]})
				fb.repeat = 1
				for _, ln := range lines[blk.Repeat:] {
					fb.segs = append(fb.segs, seg{lines: []sline{ln}})
				}
			} else {
				fb.segs = []seg{{lines: lines}}
				fb.atomic = true
			}
		}
		if len(fb.segs) > 0 {
			out = append(out, fb)
		}
	}
	return out, nil
}

func toSlines(lines []string) []sline {
	out := make([]sline, len(lines))
	for i, ln := range lines {
		out[i] = sline{text: ln}
	}
	return out
}

func halfSlines(lines []string) []sline {
	out := make([]sline, len(lines))
	for i, ln := range lines {
		out[i] = sline{text: ln, half: true}
	}
	return out
}

// lineFor assembles a typeset.Line from words at natural spacing
// under the measurer (the attribution line is never justified).
func lineFor(words []string, m pdf.Measurer) typeset.Line {
	w := 0
	for i, s := range words {
		if i > 0 {
			w += m.Space()
		}
		w += m.Width(s)
	}
	return typeset.Line{Words: words, Width: w}
}

// ── Column flow ─────────────────────────────────────────────────────

// flow distributes blocks into columns of capacity(i) lines each.
// Splits happen only between segments, leaving at least minKeep
// segments on both sides; the repeated lead-in (table headers,
// .pre N) is re-emitted after each split. Atomic blocks move whole
// unless taller than an entire fresh column. A heading is never
// left at a column bottom without minKeep segments of what follows.
//
// Capacity is in body lines; internal accounting is in half-line
// units (a body line is 2, a table note line 1). Every block starts
// on a whole body line: placement pads an odd column height with a
// blank half-line first, so half-lines stay confined inside the
// block that made them and the cross-column baseline grid holds.
func flow(blocks []fblock, capacity func(int) int) [][]sline {
	var out [][]sline
	var cur []sline
	curH := 0 // height of cur in half-line units
	colIdx := 0

	closeCol := func() {
		out = append(out, cur)
		cur = nil
		curH = 0
		colIdx++
	}
	place := func(b fblock, upto int) {
		if curH%2 != 0 {
			cur = append(cur, sline{half: true})
			curH++
		}
		if len(cur) > 0 && !b.tight {
			cur = append(cur, sline{})
			curH += 2
		}
		for _, s := range b.segs[:upto] {
			cur = append(cur, s.lines...)
			curH += s.height()
		}
	}

	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		for {
			colCap := 2 * capacity(colIdx)
			sep := 0
			if len(cur) > 0 && !b.tight {
				sep = 2
			}
			snap := curH % 2
			avail := colCap - curH - snap - sep
			h := b.height()

			// Keep-with-next: the heading and the first minKeep
			// segments of the next block must fit together.
			if b.keepNext && i+1 < len(blocks) && len(cur) > 0 {
				next := blocks[i+1]
				need := h + 2
				for _, s := range next.segs[:min(minKeep, len(next.segs))] {
					need += s.height()
				}
				if avail < need {
					closeCol()
					continue
				}
			}

			if h <= avail {
				place(b, len(b.segs))
				break
			}

			k := splitSegs(b, avail)
			if k <= 0 {
				if len(cur) > 0 {
					closeCol()
					continue
				}
				// Top of an empty column and still no acceptable
				// split: force progress.
				k = forceSplit(b, avail)
			}
			place(b, k)
			b = b.rest(k)
			b.tight = false
			closeCol()
			if len(b.segs) == 0 {
				break
			}
		}
	}
	if len(cur) > 0 || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

// fitSegs returns how many leading segments of b fit in avail
// half-line units.
func fitSegs(b fblock, avail int) int {
	k, units := 0, 0
	for k < len(b.segs) && units+b.segs[k].height() <= avail {
		units += b.segs[k].height()
		k++
	}
	return k
}

// splitSegs returns how many leading segments of b fit in avail
// half-line units under the orphan/widow rules, or 0 for "move
// whole".
func splitSegs(b fblock, avail int) int {
	if b.atomic {
		return 0
	}
	n := len(b.segs)
	// The first part must keep the repeated lead-in plus minKeep
	// content segments; the remainder keeps minKeep content segments.
	k := min(fitSegs(b, avail), n-minKeep)
	if k < b.repeat+minKeep {
		return 0
	}
	return k
}

// forceSplit fits as many segments as possible into an empty column,
// ignoring the keep rules. It always makes progress: at least one
// segment beyond the repeated lead-in goes down (otherwise rest()
// would reconstruct the identical block and flow would loop), and an
// atomic block taller than the column places whole, overflowing.
func forceSplit(b fblock, avail int) int {
	if b.atomic || len(b.segs) == 1 {
		return len(b.segs)
	}
	k := max(fitSegs(b, avail), b.repeat+1)
	return min(k, len(b.segs))
}

// rest returns the unplaced remainder after splitting at k, with the
// repeated lead-in re-attached. A block consumed to its end has no
// remainder (re-attaching the lead-in alone would loop forever).
func (b fblock) rest(k int) fblock {
	if k >= len(b.segs) {
		return fblock{}
	}
	segs := append(append([]seg{}, b.segs[:b.repeat]...), b.segs[k:]...)
	return fblock{segs: segs, repeat: b.repeat}
}

// ── Drawing ─────────────────────────────────────────────────────────

// paperSize maps the document attribute to a pdf.PageSize. Parse
// validates the vocabulary; a value it does not know reaching here
// is a bug, not a fallback case.
func paperSize(paper string) pdf.PageSize {
	switch paper {
	case "a4":
		return pdf.PageA4
	case "a5":
		return pdf.PageA5
	case "letter":
		return pdf.PageLetter
	default:
		panic(fmt.Sprintf("pica: paper %q escaped Parse validation", paper))
	}
}

// broadsheet renders a parsed document as the newspaper PDF.
func broadsheet(doc *typeset.Doc) ([]byte, error) {
	ncols := doc.Layout.Cols
	width := doc.Layout.Width
	size := paperSize(doc.Layout.Paper)
	pageW, pageH := size.Dimensions()
	usableW := pageW - 2*sheetMargin
	colW := (usableW - float64(ncols-1)*sheetGutter) / float64(ncols)

	// Derived body size: the column holds exactly .width characters
	// -- runes for the mono face, average lowercase advances for the
	// sans face, so .width means the same visual density in both.
	sans := doc.Layout.Font == "sans"
	psMono := colW / (emWidth * float64(width))
	ps := psMono
	units := 0
	if sans {
		units = width * pdf.AvgAdvance(pdf.Sans)
		ps = colW * 1000 / float64(units)
	}
	if ps < minPs {
		return nil, fmt.Errorf(
			"derived body size %.1fpt is below %.1fpt: .width %d with .cols %d on %s leaves columns too narrow",
			ps, minPs, width, ncols, doc.Layout.Paper)
	}
	lineH := ps * lineSpacing
	t := typo{sans: sans, ps: ps, psMono: psMono, lineH: lineH, units: units}

	// Masthead band on page one. The floor of 8 average characters
	// keeps short titles from ballooning. A byline (.by/.date) adds
	// a centered dateline row under the masthead.
	topY := pageH - sheetMargin
	title := doc.Title
	mastFont, bodyFont := pdf.Bold, pdf.Regular
	if sans {
		mastFont, bodyFont = pdf.SansBold, pdf.Sans
	}
	floor1 := 8 * float64(pdf.AvgAdvance(mastFont)) / 1000
	mastPt := usableW / max(pdf.Width(title, mastFont, 1), floor1)
	mastPt = max(12, min(30, mastPt))
	byline := doc.Byline()
	mastBottom := topY - mastPt*1.35
	headerBottom := mastBottom
	if byline != "" {
		headerBottom -= ps * 1.5
	}
	ruleY := headerBottom - mastRuleGap
	colTopFirst := ruleY - mastRuleGap - lineH*0.6
	colTopRest := topY
	colBottom := sheetMargin

	linesFirst := int((colTopFirst - colBottom) / lineH)
	linesRest := int((colTopRest - colBottom) / lineH)
	if linesFirst < 2*minKeep+2 {
		return nil, fmt.Errorf("page too small for %d columns at %.1fpt", ncols, ps)
	}

	blocks, err := compose(doc, t)
	if err != nil {
		return nil, err
	}
	capacity := func(col int) int {
		if col < ncols {
			return linesFirst
		}
		return linesRest
	}
	columns := flow(blocks, capacity)

	// Balance a single underfull page: the smallest uniform capacity
	// that still fits ncols columns.
	if len(columns) <= ncols {
		lo, hi, best := 2*minKeep+2, linesFirst, linesFirst
		for lo <= hi {
			mid := (lo + hi) / 2
			if c := flow(blocks, func(int) int { return mid }); len(c) <= ncols {
				best, hi = mid, mid-1
			} else {
				lo = mid + 1
			}
		}
		columns = flow(blocks, func(int) int { return best })
	}

	// flow always returns at least one column, so the loop runs at
	// least once.
	pdoc := &pdf.Doc{Title: title, Creator: "pica", PageSize: size, Compress: true}
	for pg := 0; pg*ncols < len(columns); pg++ {
		var p pdf.Page
		colTop := colTopRest
		if pg == 0 {
			colTop = colTopFirst
			p.SetFont(mastFont, mastPt)
			w := pdf.Width(title, mastFont, mastPt)
			p.Text((pageW-w)/2, topY-mastPt, title)
			if byline != "" {
				p.SetFont(bodyFont, ps)
				p.Gray(0.4)
				p.Text((pageW-pdf.Width(byline, bodyFont, ps))/2, mastBottom-ps, byline)
				p.Gray(0)
			}
			p.StrokeGray(0)
			p.Line(sheetMargin, ruleY, pageW-sheetMargin, ruleY, 1.0)
		}

		deepest := 0 // column depth in half-line units
		for c := 0; c < ncols; c++ {
			idx := pg*ncols + c
			if idx >= len(columns) {
				break
			}
			units := 0
			for _, ln := range columns[idx] {
				units += lineUnits(ln)
			}
			deepest = max(deepest, units)
			x := sheetMargin + float64(c)*(colW+sheetGutter)
			drawColumn(&p, columns[idx], x, colTop, colW, t)
		}

		// Hairline rules centered in the gutters, to content depth.
		ruleBottom := max(colTop-float64(deepest)*lineH/2, colBottom)
		p.StrokeGray(0.55)
		for c := 1; c < ncols; c++ {
			x := sheetMargin + float64(c)*(colW+sheetGutter) - sheetGutter/2
			p.Line(x, colTop, x, ruleBottom, 0.4)
		}

		// Page number, centered in the bottom margin.
		p.SetFont(bodyFont, ps*0.9)
		p.Gray(0.4)
		num := fmt.Sprintf("- %d -", pg+1)
		p.Text((pageW-pdf.Width(num, bodyFont, ps*0.9))/2, sheetMargin/2-2, num)
		p.Gray(0)

		pdoc.Add(&p)
	}
	return pdoc.Bytes(), nil
}

// drawColumn renders one column's styled lines. Proportional lines
// (words set) draw in the sans faces at the body size; text lines
// are monospace -- everything in a mono document, and verbatim or
// table content in a sans one, drawn at the size where .width runes
// fill the column.
func drawColumn(p *pdf.Page, lines []sline, x, top, colW float64, t typo) {
	y := top - t.ps
	for i, ln := range lines {
		// Leading precedes a line: each baseline sits its own slot
		// below the previous one, so a half line advances lineH/2.
		if i > 0 {
			if ln.half {
				y -= t.lineH / 2
			} else {
				y -= t.lineH
			}
		}
		switch {
		case ln.style == styleRule:
			p.StrokeGray(0.3)
			p.Line(x, y+t.ps*0.35, x+colW, y+t.ps*0.35, 0.5)

		case len(ln.words) > 0:
			font := pdf.Sans
			if ln.style == styleBold {
				font = pdf.SansBold
			}
			if ln.style == styleGray {
				p.Gray(0.45)
			}
			xw := x + float64(ln.indent)*t.ps/1000
			p.SetFont(font, t.ps)
			p.Words(xw, y, ln.words, ln.gaps)
			if ln.style == styleGray {
				p.Gray(0)
			}
			if ln.href != "" {
				w := lineWidthPt(ln, font, t.ps)
				p.Link(xw, y-t.ps*0.25, xw+w, y+t.ps, ln.href)
			}

		case ln.text != "":
			font := pdf.Regular
			if ln.style == styleBold {
				font = pdf.Bold
			}
			ps := t.psMono
			if ln.half {
				// Table note line: half size on half the leading,
				// formatted on the doubled rune grid so column
				// offsets land under their full-size columns.
				ps = t.psMono / 2
			}
			if ln.style == styleGray {
				p.Gray(0.45)
			}
			p.SetFont(font, ps)
			p.Text(x, y, ln.text)
			if ln.style == styleGray {
				p.Gray(0)
			}
			if ln.href != "" {
				w := pdf.Width(ln.text, font, ps)
				p.Link(x, y-ps*0.25, x+w, y+ps, ln.href)
			}
		}
	}
}

// lineWidthPt is the drawn width of a proportional line in points.
func lineWidthPt(ln sline, font pdf.Font, ps float64) float64 {
	m := pdf.Measure(font)
	u := 0
	for _, w := range ln.words {
		u += m.Width(w)
	}
	for _, g := range ln.gaps {
		u += g
	}
	return float64(u) * ps / 1000
}
