// The Repani mark on page one: a renderer option, not a document
// command. The document says nothing about it -- a company imprint
// is publishing policy, chosen where the PDF is made (-mark), so
// the same source renders with or without it.
package main

import (
	_ "embed"

	"repani.com/pica/pdf"
)

// mark.stream is the mark as a PDF content stream in a 64 x 70 unit
// space (navy line, red and green offset fill). It is derived from
// the header SVG in the repani.com site repo, the drawing's source
// of truth (repani.com/docs/mark/README.t records the derivation).
//
//go:embed mark.stream
var markStream string

const (
	markName = "RepaniMark"
	markW    = 64.0             // form user-space width
	markH    = 70.0             // form user-space height
	markPt   = 12.0 / 25.4 * 72 // rendered height: 12 mm
	markGap  = 12.0             // white between the title block and the mark
)

// markWidth is the mark's rendered width in points.
func markWidth() float64 { return markPt * markW / markH }

// drawMark paints the mark on p with its top-right corner at
// (right, top).
func drawMark(p *pdf.Page, right, top float64) {
	scale := markPt / markH
	p.Form(markName, right-markWidth(), top-markPt, scale)
}
