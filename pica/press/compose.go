// Composition: parsed blocks become styled lines (slines) grouped
// into flowable blocks (fblocks). The writer-identity layer shared
// by the default and report presentations.

package press

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"repani.com/pica"
	"repani.com/pica/pdf"
	"repani.com/typeset/format"
	"repani.com/typeset/wrap"
)

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
	href   string   // non-empty: the line is a clickable link target
	role   sizeRole // size role: body, half, heading, display
	// ruleSegs: styleRule drawn as one hairline per column interval
	// (table rules); empty draws the full column width.
	ruleSegs []pica.Span
	nums     []numSpan
	prose    []proseSpan
	// Emphasis (doc.go, Emphasis). emph: proportional lines,
	// parallel to words, true for words drawn in the italic face.
	// uline: monospace prose lines, the rune intervals of the
	// typescript underline -- the marker underscores are blanked in
	// text and the rule occupies their cells, so the grid, and the
	// text-page identity, never move.
	emph  []bool
	uline []pica.Span
	// lead: a .term label run in at the line's left in the bold
	// face. Monospace: text begins with the lead's runes (drawn
	// bold, the rest regular on the same grid). Proportional: the
	// words start at indent, which the composer sets past the
	// lead; a lead with no words is a label standing alone.
	lead string
}

// proseSpan is one measured line of a table cell set in the body
// face (a P prose cell, or a header label) in a sans document: the
// mono grid reserves the cell's space blank, and the line draws at
// off — em-thousandths from the column origin at the drawing size,
// so spans can also right-align or center within their column. The
// grid stays mono; only declared prose and labels lift off it.
type proseSpan struct {
	off   int // em-thousandths from the table's left edge
	words []string
	gaps  []int
}

// sizeRole is the closed set of sizes, quantized to the half-line
// grid (DESIGN.t §6, §7): every role is a slot of whole half-units
// and a glyph scale, so flow stays integer arithmetic and the
// cross-column baseline grid snaps back at block boundaries.
type sizeRole uint8

const (
	roleBody    sizeRole = iota // 2 units, 1x — prose, table rows
	roleHalf                    // 1 unit, 0.5x — table note lines
	roleHeading                 // 3 units, 1.2x — "##" subsections
	roleDisplay                 // 4 units, 1.5x — "#" sections
)

// roleUnits is the slot height in half-line units.
func roleUnits(r sizeRole) int {
	switch r {
	case roleHalf:
		return 1
	case roleHeading:
		return 3
	case roleDisplay:
		return 4
	}
	return 2
}

// roleScale is the glyph scale relative to the body size.
func roleScale(r sizeRole) float64 {
	switch r {
	case roleHalf:
		return 0.5
	case roleHeading:
		return 1.2
	case roleDisplay:
		return 1.5
	}
	return 1
}

