// The form layout (.layout form, DESIGN.t §24): a document of tables
// set as a ruled form. Every box is ruled, each .fmt block gets a
// heavier outline, rows are as tall as their measured text, and the
// page is the narrow-margined sheet a form wants. It shares fonts,
// measurers and the breaker with the flowing presentations, and
// nothing of their line-unit flow: a form's rows are measured in
// points.

package press

import (
	"fmt"
	"strings"

	"repani.com/pica"
	"repani.com/pica/pdf"
	"repani.com/typeset/wrap"
	"repani.com/typeset/wrap/hyphen"
)

// Form geometry, in points or ems of the body size: the press's
// typography, not document attributes.
const (
	formMargin  = 36.0 // half an inch on every side
	formLeading = 1.2  // line pitch in ems
	formPad     = 0.15 // a box's padding above and below its text, in ems
	formGap     = 0.6  // space between blocks, in ems
	formTitle   = 1.35 // the title's size, in ems
	formRule    = 0.4  // inner rules
	formFrame   = 1.0  // the outline of a .fmt block
	formGray    = 0.35 // inner rule gray
	formShade   = 0.9  // a header row's fill
)

// formSheet is one rendering's geometry and faces.
type formSheet struct {
	doc          *pica.Doc
	pageW, pageH float64
	cw, ps, lead float64 // grid cell width, body size, line pitch
	body, bold   pdf.Font
	mb, mbold    pdf.Measurer
}

// x is the left edge of grid column c's text; a box's rules sit half
// a cell outside its text, in the gaps.
func (s *formSheet) x(c int) float64 { return formMargin + s.cw/2 + float64(c)*s.cw }

// item is one unit of the form's flow: atomic, h points tall, drawn
// with its top at y. A table's rows carry the block they belong to,
// so the page can frame each block's part on it.
type item struct {
	h      float64
	gap    float64 // space above it, dropped at the top of a page
	block  int     // the .fmt block, or -1
	x0, x1 float64 // its outline's left and right edges (table rows)
	draw   func(p *pdf.Page, y float64)
}

func formPDF(doc *pica.Doc, mark bool) ([]byte, error) {
	size := paperSize(doc.Layout.Paper)
	pageW, pageH := size.Dimensions()
	// The grid is .width cells plus the half cell each side that
	// holds the outer rules.
	cw := (pageW - 2*formMargin) / float64(doc.Layout.Width+1)
	ps := cw / emWidth
	if ps < minPs {
		return nil, fmt.Errorf("derived body size %.1fpt is below %.1fpt: .width %d on %s leaves the form too narrow",
			ps, minPs, doc.Layout.Width, doc.Layout.Paper)
	}
	s := &formSheet{doc: doc, pageW: pageW, pageH: pageH, cw: cw, ps: ps, lead: ps * formLeading,
		body: pdf.Regular, bold: pdf.Bold}
	if doc.Layout.Font == "sans" {
		s.body, s.bold = pdf.Sans, pdf.SansBold
	}
	s.mb, s.mbold = pdf.Measure(s.body), pdf.Measure(s.bold)

	items, err := s.compose()
	if err != nil {
		return nil, err
	}

	// Pages: items in order, a new page when the next does not fit
	// above the footer. The title sits on page one.
	top := pageH - formMargin
	bottom := formMargin
	titlePt := ps * formTitle
	first := top - titlePt
	if b := doc.Byline(); b != "" {
		first -= ps * 1.4
	}
	first -= ps * formGap
	var pages [][]placed
	y, cur := first, []placed(nil)
	for _, it := range items {
		gap := it.gap
		if len(cur) == 0 {
			gap = 0
		}
		if y-gap-it.h < bottom && len(cur) > 0 {
			pages = append(pages, cur)
			cur, y, gap = nil, top, 0
		}
		y -= gap
		cur = append(cur, placed{it, y})
		y -= it.h
	}
	pages = append(pages, cur)

	pdoc := &pdf.Doc{
		Title: doc.Title, Author: doc.By, Creator: "pica", Producer: "Repani Limited",
		Created: infoDate(doc.Date), PageSize: size,
	}
	if mark {
		pdoc.AddForm(markName, markW, markH, markStream)
	}
	for pg, items := range pages {
		var p pdf.Page
		if pg == 0 {
			s.title(&p, top, titlePt)
			if mark {
				drawMark(&p, pageW-formMargin, top)
			}
		}
		for _, pl := range items {
			pl.draw(&p, pl.y)
		}
		s.frames(&p, items)
		num := fmt.Sprintf("Page %d of %d", pg+1, len(pages))
		if doc.Rights != "" {
			num = doc.Rights + " · " + num
		}
		s.centered(&p, formMargin/2, ps*0.8, s.body, 0.5, num)
		pdoc.Add(&p)
	}
	return pdoc.Bytes(), nil
}

