// The pdf subcommand: render a pica source document as an
// N-column newspaper. Geometry comes entirely from the document's
// layout trailer (self-contained: same source, same PDF bytes); the
// body point size is derived from column width and .width.
package main

import (
	"fmt"

	"repani.com/pica"
	"repani.com/pica/pdf"
)

// mastRuleGap is the extra white on each side of the masthead rule.
const mastRuleGap = 4.0

func pdfCmd(args []string) int {
	fs := newFlags("pdf")
	out := fs.String("o", "", "output file (default stdout)")
	doc, rc := loadDoc("pdf", fs, args)
	if doc == nil {
		return rc
	}
	bytes, err := broadsheet(doc)
	if err != nil {
		fmt.Fprintf(stderr, "pica pdf: %v\n", err)
		return 1
	}
	return writeOutput("pdf", *out, bytes)
}

// broadsheet renders a parsed document as the newspaper PDF.
func broadsheet(doc *pica.Doc) ([]byte, error) {
	ncols := doc.Layout.Cols
	return paged(doc, presentation{
		ncols:  ncols,
		what:   fmt.Sprintf("%d columns", ncols),
		header: masthead,
		footer: func(s *sheet, p *pdf.Page, pg, _ int) {
			// Bottom margin line, centered: the page number alone,
			// or joined to the rights notice in one combined footer.
			num := fmt.Sprintf("- %d -", pg+1)
			if s.doc.Rights != "" {
				num = fmt.Sprintf("%s · page %d", s.doc.Rights, pg+1)
			}
			s.centered(p, s.margin/2-2, s.t.ps*0.9, 0.4, num)
		},
	})
}

// masthead lays out the page-one masthead band: the title sized to
// fill the measure (the floor of 8 average characters keeps short
// titles from ballooning), a centered gray dateline when the
// document has a byline, and a rule under both.
func masthead(s *sheet) (float64, func(p *pdf.Page)) {
	usableW := s.pageW - 2*s.margin
	title, ps := s.doc.Title, s.t.ps
	floor1 := 8 * float64(pdf.AvgAdvance(s.titleFont)) / 1000
	mastPt := usableW / max(pdf.Width(title, s.titleFont, 1), floor1)
	mastPt = max(12, min(30, mastPt))
	byline := s.doc.Byline()
	mastBottom := s.topY - mastPt*1.35
	headerBottom := mastBottom
	if byline != "" {
		headerBottom -= ps * 1.5
	}
	ruleY := headerBottom - mastRuleGap
	colTop := ruleY - mastRuleGap - s.t.lineH*0.6
	return colTop, func(p *pdf.Page) {
		p.SetFont(s.titleFont, mastPt)
		w := pdf.Width(title, s.titleFont, mastPt)
		p.Text((s.pageW-w)/2, s.topY-mastPt, title)
		if byline != "" {
			s.centered(p, mastBottom-ps, ps, 0.4, byline)
		}
		p.StrokeGray(0)
		p.Line(s.margin, ruleY, s.pageW-s.margin, ruleY, 1.0)
	}
}
