// Drawing: composed lines onto a pdf.Page. The one layer that turns
// slines into draw calls -- the seam the display list (DESIGN.t §5)
// will eventually reify.
package main

import "repani.com/pica/pdf"

// emWidth is the body font's advance per rune in ems -- a metric of
// the embedded font, not a style choice.
var emWidth = pdf.EmWidth(pdf.Regular)

// drawColumn renders one column's styled lines. Proportional lines
// (words set) draw in the sans faces at the body size; text lines
// are monospace -- everything in a mono document, and verbatim or
// table content in a sans one, drawn at the size where .width runes
// fill the column.
func drawColumn(p *pdf.Page, lines []sline, x, top, colW float64, t typo) {
	y := top - t.ps
	if len(lines) > 0 {
		// A column that opens with an oversize line needs its first
		// baseline set for the larger glyphs.
		if s := roleScale(lines[0].role); s > 1 {
			y = top - t.ps*s
		}
	}
	for i, ln := range lines {
		// Leading precedes a line: each baseline sits its own slot
		// below the previous one — half a line for a note, one and a
		// half or two for the heading roles.
		if i > 0 {
			y -= t.lineH * float64(roleUnits(ln.role)) / 2
		}
		switch {
		case ln.style == styleRule:
			p.StrokeGray(0.3)
			ry := y + t.ps*0.35
			if len(ln.ruleSegs) > 0 {
				// Table rule: one hairline segment per column
				// interval on the mono grid.
				adv := emWidth * t.psMono
				for _, sg := range ln.ruleSegs {
					p.Line(x+float64(sg.Start)*adv, ry, x+float64(sg.End)*adv, ry, 0.5)
				}
			} else {
				p.Line(x, ry, x+colW, ry, 0.5)
			}

		case len(ln.words) > 0:
			font := pdf.Sans
			if ln.style == styleBold {
				font = pdf.SansBold
			}
			ps := t.ps * roleScale(ln.role)
			if ln.style == styleGray {
				p.Gray(0.45)
			}
			xw := x + float64(ln.indent)*t.ps/1000
			if ln.emph == nil {
				p.SetFont(font, ps)
				p.Words(xw, y, ln.words, ln.gaps)
			} else {
				drawEmphWords(p, xw, y, ps, ln)
			}
			if ln.style == styleGray {
				p.Gray(0)
			}
			if ln.href != "" {
				w := lineWidthPt(ln, font, ps)
				p.Link(xw, y-ps*0.25, xw+w, y+ps, ln.href)
			}

		case ln.text != "" || len(ln.nums) > 0 || len(ln.prose) > 0:
			font := pdf.Regular
			if ln.style == styleBold {
				font = pdf.Bold
			}
			// Half lines are table notes formatted on the doubled
			// rune grid, so column offsets land under their columns;
			// heading roles scale up on their taller slots.
			ps := t.psMono * roleScale(ln.role)
			if ln.style == styleGray {
				p.Gray(0.45)
			}
			if ln.text != "" {
				p.SetFont(font, ps)
				p.Text(x, y, ln.text)
			}
			// The typescript underline: one continuous rule per
			// emphasis span, occupying exactly the cells the text
			// page gives to the blanked marker underscores, so the
			// mono grid -- and the text-page identity -- never move.
			if len(ln.uline) > 0 {
				adv := emWidth * ps
				p.StrokeGray(0)
				uy := y - ps*0.15
				for _, sg := range ln.uline {
					p.Line(x+float64(sg.Start)*adv, uy, x+float64(sg.End)*adv, uy, ps*0.05)
				}
			}
			if ln.style == styleGray {
				p.Gray(0)
			}
			if ln.href != "" {
				w := pdf.Width(ln.text, font, ps)
				p.Link(x, y-ps*0.25, x+w, y+ps, ln.href)
			}
			// Numeric cells lifted off the mono grid: sans tabular
			// figures at the mono size, anchored at the column's
			// decimal cell. Sans digits (560) are narrower than mono
			// cells (600), so spans never overrun their columns.
			if len(ln.nums) > 0 || len(ln.prose) > 0 {
				adv := emWidth * ps
				sf := pdf.Sans
				if ln.style == styleBold {
					sf = pdf.SansBold
				}
				p.SetFont(sf, ps)
				for _, sp := range ln.nums {
					sx := x + float64(sp.sep)*adv
					if sp.intPart != "" {
						p.Text(sx-pdf.Width(sp.intPart, sf, ps), y, sp.intPart)
					}
					if sp.tail != "" {
						p.Text(sx, y, sp.tail)
					}
				}
				// Prose cells and header labels: measured sans lines
				// at their column offsets, ragged at natural spacing.
				for _, sp := range ln.prose {
					p.Words(x+float64(sp.off)*ps/1000, y, sp.words, sp.gaps)
				}
			}
		}
	}
}

// drawEmphWords draws a proportional prose line whose words carry
// emphasis flags: maximal same-face runs, each its own text object,
// positioned by the accumulated advances -- word widths measured
// with the face that draws them, plus the line's gaps -- so the
// drawn line reproduces the breaker's arithmetic exactly and the
// flush edge stays flush through the italics.
func drawEmphWords(p *pdf.Page, x, y, ps float64, ln sline) {
	mR, mI := pdf.Measure(pdf.Sans), pdf.Measure(pdf.SansItalic)
	off := 0 // em-thousandths from x
	for i := 0; i < len(ln.words); {
		j := i
		for j < len(ln.words) && ln.emph[j] == ln.emph[i] {
			j++
		}
		font, m := pdf.Sans, mR
		if ln.emph[i] {
			font, m = pdf.SansItalic, mI
		}
		p.SetFont(font, ps)
		p.Words(x+float64(off)*ps/1000, y, ln.words[i:j], ln.gaps[i:j-1])
		for k := i; k < j; k++ {
			off += m.Width(ln.words[k])
			if k < len(ln.gaps) {
				off += ln.gaps[k]
			}
		}
		i = j
	}
}

// lineWidthPt is the drawn width of a proportional line in points.
func lineWidthPt(ln sline, font pdf.Font, ps float64) float64 {
	m := pdf.Measure(font)
	u := 0
	for _, w := range ln.words {
		u += m.Width(w)
	}
	for _, g := range ln.gaps {
		u += g
	}
	return float64(u) * ps / 1000
}
