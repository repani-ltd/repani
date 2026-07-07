// The pdf subcommand: render a typeset source document as an
// N-column newspaper. Geometry comes entirely from the document's
// layout trailer (self-contained: same source, same PDF bytes); the
// body point size is derived from column width and .width.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pavlos/typeset"
	"github.com/pavlos/typeset/pdf"
)

// Writer identity constants (points). These are the gazette's
// typography, not document attributes.
const (
	sheetMargin = 40.0
	sheetGutter = 16.0
	lineSpacing = 1.25 // line height in ems
	minPs       = 4.5  // readability floor for the derived body size
)

// emWidth is the body font's advance per rune in ems -- a metric of
// the embedded font, not a style choice.
var emWidth = pdf.EmWidth(pdf.Regular)

// minKeep is the orphan/widow threshold: a split never leaves fewer
// than minKeep segments of a block on either side of a column break.
const minKeep = 2

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

// sline is one composed output line with its drawing style.
type sline struct {
	text  string
	style style
}

// seg is an atomic run of lines: a paragraph line, a table row (all
// its wrapped lines), a whole .pre block, ...
type seg struct {
	lines []sline
}

func (s seg) height() int { return len(s.lines) }

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
func compose(doc *typeset.Doc) ([]fblock, error) {
	width := doc.Layout.Width
	var out []fblock
	for _, blk := range doc.Blocks {
		fb := fblock{tight: blk.Tight}
		switch blk.Kind {
		case typeset.Para:
			for _, ln := range typeset.JustifyParagraph(blk.Text, width) {
				fb.segs = append(fb.segs, seg{lines: []sline{{text: ln}}})
			}

		case typeset.Heading:
			fb.segs = []seg{{lines: []sline{{text: trunc(blk.Text, width), style: styleBold}}}}
			fb.keepNext = true // flow guards the no-next-block case
			fb.atomic = true

		case typeset.RuleBlk:
			fb.segs = []seg{{lines: []sline{{style: styleRule}}}}
			fb.atomic = true

		case typeset.LinkBlk:
			fb.segs = []seg{{lines: []sline{{text: trunc(blk.Text, width), style: styleGray}}}}
			fb.atomic = true

		case typeset.TableBlk:
			tl, err := blk.Table.Layout(blk.TableWidth(width))
			if err != nil {
				return nil, err
			}
			if len(tl.Header) > 0 {
				fb.segs = append(fb.segs, seg{lines: toSlines(tl.Header)})
				fb.repeat = 1
			}
			for _, row := range tl.Rows {
				fb.segs = append(fb.segs, seg{lines: toSlines(row)})
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

// ── Column flow ─────────────────────────────────────────────────────

// flow distributes blocks into columns of capacity(i) lines each.
// Splits happen only between segments, leaving at least minKeep
// segments on both sides; the repeated lead-in (table headers,
// .pre N) is re-emitted after each split. Atomic blocks move whole
// unless taller than an entire fresh column. A heading is never
// left at a column bottom without minKeep segments of what follows.
func flow(blocks []fblock, capacity func(int) int) [][]sline {
	var out [][]sline
	var cur []sline
	colIdx := 0

	closeCol := func() {
		out = append(out, cur)
		cur = nil
		colIdx++
	}
	place := func(b fblock, upto int) {
		if len(cur) > 0 && !b.tight {
			cur = append(cur, sline{})
		}
		for _, s := range b.segs[:upto] {
			cur = append(cur, s.lines...)
		}
	}

	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		for {
			cap := capacity(colIdx)
			sep := 0
			if len(cur) > 0 && !b.tight {
				sep = 1
			}
			avail := cap - len(cur) - sep
			h := b.height()

			// Keep-with-next: the heading and the first minKeep
			// segments of the next block must fit together.
			if b.keepNext && i+1 < len(blocks) && len(cur) > 0 {
				next := blocks[i+1]
				need := h + 1
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

// fitSegs returns how many leading segments of b fit in avail lines.
func fitSegs(b fblock, avail int) int {
	k, lines := 0, 0
	for k < len(b.segs) && lines+b.segs[k].height() <= avail {
		lines += b.segs[k].height()
		k++
	}
	return k
}

// splitSegs returns how many leading segments of b fit in avail
// lines under the orphan/widow rules, or 0 for "move whole".
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

	// Derived body size: the column holds exactly .width runes.
	ps := colW / (emWidth * float64(width))
	if ps < minPs {
		return nil, fmt.Errorf(
			"derived body size %.1fpt is below %.1fpt: .width %d with .cols %d on %s leaves columns too narrow",
			ps, minPs, width, ncols, doc.Layout.Paper)
	}
	lineH := ps * lineSpacing

	// Masthead band on page one.
	topY := pageH - sheetMargin
	title := doc.Title
	mastPt := usableW / (emWidth * float64(max(len([]rune(title)), 8)))
	mastPt = max(12, min(30, mastPt))
	colTopFirst := topY - mastPt*1.35 - 10
	colTopRest := topY
	colBottom := sheetMargin

	linesFirst := int((colTopFirst - colBottom) / lineH)
	linesRest := int((colTopRest - colBottom) / lineH)
	if linesFirst < 2*minKeep+2 {
		return nil, fmt.Errorf("page too small for %d columns at %.1fpt", ncols, ps)
	}

	blocks, err := compose(doc)
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
			p.SetFont(pdf.Bold, mastPt)
			w := pdf.Width(title, pdf.Bold, mastPt)
			p.Text((pageW-w)/2, topY-mastPt, title)
			p.StrokeGray(0)
			p.Line(sheetMargin, colTop+lineH*0.6, pageW-sheetMargin, colTop+lineH*0.6, 1.0)
		}

		deepest := 0
		for c := 0; c < ncols; c++ {
			idx := pg*ncols + c
			if idx >= len(columns) {
				break
			}
			if len(columns[idx]) > deepest {
				deepest = len(columns[idx])
			}
			x := sheetMargin + float64(c)*(colW+sheetGutter)
			drawColumn(&p, columns[idx], x, colTop, colW, ps, lineH)
		}

		// Hairline rules centered in the gutters, to content depth.
		ruleBottom := max(colTop-float64(deepest)*lineH, colBottom)
		p.StrokeGray(0.55)
		for c := 1; c < ncols; c++ {
			x := sheetMargin + float64(c)*(colW+sheetGutter) - sheetGutter/2
			p.Line(x, colTop, x, ruleBottom, 0.4)
		}

		// Page number, centered in the bottom margin.
		p.SetFont(pdf.Regular, ps*0.9)
		p.Gray(0.4)
		num := fmt.Sprintf("- %d -", pg+1)
		p.Text((pageW-pdf.Width(num, pdf.Regular, ps*0.9))/2, sheetMargin/2-2, num)
		p.Gray(0)

		pdoc.Add(&p)
	}
	return pdoc.Bytes(), nil
}

// drawColumn renders one column's styled lines.
func drawColumn(p *pdf.Page, lines []sline, x, top, colW, ps, lineH float64) {
	y := top - ps
	for _, ln := range lines {
		switch {
		case ln.style == styleRule:
			p.StrokeGray(0.3)
			p.Line(x, y+ps*0.35, x+colW, y+ps*0.35, 0.5)
		case ln.text != "":
			font := pdf.Regular
			if ln.style == styleBold {
				font = pdf.Bold
			}
			if ln.style == styleGray {
				p.Gray(0.45)
			}
			p.SetFont(font, ps)
			p.Text(x, y, ln.text)
			if ln.style == styleGray {
				p.Gray(0)
			}
		}
		y -= lineH
	}
}
