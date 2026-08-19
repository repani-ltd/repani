// The HTML writer: renders a Doc to one semantic <article> fragment.
// It needs no metrics -- the browser owns wrapping, justification
// and width -- so it lives beside the text writer on the block model
// alone. The fragment carries no stylesheet, no page shell and no
// classes beyond the few that name a meaning the element cannot
// (byline, attribution, link reference, total and note rows); a
// page is the consumer's business (pica html -txtar assembles one
// from a template). Nothing is inferred from content: bare URLs in
// prose stay text, .link is the only link.
package pica

import (
	"html"
	"strings"
)

// HTML renders the document as an <article> fragment: <h1> title,
// byline and <footer> rights from the metadata; <p>, <h2>/<h3>,
// <hr>, <ul> (consecutive .item blocks form one list), <pre>,
// <blockquote> with attribution, <p class="link"><a>, and <table>
// with thead unless headerless, per-column alignment, total and
// note rows. Layout commands have no HTML meaning and are consumed;
// a fixed table width becomes max-width in ch.
func (d *Doc) HTML() string {
	var w strings.Builder
	w.WriteString("<article>\n")
	w.WriteString("<h1>" + esc(d.Title) + "</h1>\n")
	if bl := d.Byline(); bl != "" {
		w.WriteString(`<p class="byline">` + esc(bl) + "</p>\n")
	}
	inList := false
	for _, b := range d.Blocks {
		if b.Kind == Item {
			if !inList {
				w.WriteString("<ul>\n")
				inList = true
			}
			w.WriteString("<li>" + esc(b.Text) + "</li>\n")
			continue
		}
		if inList {
			w.WriteString("</ul>\n")
			inList = false
		}
		htmlBlock(&w, b)
	}
	if inList {
		w.WriteString("</ul>\n")
	}
	if d.Rights != "" {
		w.WriteString("<footer>" + esc(d.Rights) + "</footer>\n")
	}
	w.WriteString("</article>\n")
	return w.String()
}

func htmlBlock(w *strings.Builder, b Block) {
	switch b.Kind {
	case Para:
		w.WriteString("<p>" + esc(b.Text) + "</p>\n")
	case Heading:
		tag := "h2"
		if b.Level == 2 {
			tag = "h3"
		}
		w.WriteString("<" + tag + ">" + esc(b.Text) + "</" + tag + ">\n")
	case RuleBlk:
		w.WriteString("<hr>\n")
	case Pre:
		w.WriteString("<pre>")
		for i, ln := range b.Lines {
			if i > 0 {
				w.WriteString("\n")
			}
			w.WriteString(esc(ln))
		}
		w.WriteString("</pre>\n")
	case LinkBlk:
		url, title := splitLink(b.Text)
		if title == "" {
			title = url
		}
		w.WriteString(`<p class="link"><a href="` + esc(url) + `">` + esc(title) + "</a></p>\n")
	case Quote:
		w.WriteString("<blockquote>\n<p>" + esc(b.Text) + "</p>\n")
		if b.Attrib != "" {
			w.WriteString(`<p class="attrib">` + esc(b.Attrib) + "</p>\n")
		}
		w.WriteString("</blockquote>\n")
	case TableBlk:
		htmlTable(w, b)
	}
}

// splitLink separates a LinkBlk's "URL [TITLE]" text.
func splitLink(s string) (url, title string) {
	url, title, _ = strings.Cut(s, " ")
	return url, strings.TrimSpace(title)
}

// htmlTable writes a table: the header row in <thead> when the table
// has one, then data rows, total rows (class "total") and note rows
// (class "note"); every cell carries its column's text-align. A
// fixed width from the spec becomes max-width in ch.
func htmlTable(w *strings.Builder, b Block) {
	t := b.Table
	open := "<table>"
	if b.Width > 0 {
		open = `<table style="max-width:` + itoa(b.Width) + `ch">`
	}
	w.WriteString(open + "\n")
	align := func(i int) string {
		if i >= len(t.cols) {
			return ""
		}
		switch t.cols[i].align {
		case 'R', 'N':
			return ` style="text-align:right"`
		case 'C':
			return ` style="text-align:center"`
		}
		return ""
	}
	cells := func(tag string, cs []string) {
		w.WriteString("<tr>")
		for i, c := range cs {
			w.WriteString("<" + tag + align(i) + ">" + esc(c) + "</" + tag + ">")
		}
		w.WriteString("</tr>\n")
	}
	if t.header != nil {
		w.WriteString("<thead>\n")
		cells("th", t.header)
		w.WriteString("</thead>\n")
	}
	w.WriteString("<tbody>\n")
	for _, r := range t.rows {
		switch {
		case r.total:
			w.WriteString(`<tr class="total">`)
			for i, c := range r.cells {
				w.WriteString("<td" + align(i) + ">" + esc(c) + "</td>")
			}
			w.WriteString("</tr>\n")
		case r.note:
			w.WriteString(`<tr class="note">`)
			for i, c := range r.cells {
				w.WriteString("<td" + align(i) + ">" + esc(c) + "</td>")
			}
			w.WriteString("</tr>\n")
		default:
			cells("td", r.cells)
		}
	}
	w.WriteString("</tbody>\n</table>\n")
}

func esc(s string) string { return html.EscapeString(s) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