type placed struct {
	item
	y float64
}

// frames draws the heavier outline round each .fmt block's part on
// a page: its rows' outer edges, the top of its first row there and
// the bottom of its last.
func (s *formSheet) frames(p *pdf.Page, items []placed) {
	p.StrokeGray(0)
	for i, pl := range items {
		if pl.block < 0 {
			continue
		}
		p.Line(pl.x0, pl.y, pl.x0, pl.y-pl.h, formFrame)
		p.Line(pl.x1, pl.y, pl.x1, pl.y-pl.h, formFrame)
		if i == 0 || items[i-1].block != pl.block {
			p.Line(pl.x0-formFrame/2, pl.y, pl.x1+formFrame/2, pl.y, formFrame)
		}
		if i == len(items)-1 || items[i+1].block != pl.block {
			b := pl.y - pl.h
			p.Line(pl.x0-formFrame/2, b, pl.x1+formFrame/2, b, formFrame)
		}
	}
}

// title draws the title centred in bold, and the byline under it.
func (s *formSheet) title(p *pdf.Page, top, pt float64) {
	s.centered(p, top-pt, pt, s.bold, 0, s.doc.Title)
	if b := s.doc.Byline(); b != "" {
		s.centered(p, top-pt-s.ps*1.4, s.ps*0.9, s.body, 0.4, b)
	}
}

func (s *formSheet) centered(p *pdf.Page, y, pt float64, f pdf.Font, gray float64, text string) {
	p.SetFont(f, pt)
	p.Gray(gray)
	p.Text((s.pageW-pdf.Width(text, f, pt))/2, y, text)
	p.Gray(0)
}

// compose turns the blocks into items: each table row, or rows a box
// runs down, one item; each line of prose one item.
func (s *formSheet) compose() ([]item, error) {
	var out []item
	gap := s.ps * formGap
	block := -1
	for _, b := range s.doc.Blocks {
		g := gap
		if b.Tight && b.Kind == pica.TableBlk {
			g = 0
		} else if b.Kind == pica.TableBlk {
			block++
		}
		start := len(out)
		switch b.Kind {
		case pica.TableBlk:
			rows, err := s.table(b.Table, block)
			if err != nil {
				return nil, err
			}
			out = append(out, rows...)
		case pica.Heading:
			out = append(out, s.lines(b.Text, s.bold, s.mbold, "")...)
		case pica.Para, pica.Quote, pica.Term:
			text := b.Text
			if b.Kind == pica.Term {
				text = b.Label + "  " + text
			}
			out = append(out, s.lines(text, s.body, s.mb, "")...)
		case pica.Item:
			out = append(out, s.lines(b.Text, s.body, s.mb, "• ")...)
		case pica.LinkBlk:
			text := b.Text
			if b.Label != "" {
				text = b.Label + " (" + b.Text + ")"
			}
			out = append(out, s.lines(text, s.body, s.mb, "")...)
		case pica.Pre:
			for _, ln := range b.Lines {
				out = append(out, s.text(ln, pdf.Regular))
			}
		case pica.RuleBlk:
			x0, x1 := formMargin, s.pageW-formMargin
			out = append(out, item{h: s.lead, block: -1, draw: func(p *pdf.Page, y float64) {
				p.StrokeGray(0)
				p.Line(x0, y-s.lead/2, x1, y-s.lead/2, formRule)
			}})
		}
		if len(out) > start {
			out[start].gap = g
		}
	}
	return out, nil
}

