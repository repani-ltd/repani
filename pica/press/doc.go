// Package press prints parsed pica documents as PDF: the
// compositor (blocks to styled lines, column flow, the paged
// driver) behind two presentations. PDF is the default, the
// document rendered as itself -- every layout parameter comes from
// the document's own trailer, so the package exposes no layout
// options by construction, and the same source always produces
// the same bytes. Report is house stationery: single column,
// left-aligned title block, "Page N of M" footer.
//
// The settled surface is PDF; Report is held loosely and may move.
// The one option either takes is the mark, because an imprint is
// publishing policy, chosen where the PDF is made, never in the
// document.
//
// The presentation seam (chrome hooks over the paged driver) is
// deliberately unexported: DESIGN.t section 5 reifies it as a
// display list of positioned ops when the first non-PDF backend or
// viewer arrives, and this package is where those backends will
// live as siblings of draw.go.
package press
