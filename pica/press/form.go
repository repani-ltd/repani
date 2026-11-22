// The form layout (.layout form, DESIGN.t §24 and §25): a document of tables
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
	formMargin   = 36.0 // half an inch on every side
	formLeading  = 1.2  // line pitch in ems
	formPad      = 0.15 // a box's padding above and below its text, in ems
	formGap      = 0.6  // space between blocks, in ems
	formTitle    = 1.35 // the title's size, in ems
	formByline   = 1.4  // the byline's drop under the title, in ems
	formRule     = 0.4  // inner rules
	formFrame    = 1.0  // the outline of a .fmt block
	formGray     = 0.35 // inner rule gray
	formShade    = 0.9  // a header row's fill
	formNote     = 0.8  // a note row's size, of the body's
	formNoteGray = 0.3  // a note row's ink
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
	size := paperSize(doc.Trailer.Paper)
	pageW, pageH := size.Dimensions()
	// The grid is .width cells plus the half cell each side that
	// holds the outer rules.
	cw := (pageW - 2*formMargin) / float64(doc.Trailer.Width+1)
	ps := cw / emWidth
	if ps < minPs {
		return nil, fmt.Errorf("derived body size %.1fpt is below %.1fpt: .width %d on %s leaves the form too narrow",
			ps, minPs, doc.Trailer.Width, doc.Trailer.Paper)
	}
	s := &formSheet{doc: doc, pageW: pageW, pageH: pageH, cw: cw, ps: ps, lead: ps * formLeading,
		body: pdf.Regular, bold: pdf.Bold}
	if doc.Trailer.Font == "sans" {
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
		first -= ps * formByline
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

	pdoc := newDoc(doc, size, mark)
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
		centered(&p, pageW, formMargin/2, ps*0.8, s.body, 0.5, pageOf(doc, pg, len(pages)))
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
	centered(p, s.pageW, top-pt, pt, s.bold, 0, s.doc.Title)
	if b := s.doc.Byline(); b != "" {
		centered(p, s.pageW, top-pt-s.ps*formByline, s.ps*0.9, s.body, 0.4, b)
	}
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
		case pica.HeadingBlk:
			out = append(out, s.lines(b.Text, s.bold, s.mbold, "")...)
		case pica.ParaBlk, pica.QuoteBlk, pica.TermBlk:
			text := b.Text
			if b.Kind == pica.TermBlk {
				text = b.Label + "  " + text
			}
			out = append(out, s.lines(text, s.body, s.mb, "")...)
		case pica.ItemBlk:
			out = append(out, s.lines(b.Text, s.body, s.mb, "• ")...)
		case pica.LinkBlk:
			text := b.Text
			if b.Label != "" {
				text = b.Label + " (" + b.Text + ")"
			}
			out = append(out, s.lines(text, s.body, s.mb, "")...)
		case pica.PreBlk:
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
	units := int(float64(s.doc.Trailer.Width) * s.cw * 1000 / s.ps)
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
			p.Text(x, s.baseline(y, 0, 0, 1), t)
		}})
	}
	return out
}

// text is one unwrapped line in face f.
func (s *formSheet) text(t string, f pdf.Font) item {
	return item{h: s.lead, block: -1, draw: func(p *pdf.Page, y float64) {
		p.SetFont(f, s.ps*0.9)
		p.Text(s.x(0), s.baseline(y, 0, 0, 1), t)
	}}
}

// baseline is the baseline of line i of text set at sc times the body
// size, whose box top is at top, padded by pad: the text centred in its
// line pitch, lifted 0.22em for the descenders.
func (s *formSheet) baseline(top, pad float64, i int, sc float64) float64 {
	lead, ps := s.lead*sc, s.ps*sc
	return top - pad - float64(i)*lead - lead + (lead-ps)/2 + ps*0.22
}

// cell is one measured box of a row: its grid box, its face and size
// (sc times the body's: a note row's is smaller), and its lines -- for
// a box that runs down rows, all of them, set from its first row.
type cell struct {
	pica.GridBox
	bold  bool
	sc    float64
	lines []string
}

