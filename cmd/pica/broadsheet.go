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
	sheetGutter = 20.0
	lineSpacing = 1.25 // line height in ems
	minPs       = 4.5  // readability floor for the derived body size
	mastRuleGap = 4.0  // extra white on each side of the masthead rule
)

// pageMargin derives the margin from the declared column count, the
// same way the point size derives from .width: geometry is
// writer-owned but a function of the document's layout. Multi-column
// pages spend their real estate to the edges, newspaper-fashion; a
// single measure takes the book margin — the report's, so pica's
// two single-column outputs share one geometry.
func pageMargin(ncols int) float64 {
	if ncols == 1 {
		return reportMargin
	}
	return sheetMargin
}

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
	size := paperSize(doc.Layout.Paper)
	pageW, pageH := size.Dimensions()
	margin := pageMargin(ncols)
	usableW := pageW - 2*margin
	colW := (usableW - float64(ncols-1)*sheetGutter) / float64(ncols)

	t, err := deriveTypo(doc, colW)
	if err != nil {
		return nil, err
	}
	sans, ps, lineH := t.sans, t.ps, t.lineH

	// Masthead band on page one. The floor of 8 average characters
	// keeps short titles from ballooning. A byline (.by/.date) adds
	// a centered dateline row under the masthead.
	topY := pageH - margin
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
	colBottom := margin

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
			p.Line(margin, ruleY, pageW-margin, ruleY, 1.0)
		}

		deepest := 0 // column depth in half-line units
		for c := range ncols {
			idx := pg*ncols + c
			if idx >= len(columns) {
				break
			}
			units := 0
			for _, ln := range columns[idx] {
				units += lineUnits(ln)
			}
			deepest = max(deepest, units)
			x := margin + float64(c)*(colW+sheetGutter)
			drawColumn(&p, columns[idx], x, colTop, colW, t)
		}

		// Hairline rules centered in the gutters, to content depth.
		ruleBottom := max(colTop-float64(deepest)*lineH/2, colBottom)
		p.StrokeGray(0.55)
		for c := 1; c < ncols; c++ {
			x := margin + float64(c)*(colW+sheetGutter) - sheetGutter/2
			p.Line(x, colTop, x, ruleBottom, 0.4)
		}

		// Bottom margin line, centered: the page number alone, or
		// joined to the rights notice in one combined footer.
		p.SetFont(bodyFont, ps*0.9)
		p.Gray(0.4)
		num := fmt.Sprintf("- %d -", pg+1)
		if doc.Rights != "" {
			num = fmt.Sprintf("%s · page %d", doc.Rights, pg+1)
		}
		p.Text((pageW-pdf.Width(num, bodyFont, ps*0.9))/2, margin/2-2, num)
		p.Gray(0)

		pdoc.Add(&p)
	}
	return pdoc.Bytes(), nil
}
