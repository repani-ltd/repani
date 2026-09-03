package raster

import (
	"fmt"
	"html"
	"strings"
)

// HTMLRows renders one panel as Rows lines of HTML for a <pre>: runs
// of cells in one ink become <span class="fN bM"> elements (N and M
// the palette indices); default-ink runs are bare text; a link is an
// <a> whose href is "#" and its target, wrapping the whole span,
// brackets included. Lines are not trimmed, so every line is exactly
// Cols cells.
func (c *Canvas) HTMLRows(panel int) []string {
	out := make([]string, c.Rows)
	for r := range c.Rows {
		var b strings.Builder
		var open Ink
		inSpan := false
		closeSpan := func() {
			if inSpan {
				b.WriteString("</span>")
				inSpan = false
			}
		}
		links := c.Links(panel, r)
		linkEnd := -1
		for x, cell := range c.Row(panel, r) {
			if len(links) > 0 && links[0].Col == x {
				closeSpan()
				fmt.Fprintf(&b, `<a href="#%s">`, html.EscapeString(links[0].Target))
				linkEnd = x + links[0].Len
				links = links[1:]
			}
			s := cell.Ink
			if cell.blank() {
				// A blank shows only its background: it stays in the open
				// span on the same ground, and needs none on the default.
				s = Ink{BG: cell.BG}
				if cell.BG == open.BG {
					s = open
				}
			}
			if s != open || (!inSpan && s != Ink{}) {
				closeSpan()
				if s != (Ink{}) {
					fmt.Fprintf(&b, `<span class="f%d b%d">`, s.FG, s.BG)
					inSpan = true
				}
				open = s
			}
			b.WriteString(html.EscapeString(string(CellRune(cell.Glyph))))
			if x+1 == linkEnd {
				closeSpan()
				b.WriteString("</a>")
				open = Ink{}
				linkEnd = -1
			}
		}
		closeSpan()
		out[r] = b.String()
	}
	return out
}

// HTMLRows is Decode(p).HTMLRows.
func (p *Page) HTMLRows(panel int) []string { return Decode(p).HTMLRows(panel) }

// HTMLDocument renders the page as one self-contained HTML document:
// a <pre> per panel laid out across panels to a row, an embedded
// stylesheet with the palette as CSS variables, no external resources.
// It is the showcase form: open it in any browser, or paste the body
// into another page.
func HTMLDocument(p *Page, across int, title string) string {
	if across < 1 {
		across = 1
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
:root {
  --ground: #0a0e17; --panel: #05080f; --rule: #1f2a3f;
  --c0: #cfd6e4; --c1: #ec4b3c; --c2: #3fd06f; --c3: #f2c53d;
  --c4: #4b7ff0; --c5: #d55cd8; --c6: #3fc9e6; --c7: #f4f6fa;
  --g1: #b3271b; --g2: #1f8a44; --g3: #b98e12; --g4: #1f4fc4;
  --g5: #9a2f9d; --g6: #1c8fa8; --g7: #e6e9f0;
}
body { margin: 0; padding: 24px; background: var(--ground); color: var(--c0); }
.raster { display: grid; grid-template-columns: repeat(%d, max-content); gap: 14px; width: max-content; }
.raster pre {
  margin: 0; padding: 0; background: var(--panel); border: 1px solid var(--rule);
  font-family: "IBM Plex Mono", Menlo, "DejaVu Sans Mono", Consolas, monospace;
  font-size: 16px; line-height: 1.2; white-space: pre; font-variant-ligatures: none;
}
.f1 { color: var(--c1) } .f2 { color: var(--c2) } .f3 { color: var(--c3) } .f4 { color: var(--c4) }
.f5 { color: var(--c5) } .f6 { color: var(--c6) } .f7 { color: var(--c7) }
.b1 { background: var(--g1) } .b2 { background: var(--g2) } .b3 { background: var(--g3) } .b4 { background: var(--g4) }
.b5 { background: var(--g5) } .b6 { background: var(--g6) } .b7 { background: var(--g7); color: var(--ground) }
a { color: inherit; text-decoration: none; cursor: pointer; }
a:hover, a:active { text-decoration: underline; }
</style>
<div class="raster">
`, html.EscapeString(title), across)
	for i := range p.Panels {
		fmt.Fprintf(&b, "<pre>%s</pre>\n", strings.Join(p.HTMLRows(i), "\n"))
	}
	b.WriteString("</div>\n</html>\n")
	return b.String()
}
