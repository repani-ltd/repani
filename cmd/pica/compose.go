// Composition: parsed blocks become styled lines (slines) grouped
// into flowable blocks (fblocks). The writer-identity layer shared
// by the broadsheet and report presentations.
package main

import (
	"fmt"
	"strings"

	"github.com/pavlos/typeset"
	"github.com/pavlos/typeset/pdf"
)

// bullet is the .item marker, matching the text writer's (U+2022,
// covered by all four embedded faces).
const bullet = "•"

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
	ruleW  int      // styleRule: width in mono grid runes (0 = full column)
	nums   []numSpan
	prose  []proseSpan
}

// proseSpan is one measured line of a P (prose) table cell in a
// sans document: the mono grid reserves the cell's space blank, and
// the line draws in the sans face at the column's grid offset. The
// grid stays mono; only declared prose lifts off it — the same
// narrowing that made numeric columns cheap.
type proseSpan struct {
	start int // rune offset of the column on the mono grid
	words []string
	gaps  []int
}

// sizeRole is the closed set of sizes, quantized to the half-line
// grid (DESIGN.md §6, §7): every role is a slot of whole half-units
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
func deriveTypo(doc *typeset.Doc, colW float64) (typo, error) {
	width := doc.Layout.Width
	sans := doc.Layout.Font == "sans"
	psMono := colW / (emWidth * float64(width))
	ps := psMono
	units := 0
	if sans {
		units = width * pdf.AvgAdvance(pdf.Sans)
		ps = colW * 1000 / float64(units)
	}
	if ps < minPs {
		return typo{}, fmt.Errorf(
			"derived body size %.1fpt is below %.1fpt: .width %d on %s leaves the measure too narrow",
			ps, minPs, width, doc.Layout.Paper)
	}
	return typo{
		sans: sans, ps: ps, psMono: psMono,
		lineH: ps * lineSpacing, units: units,
	}, nil
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

func lineUnits(ln sline) int { return roleUnits(ln.role) }

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
	var out []fblock
	for bi := 0; bi < len(doc.Blocks); bi++ {
		blk := doc.Blocks[bi]
		if blk.Kind == typeset.Item {
			run := []typeset.Block{blk}
			for bi+1 < len(doc.Blocks) && doc.Blocks[bi+1].Kind == typeset.Item && doc.Blocks[bi+1].Tight {
				bi++
				run = append(run, doc.Blocks[bi])
			}
			out = append(out, composeItems(run, t, width)...)
			continue
		}
		fb := fblock{tight: blk.Tight}
		switch blk.Kind {
		case typeset.Para:
			if t.sans {
				m := pdf.Measure(pdf.Sans)
				lines := typeset.JustifyLines(blk.Text, t.units, m)
				for i, ln := range lines {
					last := i == len(lines)-1
					sl := sline{words: ln.Words, gaps: spread(ln, t.units, m, last)}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
			} else {
				for _, ln := range typeset.JustifyParagraph(blk.Text, width) {
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
				lines := typeset.JustifyLines(blk.Text, measure, m)
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
				for _, ln := range typeset.JustifyParagraph(blk.Text, width-4) {
					fb.segs = append(fb.segs, seg{lines: []sline{{text: "  " + ln}}})
				}
				if blk.Attrib != "" {
					s := trunc("-- "+blk.Attrib, width-4)
					s = strings.Repeat(" ", width-2-len([]rune(s))) + s
					fb.segs = append(fb.segs, seg{lines: []sline{{text: s}}})
				}
			}

		case typeset.Heading:
			// "#" sections set at the display role, "##" subsections
			// at the heading role: larger glyphs on taller slots of
			// the same half-line grid, so the hierarchy reads without
			// flow learning anything new. The wrap measure shrinks by
			// the glyph scale (bigger ems, same physical column).
			role := roleDisplay
			if blk.Level == 2 {
				role = roleHeading
			}
			if t.sans {
				measure := t.units * 2 / 3
				if role == roleHeading {
					measure = t.units * 5 / 6
				}
				m := pdf.Measure(pdf.SansBold)
				for _, ln := range typeset.WrapLines(blk.Text, measure, m) {
					sl := sline{words: ln.Words, gaps: spread(ln, measure, m, true), style: styleBold, role: role}
					fb.segs = append(fb.segs, seg{lines: []sline{sl}})
				}
			} else {
				budget := width * 2 / 3
				if role == roleHeading {
					budget = width * 5 / 6
				}
				fb.segs = []seg{{lines: []sline{{text: trunc(blk.Text, budget), style: styleBold, role: role}}}}
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
			// Sans documents measure P (prose) cells with the sans
			// measurer at the column's mono measure (rune width x
			// the mono advance in em-thousandths); mono documents
			// lay P out as L.
			var tl *typeset.TableLayout
			var err error
			if t.sans {
				tl, err = blk.Table.LayoutMeasured(
					blk.TableWidth(width), pdf.Measure(pdf.Sans), int(emWidth*1000))
			} else {
				tl, err = blk.Table.Layout(blk.TableWidth(width))
			}
			if err != nil {
				return nil, err
			}
			// Sans documents: numeric cells leave the mono grid and
			// re-render in the sans face with tabular figures. The
			// extraction runs over every full-size line; headers,
			// separators, and "n/a" cells fail SplitNumeric and stay
			// mono. Note lines stay mono half-size.
			mark := func(lines []sline) []sline {
				if !t.sans {
					return lines
				}
				for i := range lines {
					if lines[i].role == roleBody {
						lines[i] = extractNums(lines[i], tl.NumCols)
					}
				}
				return lines
			}
			// The separator/total rule: a hairline spanning the
			// table. The dash row is the text writer's rendering;
			// every PDF presentation draws the real rule.
			rule := func() sline {
				return sline{style: styleRule, ruleW: len([]rune(tl.Sep))}
			}
			if len(tl.Header) > 0 {
				lines := toSlines(tl.Header)
				lines = append(lines, halfSlines(tl.HeaderNotes)...)
				lines = append(lines, rule())
				fb.segs = append(fb.segs, seg{lines: mark(lines)})
				fb.repeat = 1
			}
			// Uniform row pitch: when every row note fits a single
			// half-line, every data row carries the half — noted
			// rows use it, plain rows leave it blank air — so the
			// table's vertical rhythm stays even. Variable heights
			// are right only when notes wrap. Total rows sit under
			// their rule and stay unpadded.
			hasNote, fits := false, true
			for _, n := range tl.RowNotes {
				if len(n) > 0 {
					hasNote = true
				}
				if len(n) > 1 {
					fits = false
				}
			}
			even := hasNote && fits
			for j, row := range tl.Rows {
				rowLines := toSlines(row)
				attachProse(rowLines, tl, j)
				var lines []sline
				if tl.Totals[j] {
					for i := range rowLines {
						rowLines[i].style = styleBold
					}
					lines = append(lines, rule())
				}
				lines = append(lines, rowLines...)
				lines = append(lines, halfSlines(tl.RowNotes[j])...)
				if even && len(tl.RowNotes[j]) == 0 && !tl.Totals[j] {
					lines = append(lines, sline{role: roleHalf})
				}
				fb.segs = append(fb.segs, seg{lines: mark(lines)})
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

// composeItems renders a tight run of items. When any item in the
// run turns over, a half-line gap separates the items — the same
// conditional, quantized policy as the table row pitch: an all
// single-line run stays classically tight, a wrapped run gets air
// so turnovers cannot be misread as sibling items. The spacer joins
// each item's LAST seg, so a split between items lands after it (an
// invisible half-line at a column bottom) and a column can never
// open with a stranded gap. The text writer has no half-line and
// keeps every run tight.
func composeItems(run []typeset.Block, t typo, width int) []fblock {
	fbs := make([]fblock, len(run))
	wrapped := false
	for i, blk := range run {
		fbs[i] = composeItem(blk, t, width)
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
func composeItem(blk typeset.Block, t typo, width int) fblock {
	fb := fblock{tight: blk.Tight}
	if t.sans {
		m := pdf.Measure(pdf.Sans)
		ii := m.Width(bullet) + m.Space()
		measure := t.units - ii
		lines := typeset.JustifyLines(blk.Text, measure, m)
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
		for i, ln := range typeset.JustifyParagraph(blk.Text, width-2) {
			pre := "  "
			if i == 0 {
				pre = bullet + " "
			}
			fb.segs = append(fb.segs, seg{lines: []sline{{text: pre + ln}}})
		}
	}
	return fb
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
		out[i] = sline{text: ln, role: roleHalf}
	}
	return out
}

// attachProse hangs a row's measured P-cell lines (LayoutMeasured
// only; RowProse is nil otherwise) onto the row's slines as
// positioned sans spans at natural spacing.
func attachProse(rowLines []sline, tl *typeset.TableLayout, j int) {
	if j >= len(tl.RowProse) || tl.RowProse[j] == nil {
		return
	}
	m := pdf.Measure(pdf.Sans)
	for k, lines := range tl.RowProse[j] {
		for h, ln := range lines {
			if h >= len(rowLines) {
				break
			}
			rowLines[h].prose = append(rowLines[h].prose, proseSpan{
				start: tl.ProseCols[k].Start,
				words: ln.Words,
				gaps:  spread(ln, 0, m, true),
			})
		}
	}
}

// extractNums lifts a line's numeric N-column cells into spans and
// blanks them out of the mono text. Trailing trim may have shortened
// the line into or before a cell; the slice below clamps for that.
func extractNums(ln sline, cols []typeset.NumCol) sline {
	r := []rune(ln.text)
	for _, c := range cols {
		if c.Start >= len(r) {
			continue
		}
		end := min(c.End, len(r))
		raw := strings.TrimSpace(string(r[c.Start:end]))
		intPart, tail, ok := typeset.SplitNumeric(raw)
		if !ok {
			continue
		}
		for i := c.Start; i < end; i++ {
			r[i] = ' '
		}
		ln.nums = append(ln.nums, numSpan{sep: c.SepIndex(), intPart: intPart, tail: tail})
	}
	if len(ln.nums) > 0 {
		ln.text = strings.TrimRight(string(r), " ")
	}
	return ln
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
