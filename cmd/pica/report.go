// Report presentation (DESIGN.md §7): the same source document
// rendered as a single-column report rather than a newspaper —
// generous margins, a left-aligned title block, hairline table
// rules, a "Page N of M" footer. Layout comes from the document
// trailer exactly as in the broadsheet; .cols is ignored, a report
// is one wide column, so .width is the report's characters per
// line.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pavlos/typeset"
	"github.com/pavlos/typeset/pdf"
)

const reportMargin = 54.0

func reportCmd(args []string) int {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	out := fs.String("o", "", "output file (default stdout)")
	pos := parseMixed(fs, args)
	if len(pos) > 1 {
		fmt.Fprintln(os.Stderr, "pica report: at most one input file (default stdin)")
		return 1
	}

	src, err := readInput(pos)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica report: %v\n", err)
		return 1
	}
	doc, err := typeset.Parse(string(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica report: %v\n", err)
		return 1
	}
	bytes, err := report(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica report: %v\n", err)
		return 1
	}
	return writeOutput(*out, bytes)
}

// report renders a parsed document as the report PDF.
func report(doc *typeset.Doc) ([]byte, error) {
	size := paperSize(doc.Layout.Paper)
	pageW, pageH := size.Dimensions()
	colW := pageW - 2*reportMargin

	t, err := deriveTypo(doc, colW, true)
	if err != nil {
		return nil, err
	}
	sans, ps, lineH := t.sans, t.ps, t.lineH

	titleFont, bodyFont := pdf.Bold, pdf.Regular
	if sans {
		titleFont, bodyFont = pdf.SansBold, pdf.Sans
	}
	title := doc.Title
	titlePt := max(13.0, min(22.0, ps*1.6))
	if w := pdf.Width(title, titleFont, titlePt); w > colW {
		titlePt *= colW / w
	}

	// Title block on page one: title, gray byline, rule; content
	// starts under the rule. Later pages run margin to margin.
	topY := pageH - reportMargin
	titleY := topY - titlePt
	headerBottom := titleY
	byline := doc.Byline()
	if byline != "" {
		headerBottom -= ps * 1.5
	}
	ruleY := headerBottom - 8
	colTopFirst := ruleY - lineH*0.8
	colTopRest := topY
	colBottom := reportMargin

	linesFirst := int((colTopFirst - colBottom) / lineH)
	linesRest := int((colTopRest - colBottom) / lineH)
	if linesFirst < 2*minKeep+2 {
		return nil, fmt.Errorf("page too small for a report at %.1fpt", ps)
	}

	blocks, err := compose(doc, t)
	if err != nil {
		return nil, err
	}
	capacity := func(col int) int {
		if col == 0 {
			return linesFirst
		}
		return linesRest
	}
	columns := flow(blocks, capacity)

	pdoc := &pdf.Doc{Title: title, Creator: "pica", PageSize: size, Compress: true}
	for pg, col := range columns {
		var p pdf.Page
		colTop := colTopRest
		if pg == 0 {
			colTop = colTopFirst
			p.SetFont(titleFont, titlePt)
			p.Text(reportMargin, titleY, title)
			if byline != "" {
				p.SetFont(bodyFont, ps)
				p.Gray(0.4)
				p.Text(reportMargin, titleY-ps*1.5, byline)
				p.Gray(0)
			}
			p.StrokeGray(0)
			p.Line(reportMargin, ruleY, pageW-reportMargin, ruleY, 0.75)
		}
		drawColumn(&p, col, reportMargin, colTop, colW, t)

		// Footer: page number with the total, known after flow.
		p.SetFont(bodyFont, ps*0.85)
		p.Gray(0.5)
		num := fmt.Sprintf("Page %d of %d", pg+1, len(columns))
		p.Text((pageW-pdf.Width(num, bodyFont, ps*0.85))/2, reportMargin/2-2, num)
		p.Gray(0)

		pdoc.Add(&p)
	}
	return pdoc.Bytes(), nil
}