// lines wraps prose ragged across the grid, each line an item; lead
// starts the first line and hangs the rest.
func (s *formSheet) lines(text string, f pdf.Font, m pdf.Measurer, lead string) []item {
	units := int(float64(s.doc.Layout.Width) * s.cw * 1000 / s.ps)
	ind := m.Width(lead)
	var out []item
	for i, ln := range wrap.Hyphenated(text, units-ind, units-ind, hyphen.Default, wrap.PenaltyProse, m) {
		t := strings.Join(ln.Words, " ")
		if i == 0 {
			t = lead + t
		}
		x := s.x(0)
		if i > 0 {
			x += float64(ind) * s.ps / 1000
		}
		out = append(out, item{h: s.lead, block: -1, draw: func(p *pdf.Page, y float64) {
			p.SetFont(f, s.ps)
			p.Text(x, s.baseline(y, 0, 0), t)
		}})
	}
	return out
}

// text is one unwrapped line in face f.
func (s *formSheet) text(t string, f pdf.Font) item {
	return item{h: s.lead, block: -1, draw: func(p *pdf.Page, y float64) {
		p.SetFont(f, s.ps*0.9)
		p.Text(s.x(0), s.baseline(y, 0, 0), t)
	}}
}

// baseline is the baseline of line i of text whose box top is at y,
// padded by pad.
func (s *formSheet) baseline(top, pad float64, i int) float64 {
	return top - pad - float64(i)*s.lead - s.lead + (s.lead-s.ps)/2 + s.ps*0.22
}

// cell is one measured box of a row: its edges, alignment, face and
// lines -- or, for a box that rows run down, its span.
type cell struct {
	pica.GridBox
	bold  bool
	lines []string
	span  *fspan
}

// fspan is a box running down rows: its lines and the rows it covers.
type fspan struct {
	lines    []string
	from, to int
}

// table measures a grid's rows and returns them as items, rows a box
// runs down joined into one.
func (s *formSheet) table(t *pica.Table, block int) ([]item, error) {
	width := s.doc.Layout.Width
	_, rows, err := t.Grid(width)
	if err != nil {
		return nil, err
	}
	cells := make([][]cell, len(rows))
	for i, r := range rows {
		for _, b := range r.Boxes {
			c := cell{GridBox: b, bold: r.Header || r.Total}
			if r.Note {
				c.Align = 'L'
			}
			if !b.Cont {
				c.lines = s.wrapCell(b, c.bold)
			}
			cells[i] = append(cells[i], c)
		}
	}
	// Spans: a box the next row continues begins one; a continuing
	// box joins the span of the box above it.
	for i := range cells {
		for k := range cells[i] {
			c := &cells[i][k]
			if !c.Cont {
				continue
			}
			above := find(cells[i-1], c.Span)
			if above.span == nil {
				above.span = &fspan{lines: above.lines, from: i - 1}
			}
			above.span.to = i
			c.span = above.span
		}
	}
	// Heights in points: a row is as tall as its own boxes' lines,
	// padded; a span sets its lines at the leading from the top of
	// its first row, and when they need more than its rows give, its
	// last row grows by the difference.
	pad := s.ps * formPad
	tall := func(n int) float64 { return float64(max(n, 1))*s.lead + 2*pad }
	hp := make([]float64, len(rows))
	for i := range cells {
		n := 1
		for _, c := range cells[i] {
			if c.span == nil {
				n = max(n, len(c.lines))
			}
		}
		hp[i] = tall(n)
	}
	seen := map[*fspan]bool{}
	for i := range cells {
		for _, c := range cells[i] {
			if c.span == nil || seen[c.span] {
				continue
			}
			seen[c.span] = true
			have := 0.0
			for j := c.span.from; j <= c.span.to; j++ {
				have += hp[j]
			}
			if need := tall(len(c.span.lines)) - have; need > 0 {
				hp[c.span.to] += need
			}
		}
	}
	rowH := func(i int) float64 { return hp[i] }

	var out []item
	for i := 0; i < len(rows); {
		// The rows a span runs down are one item.
		j := i
		for j+1 < len(rows) && hasCont(cells[j+1]) {
			j++
		}
		first, last := i, j
		height := 0.0
		for k := first; k <= last; k++ {
			height += rowH(k)
		}
		x0 := s.x(cells[first][0].Start) - s.cw/2
		lastBox := cells[first][len(cells[first])-1]
		x1 := s.x(lastBox.End) + s.cw/2
		out = append(out, item{h: height, block: block, x0: x0, x1: x1, draw: func(p *pdf.Page, y float64) {
			// Header rows are shaded first, under every rule and
			// letter: each own box of the row, over the row's height.
			top := y
			p.Gray(formShade)
			for k := first; k <= last; k++ {
				if rows[k].Header {
					for _, c := range cells[k] {
						if !c.Cont {
							l, r := s.x(c.Start)-s.cw/2, s.x(c.End)+s.cw/2
							p.Fill(l, top-rowH(k), r-l, rowH(k))
						}
					}
				}
				top -= rowH(k)
			}
			p.Gray(0)
			top = y
			for k := first; k <= last; k++ {
				s.drawRow(p, cells[k], top, rowH(k), pad, rows[k].Note)
				// The bottom of every box the next row does not continue.
				p.StrokeGray(formGray)
				for _, c := range cells[k] {
					if k == last || !continued(cells[k+1], c.Span) {
						p.Line(s.x(c.Start)-s.cw/2, top-rowH(k), s.x(c.End)+s.cw/2, top-rowH(k), formRule)
					}
				}
				top -= rowH(k)
			}
		}})
		i = j + 1
	}
	return out, nil
}