// numSpan is one numeric table cell re-rendered in the sans face
// with tabular figures (sans documents only): the mono line keeps
// the cell blanked, and the number draws anchored at the column's
// decimal position on the mono grid — integer part ending there,
// fraction tail starting there — so alignment across rows is exact
// by construction whatever the sans glyph widths are.
type numSpan struct {
	sep     int // rune index of the column's decimal cell
	intPart string
	tail    string
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

// deriveTypo resolves a presentation's typography from the document
// layout and its column width: the measure holds exactly .width
// characters — runes for the mono face, average lowercase advances
// for the sans face, so .width means the same visual density in
// both. This is THE size contract; every presentation derives
// through it.
func deriveTypo(doc *pica.Doc, colW float64) (typo, error) {
	width := doc.Layout.Width
	sans := doc.Layout.Font == "sans"
	psMono := colW / (emWidth * float64(width))
	ps := psMono
	units := 0
	if sans {
		units = width * pdf.AvgAdvance(pdf.Sans)
		ps = colW * 1000 / float64(units)
	}
	// The floor guards the smaller of the two sizes: in sans mode
	// the mono size (tables, verbatim) is the smaller one, since
	// the average sans advance is narrower than the mono cell.
	if small := min(ps, psMono); small < minPs {
		return typo{}, fmt.Errorf(
			"derived body size %.1fpt is below %.1fpt: .width %d on %s leaves the measure too narrow",
			small, minPs, width, doc.Layout.Paper)
	}
	return typo{
		sans: sans, ps: ps, psMono: psMono,
		lineH: ps * leadingFor(width), units: units,
	}, nil
}

// leadingFor derives the leading from the measure — the classic
// rule that longer lines need more air between them: newspaper
// leading up to 50 characters per line, book leading beyond. Like
// the margin, it is writer-owned geometry computed from the
// declared layout, never a knob.
func leadingFor(width int) float64 {
	if width <= 50 {
		return lineSpacing
	}
	return lineSpacingWide
}

// spread returns the inter-word advances for one composed line in
// thousandths of an em: wrap.Gaps, which spreads a justified line's
// slack as the breaker measured it.
func spread(ln pica.Line, units int, m pdf.Measurer, last bool) []int {
	return wrap.Gaps(ln, units, m, last)
}

// seg is an atomic run of lines: a paragraph line, a table row (all
// its wrapped lines plus its note lines), a whole .pre block, ...
type seg struct {
	lines []sline
}

// height is in half-line units: a body line is 2, a note line 1.
// The half-line is the flow grid's quantum (DESIGN.t §6); blocks
// snap back to whole body lines at placement, so only table rows
// with notes ever produce odd heights.
func (s seg) height() int {
	h := 0
	for _, ln := range s.lines {
		h += roleUnits(ln.role)
	}
	return h
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
func compose(doc *pica.Doc, t typo) ([]fblock, error) {
	width := doc.Layout.Width
	var out []fblock
	for bi := 0; bi < len(doc.Blocks); bi++ {
		blk := doc.Blocks[bi]
		if blk.Kind == pica.Item || blk.Kind == pica.Term {
			run := []pica.Block{blk}
			for bi+1 < len(doc.Blocks) && doc.Blocks[bi+1].Kind == blk.Kind && doc.Blocks[bi+1].Tight {
				bi++
				run = append(run, doc.Blocks[bi])
			}
			out = append(out, composeItems(run, t, width)...)
			continue
		}
		fb := fblock{tight: blk.Tight}
		switch blk.Kind {
		case pica.Para:
			if t.sans {
				m := pdf.Measure(pdf.Sans)
				lines := pica.JustifyLines(blk.Text, t.units, t.units, m, pdf.Measure(pdf.SansItalic))
				for i, ln := range lines {
					last := i == len(lines)-1
					sl := sline{words: ln.Words, emph: ln.Emph, gaps: spread(ln, t.units, m, last)}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
			} else {
				fb.segs = monoProse(blk.Text, pica.JustifyText(blk.Text, width, width), noPrefix)
			}

		case pica.Quote:
			// Inset pica.QuoteIndent spaces on both sides; the
			// attribution line is right-aligned to the quote's right
			// margin. Mirrors the text writer's geometry.
			if t.sans {
				m := pdf.Measure(pdf.Sans)
				qi := pica.QuoteIndent * m.Space()
				measure := t.units - 2*qi
				lines := pica.JustifyLines(blk.Text, measure, measure, m, pdf.Measure(pdf.SansItalic))
				for i, ln := range lines {
					last := i == len(lines)-1
					sl := sline{words: ln.Words, emph: ln.Emph, gaps: spread(ln, measure, m, last), indent: qi}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
				if blk.Attrib != "" {
					ln := wrap.LineOf(strings.Fields("-- "+blk.Attrib), m)
					sl := sline{words: ln.Words, gaps: spread(ln, measure, m, true),
						indent: qi + max(0, measure-ln.Width)}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
			} else {
				inset := strings.Repeat(" ", pica.QuoteIndent)
				lines := pica.JustifyText(blk.Text, width-2*pica.QuoteIndent, width-2*pica.QuoteIndent)
				fb.segs = monoProse(blk.Text, lines, func(int) string { return inset })
				if blk.Attrib != "" {
					fb.segs = append(fb.segs, seg{lines: []sline{{text: pica.AttribLine(blk.Attrib, width)}}})
				}
			}

		case pica.Heading:
			// "#" sections set at the display role, "##" subsections
			// at the heading role: larger glyphs on taller slots of
			// the same half-line grid, so the hierarchy reads without
			// flow learning anything new. The wrap measure shrinks by
			// the glyph scale (bigger ems, same physical column); a
			// heading longer than its measure wraps in both modes.
			role := roleDisplay
			if blk.Level == 2 {
				role = roleHeading
			}
			shrink := func(measure int) int {
				if role == roleHeading {
					return measure * 5 / 6
				}
				return measure * 2 / 3
			}
			if t.sans {
				measure, m := shrink(t.units), pdf.Measure(pdf.SansBold)
				for _, ln := range pica.WrapLines(blk.Text, measure, measure, m) {
					sl := sline{words: ln.Words, gaps: spread(ln, measure, m, true), style: styleBold, role: role}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
			} else {
				for _, ln := range pica.WrapLines(blk.Text, shrink(width), shrink(width), pica.Mono) {
					sl := sline{text: strings.Join(ln.Words, " "), style: styleBold, role: role}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
			}
			fb.keepNext = true // flow guards the no-next-block case
			fb.atomic = true

		case pica.RuleBlk:
			fb.segs = []seg{{lines: []sline{{style: styleRule}}}}
			fb.atomic = true

		case pica.LinkBlk:
			url, title, _ := strings.Cut(blk.Text, " ")
			label := title
			if label == "" {
				label = url
			}
			if t.sans {
				label = truncMeasured(label, t.units, pdf.Measure(pdf.Sans))
				fb.segs = []seg{{lines: []sline{{words: []string{label}, style: styleGray, href: url}}}}
			} else {
				fb.segs = []seg{{lines: []sline{{text: format.Trunc(label, width), style: styleGray, href: url}}}}
			}
			fb.atomic = true

		case pica.TableBlk:
			// Sans documents measure P (prose) cells with the sans
			// measurer at the column's mono measure (rune width x
			// the mono advance in em-thousandths); mono documents
			// lay P out as L.
			var tl *pica.TableLayout
			var err error
			if t.sans {
				tl, err = blk.Table.LayoutMeasured(width,
					pdf.Measure(pdf.Sans), pdf.Measure(pdf.SansBold), runeUnits)
			} else {
				tl, err = blk.Table.Layout(width)
			}
			if err != nil {
				return nil, err
			}
			// The separator/total rule: one hairline segment per
			// column interval — the PDF form of the text writer's
			// dashed separator row.
			rule := func() sline {
				return sline{style: styleRule, ruleSegs: tl.Cols}
			}
			if h := tl.Header; h != nil {
				lines := toSlines(h.Lines)
				// The header row is the table's labels: bold, and in
				// sans documents set in the body face as positioned
				// spans over blank-reserved grid space.
				for i := range lines {
					lines[i].style = styleBold
				}
				attachProse(lines, h.Prose, pdf.Measure(pdf.SansBold))
				lines = append(lines, halfSlines(h.Notes)...)
				lines = append(lines, rule())
				fb.segs = append(fb.segs, seg{lines: lines})
				fb.repeat = 1
			}
			// Uniform row pitch: when every row note fits a single
			// half-line, every data row carries the half — noted
			// rows use it, plain rows leave it blank air — so the
			// table's vertical rhythm stays even. Variable heights
			// are right only when notes wrap. Total rows sit under
			// their rule and stay unpadded.
			hasNote, fits := false, true
			for _, r := range tl.Rows {
				hasNote = hasNote || len(r.Notes) > 0
				fits = fits && len(r.Notes) <= 1
			}
			even := hasNote && fits
			for _, row := range tl.Rows {
				rowLines := toSlines(row.Lines)
				attachProse(rowLines, row.Prose, pdf.Measure(pdf.Sans))
				if t.sans {
					// Numbers leave the mono grid and set in the sans
					// face with tabular figures, anchored on the point.
					rowLines[0] = liftNums(rowLines[0], row.Nums)
				}
				var lines []sline
				if row.Total {
					for i := range rowLines {
						rowLines[i].style = styleBold
					}
					lines = append(lines, rule())
				}
				lines = append(lines, rowLines...)
				lines = append(lines, halfSlines(row.Notes)...)
				if even && len(row.Notes) == 0 && !row.Total {
					lines = append(lines, sline{role: roleHalf})
				}
				fb.segs = append(fb.segs, seg{lines: lines})
			}

		case pica.Pre:
			lines := make([]sline, len(blk.Lines))
			for j, ln := range blk.Lines {
				lines[j] = sline{text: format.Trunc(ln, width)}
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

// composeItems renders a tight run of items, or of terms (one
// kind per run). When any entry in the run turns over, a half-line
// gap separates the entries — the same
// conditional, quantized policy as the table row pitch: an all
// single-line run stays classically tight, a wrapped run gets air
// so turnovers cannot be misread as sibling items. The spacer joins
// each item's LAST seg, so a split between items lands after it (an
// invisible half-line at a column bottom) and a column can never
// open with a stranded gap. The text writer has no half-line and
// keeps every run tight.
func composeItems(run []pica.Block, t typo, width int) []fblock {
	fbs := make([]fblock, len(run))
	wrapped := false
	for i, blk := range run {
		if blk.Kind == pica.Term {
			fbs[i] = composeTerm(blk, t, width)
		} else {
			fbs[i] = composeItem(blk, t, width)
		}
		if len(fbs[i].segs) > 1 {
			wrapped = true
		}
	}
	if wrapped {
		for i := range fbs[:len(fbs)-1] {
			last := &fbs[i].segs[len(fbs[i].segs)-1]
			last.lines = append(last.lines, sline{role: roleHalf})
		}
	}
	return fbs
}

// composeItem renders one bulleted item: a bullet, then the text
// with a hanging indent for continuation lines.
func composeItem(blk pica.Block, t typo, width int) fblock {
	fb := fblock{tight: blk.Tight}
	if t.sans {
		m := pdf.Measure(pdf.Sans)
		ii := m.Width(pica.Bullet) + m.Space()
		measure := t.units - ii
		lines := pica.JustifyLines(blk.Text, measure, measure, m, pdf.Measure(pdf.SansItalic))
		for i, ln := range lines {
			last := i == len(lines)-1
			sl := sline{words: ln.Words, emph: ln.Emph, gaps: spread(ln, measure, m, last), indent: ii}
			if i == 0 {
				sl.words = append([]string{pica.Bullet}, ln.Words...)
				sl.gaps = append([]int{m.Space()}, sl.gaps...)
				sl.indent = 0
				if sl.emph != nil {
					sl.emph = append([]bool{false}, sl.emph...)
				}
			}
			fb.segs = append(fb.segs, seg{lines: []sline{sl}})
		}
	} else {
		lines := pica.JustifyText(blk.Text, width-pica.ItemIndent, width-pica.ItemIndent)
		fb.segs = append(fb.segs, monoProse(blk.Text, lines, func(i int) string {
			if i == 0 {
				return pica.Bullet + " "
			}
			return strings.Repeat(" ", pica.ItemIndent)
		})...)
	}
	return fb
}

// composeTerm renders one .term: the label run in, set in the bold
// face, then the text, its turnovers hanging pica.ItemIndent; a
// label that leaves the text less than half the measure stands on
// its own line with the text beneath. The mono path follows the
// text writer's geometry cell for cell (pica.TermRunIn,
// pica.TermGap); the sans path applies the same rule in measured
// units, the label measured in the face that draws it.
func composeTerm(blk pica.Block, t typo, width int) fblock {
	fb := fblock{tight: blk.Tight}
	if t.sans {
		m, mb, mi := pdf.Measure(pdf.Sans), pdf.Measure(pdf.SansBold), pdf.Measure(pdf.SansItalic)
		hang := pica.ItemIndent * m.Space()
		measure := t.units - hang
		lead := mb.Width(blk.Label) + pica.TermGap*m.Space()
		first := t.units - lead
		if 2*first < t.units {
			fb.segs = append(fb.segs, seg{lines: []sline{{lead: truncMeasured(blk.Label, t.units, mb)}}})
			lines := pica.JustifyLines(blk.Text, measure, measure, m, mi)
			for i, ln := range lines {
				last := i == len(lines)-1
				sl := sline{words: ln.Words, emph: ln.Emph, gaps: spread(ln, measure, m, last), indent: hang}
				fb.segs = append(fb.segs, seg{lines: []sline{sl}})
			}
			return fb
		}
		lines := pica.JustifyLines(blk.Text, first, measure, m, mi)
		for i, ln := range lines {
			last := i == len(lines)-1
			sl := sline{words: ln.Words, emph: ln.Emph, gaps: spread(ln, measure, m, last), indent: hang}
			if i == 0 {
				sl.lead, sl.indent, sl.gaps = blk.Label, lead, spread(ln, first, m, last)
			}
			fb.segs = append(fb.segs, seg{lines: []sline{sl}})
		}
		return fb
	}
	hang := strings.Repeat(" ", pica.ItemIndent)
	first, runIn := pica.TermRunIn(blk.Label, width)
	if !runIn {
		label := format.Trunc(blk.Label, width)
		fb.segs = append(fb.segs, seg{lines: []sline{{text: label, lead: label}}})
		lines := pica.JustifyText(blk.Text, width-pica.ItemIndent, width-pica.ItemIndent)
		fb.segs = append(fb.segs, monoProse(blk.Text, lines, func(int) string { return hang })...)
		return fb
	}
	lines := pica.JustifyText(blk.Text, first, width-pica.ItemIndent)
	fb.segs = monoProse(blk.Text, lines, func(i int) string {
		if i == 0 {
			return blk.Label + strings.Repeat(" ", pica.TermGap)
		}
		return hang
	})
	if len(fb.segs) > 0 {
		fb.segs[0].lines[0].lead = blk.Label
	}
	return fb
}

// monoProse composes a prose block's monospace lines, one segment
// each: emphasis found once over the paragraph and mapped onto the
// lines (pica.EmphLines), marker cells blanked, underlines recorded.
// prefix gives line i's prefix -- an inset, a bullet, a run-in label
// -- set in front of it, so the recorded cells are the drawn cells
// while the prefix is never read for markers (a label may begin with
// an underscore). Only prose comes through here; verbatim, table and
// heading text never carries emphasis.
func monoProse(para string, lines []string, prefix func(i int) string) []seg {
	clean, spans := pica.EmphLines(para, lines)
	segs := make([]seg, len(lines))
	for i := range lines {
		pre := prefix(i)
		if off := utf8.RuneCountInString(pre); off > 0 {
			for k := range spans[i] {
				spans[i][k].Start += off
				spans[i][k].End += off
			}
		}
		segs[i] = seg{lines: []sline{{text: pre + clean[i], uline: spans[i]}}}
	}
	return segs
}

// truncMeasured cuts s to its longest prefix no wider than units
// under m, one rune at the least: pica.TruncLine for a measured face,
// for the one-line texts a sans page never wraps (a link's label, a
// term's label on its own line).
func truncMeasured(s string, units int, m pdf.Measurer) string {
	r := []rune(s)
	for len(r) > 1 && m.Width(string(r)) > units {
		r = r[:len(r)-1]
	}
	return string(r)
}

// noPrefix is monoProse's prefix for lines set flush.
func noPrefix(int) string { return "" }

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
		out[i] = sline{text: ln, role: roleHalf}
	}
	return out
}

// runeUnits is the mono advance in em-thousandths: the conversion
// between the table's rune grid and measurer units at the drawing
// size.
var runeUnits = int(emWidth * 1000)

// attachProse hangs a row's measured cells (LayoutMeasured only;
// none otherwise) onto its slines as positioned spans at natural
// spacing under m, each honoring its box's alignment: N and R
// right-align at the box's end, C centers, L and P sit at the box's
// start -- so a numeric column's header label hangs over its
// numbers.
func attachProse(lines []sline, cells []pica.ProseCell, m pdf.Measurer) {
	for _, pc := range cells {
		box := pc.Box
		for h, ln := range pc.Lines {
			if h >= len(lines) {
				break
			}
			off := box.Start * runeUnits
			switch pc.Align {
			case 'N', 'R':
				off = box.End*runeUnits - ln.Width
			case 'C':
				off = box.Start*runeUnits + ((box.End-box.Start)*runeUnits-ln.Width)/2
			}
			lines[h].prose = append(lines[h].prose, proseSpan{
				off:   off,
				words: ln.Words,
				gaps:  spread(ln, 0, m, true),
			})
		}
	}
}

// liftNums lifts a row's numbers out of its first line's mono text
// into spans anchored on each column's point, blanking their boxes.
// Trailing trim may have shortened the line into or before a box;
// the slice below clamps for that.
func liftNums(ln sline, nums []pica.NumCell) sline {
	if len(nums) == 0 {
		return ln
	}
	r := []rune(ln.text)
	for _, n := range nums {
		for i := n.Box.Start; i < min(n.Box.End, len(r)); i++ {
			r[i] = ' '
		}
		ln.nums = append(ln.nums, numSpan{sep: n.Sep, intPart: n.Int, tail: n.Tail})
	}
	ln.text = strings.TrimRight(string(r), " ")
	return ln
}
