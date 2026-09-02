package tessera

import (
	"fmt"
	"html"
	"strings"
)

// HTMLRows renders one panel as 28 lines of HTML for a <pre>: runs of
// cells in one ink become <span class="fN bM"> elements (N and M the
// palette indices); default-ink runs are bare text. Ink codes render
// as a space in the state they establish. Lines are not trimmed, so
// every line is exactly 34 cells.
func (p *Page) HTMLRows(panel int) []string {
	out := make([]string, Rows)
	for r := range Rows {
		var b strings.Builder
		var s, open ink
		inSpan := false
		flush := func() {
			if inSpan {
				b.WriteString("</span>")
				inSpan = false
			}
		}
		for _, c := range p.Row(panel, r) {
			ch := CellRune(c)
			switch {
			case c >= InkFG && c < InkBG:
				s.fg = c - InkFG
			case c >= InkBG && c <= inkLast:
				s.bg = c - InkBG
			}
			if s != open || (!inSpan && s != ink{}) {
				flush()
				if s != (ink{}) {
					fmt.Fprintf(&b, `<span class="f%d b%d">`, s.fg, s.bg)
					inSpan = true
				}
				open = s
			}
			b.WriteString(html.EscapeString(string(ch)))
		}
		flush()
		out[r] = b.String()
	}
	return out
}

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
.tessera { display: grid; grid-template-columns: repeat(%d, max-content); gap: 14px; width: max-content; }
.tessera pre {
  margin: 0; padding: 0; background: var(--panel); border: 1px solid var(--rule);
  font-family: "IBM Plex Mono", Menlo, "DejaVu Sans Mono", Consolas, monospace;
  font-size: 16px; line-height: 1.2; white-space: pre; font-variant-ligatures: none;
}
.f1 { color: var(--c1) } .f2 { color: var(--c2) } .f3 { color: var(--c3) } .f4 { color: var(--c4) }
.f5 { color: var(--c5) } .f6 { color: var(--c6) } .f7 { color: var(--c7) }
.b1 { background: var(--g1) } .b2 { background: var(--g2) } .b3 { background: var(--g3) } .b4 { background: var(--g4) }
.b5 { background: var(--g5) } .b6 { background: var(--g6) } .b7 { background: var(--g7); color: var(--ground) }
</style>
<div class="tessera">
`, html.EscapeString(title), across)
	for i := range Panels {
		fmt.Fprintf(&b, "<pre>%s</pre>\n", strings.Join(p.HTMLRows(i), "\n"))
	}
	b.WriteString("</div>\n</html>\n")
	return b.String()
}
