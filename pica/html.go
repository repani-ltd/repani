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
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"

	"repani.com/typeset/tbl"
)

// HTML renders the document as an <article> fragment: <h1> title,
// byline and <footer> rights from the metadata; <p>, <h2>/<h3>,
// <hr>, <ul> (consecutive .item blocks form one list), <dl> with
// <dt>/<dd> (consecutive .term blocks form one list), <pre>,
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
	// Consecutive items form one <ul>, consecutive terms one <dl>;
	// open names the list element currently open, if any.
	open := ""
	closeList := func() {
		if open != "" {
			w.WriteString("</" + open + ">\n")
			open = ""
		}
	}
	for _, b := range d.Blocks {
		switch b.Kind {
		case Item:
			if open != "ul" {
				closeList()
				w.WriteString("<ul>\n")
				open = "ul"
			}
			w.WriteString("<li>" + emphHTML(b.Text) + "</li>\n")
			continue
		case Term:
			if open != "dl" {
				closeList()
				w.WriteString("<dl>\n")
				open = "dl"
			}
			w.WriteString("<dt>" + esc(b.Label) + "</dt>\n<dd>" + emphHTML(b.Text) + "</dd>\n")
			continue
		}
		closeList()
		htmlBlock(&w, b)
	}
	closeList()
	if d.Rights != "" {
		w.WriteString("<footer>" + esc(d.Rights) + "</footer>\n")
	}
	w.WriteString("</article>\n")
	return w.String()
}

func htmlBlock(w *strings.Builder, b Block) {
	switch b.Kind {
	case Para:
		w.WriteString("<p>" + emphHTML(b.Text) + "</p>\n")
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
		url, title := b.Text, b.Label
		if title == "" {
			title = url
		}
		w.WriteString(`<p class="link"><a href="` + esc(url) + `">` + esc(title) + "</a></p>\n")
	case Quote:
		w.WriteString("<blockquote>\n<p>" + emphHTML(b.Text) + "</p>\n")
		if b.Attrib != "" {
			w.WriteString(`<p class="attrib">` + esc(b.Attrib) + "</p>\n")
		}
		w.WriteString("</blockquote>\n")
	case TableBlk:
		htmlTable(w, b.Table)
	}
}

// htmlTable writes a table: the header row in <thead> when the table
// has one, then data rows, total rows (class "total") and note rows
// (class "note"); every cell but a note's, which sets left as on
// every page, carries the text-align of its box's first column, a box
// over several columns a colspan, a box running down rows (^) a
// rowspan, and a coloured cell its ink and fill from the print
// palette. A narrowing width from the spec becomes max-width in ch.
func htmlTable(w *strings.Builder, t *Table) {
	open := "<table>"
	if n := t.Narrow(); n > 0 {
		open = `<table style="max-width:` + strconv.Itoa(n) + `ch">`
	}
	w.WriteString(open + "\n")
	rows := t.tt.Rows()
	header := len(rows) > 0 && rows[0].Role == tbl.Header
	caret := func(j, col int) bool { return t.fms[j].Cols[col].Align == '^' }
	type cell struct {
		box  tbl.Box
		text string
		own  bool
		code tbl.Code // resolved: the cell's mark, the row code, the column's
	}
	write := func(i int, tag, class string) {
		r, fm := rows[i], t.fms[i]
		var cells []cell
		for k, bx := range fm.Boxes(len(r.Cells)) {
			code := r.Cells[k].Code.Over(fm.Row.Over(fm.Cols[bx.First].Code))
			cells = append(cells, cell{bx, r.Cells[k].Text, true, code})
		}
		// A header box that rows continue (^) cannot span out of
		// <thead>: each row continuing it holds an empty cell instead.
		if header && i > 0 {
			for _, bx := range t.fms[0].Boxes(len(rows[0].Cells)) {
				j := 1
				for j <= i && caret(j, bx.First) {
					j++
				}
				if j > i {
					cells = append(cells, cell{box: bx})
				}
			}
			sort.Slice(cells, func(a, b int) bool { return cells[a].box.First < cells[b].box.First })
		}
		w.WriteString("<tr" + class + ">")
		for _, c := range cells {
			bx, attrs := c.box, ""
			if bx.Last > bx.First {
				attrs = ` colspan="` + strconv.Itoa(bx.Last-bx.First+1) + `"`
			}
			if c.own && !(header && i == 0) {
				n := 1
				for j := i + 1; j < len(rows) && caret(j, bx.First); j++ {
					n++
				}
				if n > 1 {
					attrs += ` rowspan="` + strconv.Itoa(n) + `"`
				}
			}
			var style []string
			switch align := fm.Cols[bx.First].Align; {
			case !c.own, r.Role == tbl.Note:
			case align == 'R' || align == 'N':
				style = append(style, "text-align:right")
			case align == 'C':
				style = append(style, "text-align:center")
			}
			if c.code.HasFG && c.code.FG != 0 {
				style = append(style, "color:"+hexColor(Ink(uint8(c.code.FG))))
			}
			if c.code.HasBG && c.code.BG != 0 {
				style = append(style, "background:"+hexColor(Tint(uint8(c.code.BG))))
			}
			if style != nil {
				attrs += ` style="` + strings.Join(style, ";") + `"`
			}
			w.WriteString("<" + tag + attrs + ">" + esc(c.text) + "</" + tag + ">")
		}
		w.WriteString("</tr>\n")
	}
	first := 0
	if header {
		w.WriteString("<thead>\n")
		write(0, "th", "")
		w.WriteString("</thead>\n")
		first = 1
	}
	w.WriteString("<tbody>\n")
	for i := first; i < len(rows); i++ {
		switch rows[i].Role {
		case tbl.Total:
			write(i, "td", ` class="total"`)
		case tbl.Note:
			write(i, "td", ` class="note"`)
		default:
			write(i, "td", "")
		}
	}
	w.WriteString("</tbody>\n</table>\n")
}

func esc(s string) string { return html.EscapeString(s) }

// hexColor writes a palette colour as CSS #rrggbb.
func hexColor(r, g, b float64) string {
	return fmt.Sprintf("#%02x%02x%02x", int(r*255+0.5), int(g*255+0.5), int(b*255+0.5))
}

// emphHTML renders prose that may carry _..._ emphasis: the element
// that carries the meaning is <em>, and the markers are consumed.
// Only flowing prose (Para, Quote, Item) goes through here; every
// other block's underscores are content.
func emphHTML(s string) string {
	segs := emphSegments(s)
	if len(segs) == 1 && !segs[0].Emph {
		return esc(s)
	}
	var b strings.Builder
	for _, sg := range segs {
		if sg.Emph {
			b.WriteString("<em>" + esc(sg.Text) + "</em>")
		} else {
			b.WriteString(esc(sg.Text))
		}
	}
	return b.String()
}
