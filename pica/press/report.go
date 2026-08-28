// Report presentation (DESIGN.t §7): the same source document
// rendered as a single-column client report rather than under the
// default masthead — generous margins, a left-aligned title block,
// a "Page N of M" footer. Layout comes from the document trailer
// exactly as in PDF; .cols is ignored, a report is one wide
// column, so .width is the report's characters per line.

package press

import (
	"fmt"

	"repani.com/pica"
	"repani.com/pica/pdf"
)

// Report renders a parsed document as the report PDF. mark paints
// the Repani mark top-right of page one, as in PDF.
func Report(doc *pica.Doc, mark bool) ([]byte, error) {
	return paged(doc, presentation{
		ncols:  1,
		what:   "a report",
		mark:   mark,
		header: titleBlock,
		footer: func(s *sheet, p *pdf.Page, pg, total int) {
			// Footer, centered: the page number with the total,
			// joined to the rights notice when present.
			num := fmt.Sprintf("Page %d of %d", pg+1, total)
			if s.doc.Rights != "" {
				num = s.doc.Rights + " · " + num
			}
			s.centered(p, s.margin/2-2, s.t.ps*0.85, 0.5, num)
		},
	})
}

// titleBlock lays out the page-one title block: left-aligned
// title, gray byline, rule; content starts under the rule.
func titleBlock(s *sheet) (float64, func(p *pdf.Page)) {
	title, ps := s.doc.Title, s.t.ps
	titlePt := max(13.0, min(22.0, ps*1.6))
	if w, budget := pdf.Width(title, s.titleFont, titlePt), s.headerRight()-s.margin; w > budget {
		titlePt *= budget / w
	}
	titleY := s.topY - titlePt
	headerBottom := titleY
	byline := s.doc.Byline()
	if byline != "" {
		headerBottom -= ps * 1.5
	}
	ruleY := headerBottom - 8
	colTop := ruleY - s.t.lineH*0.8
	return colTop, func(p *pdf.Page) {
		p.SetFont(s.titleFont, titlePt)
		p.Text(s.margin, titleY, title)
		if byline != "" {
			p.SetFont(s.bodyFont, ps)
			p.Gray(0.4)
			p.Text(s.margin, titleY-ps*1.5, byline)
			p.Gray(0)
		}
		p.StrokeGray(0)
		p.Line(s.margin, ruleY, s.pageW-s.margin, ruleY, 0.75)
	}
}