// drawRow draws one row's text and its boxes' top and side rules; a
// continuing box draws no top and no text, since the row that began
// its span set all of the span's lines from the box's top.
func (s *formSheet) drawRow(p *pdf.Page, cells []cell, top, height, pad float64, note bool) {
	p.StrokeGray(formGray)
	for _, c := range cells {
		l, r := s.x(c.Start)-s.cw/2, s.x(c.End)+s.cw/2
		if !c.Cont {
			p.Line(l, top, r, top, formRule)
		}
		p.Line(l, top, l, top-height, formRule)
		p.Line(r, top, r, top-height, formRule)
	}
	for _, c := range cells {
		if c.Cont {
			continue
		}
		lines, align, bold := c.lines, c.Align, c.bold
		f, pt := s.body, s.ps
		if bold {
			f = s.bold
		}
		if note {
			pt, f = s.ps*0.85, s.body
			p.Gray(0.35)
		}
		p.SetFont(f, pt)
		w := float64(c.End-c.Start) * s.cw
		for n, t := range lines {
			x := s.x(c.Start)
			switch tw := pdf.Width(t, f, pt); align {
			case 'R', 'N':
				x += w - tw
			case 'C':
				x += (w - tw) / 2
			}
			p.Text(x, s.baseline(top, pad, n), t)
		}
		p.Gray(0)
	}
}

// wrapCell breaks a box's text at its width in the face it sets in:
// one line for a clip or N box, else the cell breaker's hyphenating
// ragged lines.
func (s *formSheet) wrapCell(b pica.GridBox, bold bool) []string {
	m := s.mb
	if bold {
		m = s.mbold
	}
	if b.Text == "" {
		return nil
	}
	units := int(float64(b.End-b.Start) * s.cw * 1000 / s.ps)
	var out []string
	for _, ln := range wrap.Hyphenated(b.Text, units, units, hyphen.Default, wrap.PenaltyCell, m) {
		out = append(out, strings.Join(ln.Words, " "))
	}
	if (b.Clip || b.Align == 'N') && len(out) > 1 {
		out = out[:1]
	}
	return out
}

func hasCont(cs []cell) bool {
	for _, c := range cs {
		if c.Cont {
			return true
		}
	}
	return false
}

func continued(next []cell, sp pica.Span) bool {
	for _, c := range next {
		if c.Cont && c.Span == sp {
			return true
		}
	}
	return false
}

func find(cs []cell, sp pica.Span) *cell {
	for k := range cs {
		if cs[k].Span == sp {
			return &cs[k]
		}
	}
	panic("press: a continued box missing from the row above")
}
