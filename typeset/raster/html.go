package raster

import (
	"fmt"
	"html"
	"strings"
)

// HTMLRows renders the raster as Height lines of HTML for a <pre>:
// runs of cells in one ink become <span class="fN bM"> elements (N
// and M the palette indices); default-ink runs are bare text; a link
// is an <a> whose href is "#" and its target, wrapping the whole
// span, brackets included. Lines are not trimmed, so every line is
// exactly Cols cells.
func (r *Raster) HTMLRows() []string {
	out := make([]string, r.Height())
	for i := range out {
		var b strings.Builder
		var open Ink
		inSpan := false
		closeSpan := func() {
			if inSpan {
				b.WriteString("</span>")
				inSpan = false
			}
		}
		links := r.Links(i)
		linkEnd := -1
		for x, cell := range r.Rows[i] {
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
		out[i] = b.String()
	}
	return out
}

// HTMLDocument renders rasters as one self-contained HTML document in
// a theme: a <pre> per raster laid out across to a row, an embedded
// stylesheet with the embedded face (FontCSS), no external resources.
// It is the showcase form: open it in any browser, or paste the body
// into another page.
func HTMLDocument(rs []*Raster, across int, title string, theme Theme) string {
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
%sbody { margin: 0; padding: 24px; background: var(--ground); color: var(--c0); }
.raster { display: grid; grid-template-columns: repeat(%d, max-content); gap: 14px; width: max-content; }
.raster pre {
  margin: 0; padding: 0; background: var(--panel); border: 1px solid var(--rule);
  font-family: JuliaMono, monospace;
  font-size: 15px; line-height: 20px; white-space: pre;
  font-variant-ligatures: none; font-kerning: none; font-feature-settings: "calt" 0, "liga" 0; text-rendering: optimizeSpeed;
}
a { color: inherit; text-decoration: none; cursor: pointer; }
a:hover, a:active { text-decoration: underline; }
%s</style>
<div class="raster">
`, html.EscapeString(title), FontCSS(), across, theme.CSS())
	for _, r := range rs {
		fmt.Fprintf(&b, "<pre>%s</pre>\n", strings.Join(r.HTMLRows(), "\n"))
	}
	b.WriteString("</div>\n</html>\n")
	return b.String()
}
