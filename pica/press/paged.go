// The paged driver shared by the two PDF presentations: page
// geometry from the document trailer, typography through
// deriveTypo, compose, flow, then one pdf.Page per page with the
// presentation's title block on page one and its footer on every
// page. The default presentation (PDF) and the report differ only in what
// they hand the driver.

package press

import (
	"fmt"
	"strings"
	"time"

	"repani.com/pica"
	"repani.com/pica/pdf"
)

// Writer identity constants (points). These are the press's
// typography, not document attributes.
const (
	sheetMargin     = 40.0
	reportMargin    = 54.0
	sheetGutter     = 20.0
	lineSpacing     = 1.25 // line height in ems, short measures
	lineSpacingWide = 1.35 // line height in ems, long measures
	minPs           = 4.5  // readability floor for the derived body size
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

// infoDate attempts to read the free-text .date as a calendar date
// for the PDF Info CreationDate. The spec deliberately does not
// constrain .date, so this is best-effort over common shapes; text
// that matches none of them returns the zero time and the Info
// entry is simply omitted. The date comes from the document, never
// the clock, so output stays byte-deterministic.
func infoDate(s string) time.Time {
	for _, layout := range []string{
		"2006-01-02",
		"January 2, 2006",
		"2 January 2006",
		"Jan 2, 2006",
		"2 Jan 2006",
	} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t
		}
	}
	return time.Time{}
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

// sheet is the resolved page geometry and typography of one
// rendering: what a presentation's header and footer draw against.
type sheet struct {
	doc                 *pica.Doc
	pageW, margin, colW float64
	topY                float64 // top of the type area (page height - margin)
	t                   typo
	titleFont, bodyFont pdf.Font
	mark                bool // the Repani mark sits top-right of page one
}

// headerRight is the right edge available to the page-one title
// block: the type area's, less the mark and its gap when the mark
// is on.
func (s *sheet) headerRight() float64 {
	r := s.pageW - s.margin
	if s.mark {
		r -= markWidth() + markGap
	}
	return r
}

// presentation is what distinguishes the default presentation from the report
// under the paged driver.
type presentation struct {
	ncols int    // columns per page
	what  string // for the page-too-small error: "3 columns", "a report"
	mark  bool   // paint the Repani mark top-right of page one
	// header lays out the page-one title block: it returns the y
	// where column content starts under it, and the draw call that
	// paints it on the first page.
	header func(s *sheet) (colTop float64, draw func(p *pdf.Page))
	// footer paints the bottom-margin line of page pg (0-based) of
	// total.
	footer func(s *sheet, p *pdf.Page, pg, total int)
}

// paged renders doc through the presentation: page one opens with
// the header, later pages run margin to margin, every page carries
// the footer. flow always returns at least one column, so at least
// one page is emitted.
func paged(doc *pica.Doc, pres presentation) ([]byte, error) {
	ncols := pres.ncols
	size := paperSize(doc.Layout.Paper)
	pageW, pageH := size.Dimensions()
	margin := pageMargin(ncols)
	usableW := pageW - 2*margin
	colW := (usableW - float64(ncols-1)*sheetGutter) / float64(ncols)

	t, err := deriveTypo(doc, colW)
	if err != nil {
		return nil, err
	}
	s := &sheet{
		doc: doc, pageW: pageW, margin: margin, colW: colW,
		topY: pageH - margin, t: t,
		titleFont: pdf.Bold, bodyFont: pdf.Regular,
		mark: pres.mark,
	}
	if t.sans {
		s.titleFont, s.bodyFont = pdf.SansBold, pdf.Sans
	}
	colTopFirst, drawHeader := pres.header(s)
	colTopRest := s.topY
	colBottom := margin

	linesFirst := int((colTopFirst - colBottom) / t.lineH)
	linesRest := int((colTopRest - colBottom) / t.lineH)
	if linesFirst < 2*minKeep+2 {
		return nil, fmt.Errorf("page too small for %s at %.1fpt", pres.what, t.ps)
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

	// Balance a single underfull multi-column page: the smallest
	// uniform capacity that still fits ncols columns. (One column
	// already holds everything; there is nothing to balance.)
	if ncols > 1 && len(columns) <= ncols {
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

	pdoc := &pdf.Doc{
		Title:    doc.Title,
		Author:   doc.By,
		Creator:  "pica",
		Producer: "Repani Limited",
		Created:  infoDate(doc.Date),
		PageSize: size,
		Compress: true,
	}
	if pres.mark {
		pdoc.AddForm(markName, markW, markH, markStream)
	}
	total := (len(columns) + ncols - 1) / ncols
	for pg := 0; pg < total; pg++ {
		var p pdf.Page
		colTop := colTopRest
		if pg == 0 {
			colTop = colTopFirst
			drawHeader(&p)
			if pres.mark {
				drawMark(&p, s.pageW-s.margin, s.topY)
			}
		}

		deepest := 0 // column depth in half-line units
		for c := range ncols {
			idx := pg*ncols + c
			if idx >= len(columns) {
				break
			}
			units := 0
			for _, ln := range columns[idx] {
				units += roleUnits(ln.role)
			}
			deepest = max(deepest, units)
			x := margin + float64(c)*(colW+sheetGutter)
			drawColumn(&p, columns[idx], x, colTop, colW, t)
		}

		// Hairline rules centered in the gutters, to content depth.
		if ncols > 1 {
			ruleBottom := max(colTop-float64(deepest)*t.lineH/2, colBottom)
			p.StrokeGray(0.55)
			for c := 1; c < ncols; c++ {
				x := margin + float64(c)*(colW+sheetGutter) - sheetGutter/2
				p.Line(x, colTop, x, ruleBottom, 0.4)
			}
		}

		pres.footer(s, &p, pg, total)
		pdoc.Add(&p)
	}
	return pdoc.Bytes(), nil
}

// centered draws text centered on the page at baseline y, in the
// body face at size ps and the given gray level (black restored
// after).
func (s *sheet) centered(p *pdf.Page, y, ps, gray float64, text string) {
	p.SetFont(s.bodyFont, ps)
	p.Gray(gray)
	p.Text((s.pageW-pdf.Width(text, s.bodyFont, ps))/2, y, text)
	p.Gray(0)
}