// table measures a grid's rows and returns them as items, the rows a
// box runs down joined into one.
func (s *formSheet) table(t *pica.Table, block int) ([]item, error) {
	rows, err := t.Grid(s.doc.Trailer.Width)
	if err != nil {
		return nil, err
	}
	cells := make([][]cell, len(rows))
	for i, r := range rows {
		for _, b := range r.Boxes {
			c := cell{GridBox: b, bold: r.Header || r.Total, sc: 1}
			if r.Note {
				c.Align, c.sc = 'L', formNote
			}
			if !b.Cont {
				c.lines = s.cellLines(c.GridBox, c.bold, c.sc) // a note's L, not its column's
			}
			cells[i] = append(cells[i], c)
		}
	}
	// Heights in points: a row is as tall as the lines of its boxes
	// that run down no rows, padded. A box that runs down rows sets its
	// lines at the leading from the top of its first row, and when they
	// need more than its rows give, its last row grows by the
	// difference.
	pad := s.ps * formPad
	tall := func(n int, sc float64) float64 { return float64(max(n, 1))*s.lead*sc + 2*pad }
	hp := make([]float64, len(rows))
	for i := range cells {
		sc := 1.0
		if rows[i].Note {
			sc = formNote
		}
		hp[i] = tall(1, sc)
		for _, c := range cells[i] {
			if c.Rows == 1 {
				hp[i] = max(hp[i], tall(len(c.lines), c.sc))
			}
		}
	}
	// down is the height of n rows from row i.
	down := func(i, n int) float64 {
		h := 0.0
		for _, x := range hp[i : i+n] {
			h += x
		}
		return h
	}
	for i := range cells {
		for _, c := range cells[i] {
			if !c.Cont && c.Rows > 1 {
				if need := tall(len(c.lines), c.sc) - down(i, c.Rows); need > 0 {
					hp[i+c.Rows-1] += need
				}
			}
		}
	}

	var out []item
	for i := 0; i < len(rows); {
		// The rows a box runs down are one item, and so are rows whose
		// boxes overlap theirs.
		first, last := i, i
		for k := first; k <= last; k++ {
			for _, c := range cells[k] {
				last = max(last, c.Top+c.Rows-1)
			}
		}
		height := down(first, last-first+1)
		x0 := s.x(cells[first][0].Start) - s.cw/2
		lastBox := cells[first][len(cells[first])-1]
		x1 := s.x(lastBox.End) + s.cw/2
		out = append(out, item{h: height, block: block, x0: x0, x1: x1, draw: func(p *pdf.Page, y float64) {
			// Fills first, under every rule and letter: a box with a
			// background colour takes its tint, a header row's box
			// without one the header gray, each over its whole height
			// -- every row it runs down.
			top := y
			for k := first; k <= last; k++ {
				for _, c := range cells[k] {
					if c.Cont || (c.BG == 0 && !rows[k].Header) {
						continue
					}
					h := down(k, c.Rows)
					if c.BG != 0 {
						p.RGB(pica.Tint(c.BG))
					} else {
						p.Gray(formShade)
					}
					l, r := s.x(c.Start)-s.cw/2, s.x(c.End)+s.cw/2
					p.Fill(l, top-h, r-l, h)
				}
				top -= hp[k]
			}
			p.Gray(0)
			top = y
			for k := first; k <= last; k++ {
				s.drawRow(p, cells[k], top, hp[k], pad, rows[k].Note)
				// The bottom of every box that ends on this row.
				p.StrokeGray(formGray)
				for _, c := range cells[k] {
					if k == c.Top+c.Rows-1 {
						p.Line(s.x(c.Start)-s.cw/2, top-hp[k], s.x(c.End)+s.cw/2, top-hp[k], formRule)
					}
				}
				top -= hp[k]
			}
		}})
		i = last + 1
	}
	return out, nil
}

// drawRow draws one row's text and its boxes' top and side rules; a
// continuing box draws no top and no text, since the row that began
// it set all of its lines from the box's top.
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
		f, pt := s.body, s.ps*c.sc
		if bold {
			f = s.bold
		}
		switch {
		case c.FG != 0:
			p.RGB(pica.Ink(c.FG))
		case note:
			p.Gray(formNoteGray)
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
			p.Text(x, s.baseline(top, pad, n, c.sc), t)
		}
		p.Gray(0)
	}
}

// cellLines breaks a box's text at its width in the face and size it
// sets in, with pica's cell breaker: one line for a clip or N box.
func (s *formSheet) cellLines(b pica.GridBox, bold bool, sc float64) []string {
	m := s.mb
	if bold {
		m = s.mbold
	}
	if b.Text == "" {
		return nil
	}
	units := int(float64(b.End-b.Start) * s.cw * 1000 / (s.ps * sc))
	var out []string
	for _, ln := range pica.CellLines(b.Text, units, m, b.Clip || b.Align == 'N') {
		out = append(out, strings.Join(ln.Words, " "))
	}
	return out
}
