// The text writer: renders a Doc to the fixed-width monospace page
// the document describes. Its typographic identity is fixed:
// ragged-right paragraphs, verbatim blocks truncated at width,
// layout commands consumed. See doc.go.
package pica

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"repani.com/typeset/format"
)

// Text renders the document at its own Layout.Width: the title
// first (followed by the byline when .by or .date is set), then
// each block -- paragraphs wrapped ragged-right, headings as
// "# text", rules as "---", tables laid out, verbatim lines
// truncated, .link lines re-emitted for the wire. Blocks are
// separated by one blank line unless they were contiguous in the
// source.
func (d *Doc) Text() string {
	width := d.Layout.Width
	out := []string{format.Trunc(d.Title, width)}
	if bl := d.Byline(); bl != "" {
		out = append(out, format.Trunc(bl, width))
	}
	for _, b := range d.Blocks {
		lines := renderBlock(b, width)
		if len(lines) == 0 {
			continue
		}
		if !b.Tight {
			out = append(out, "")
		}
		out = append(out, lines...)
	}
	// The rights notice closes the page: the text medium has no
	// per-page footer, so the honest rendering is a final line.
	if d.Rights != "" {
		out = append(out, "", format.Trunc(d.Rights, width))
	}
	return strings.Join(out, "\n") + "\n"
}

// renderBlock lays out one block at the given width as the text
// writer renders it: the fixed-width lines of that block alone, no
// separator. A table lays out at width because Parse laid it out at
// the same width.
func renderBlock(b Block, width int) []string {
	switch b.Kind {
	case Heading:
		marker := "# "
		if b.Level == 2 {
			marker = "## "
		}
		return []string{format.Trunc(marker+b.Text, width)}

	case Para, Quote, Item, Term:
		lp := LayProse(b, width, wrapText)
		var out []string
		if lp.Head != "" {
			out = append(out, lp.Head)
		}
		for i, ln := range lp.Lines {
			out = append(out, lp.Prefix[i]+ln)
		}
		if lp.Tail != "" {
			out = append(out, lp.Tail)
		}
		return out

	case RuleBlk:
		return []string{"---"}

	case LinkBlk:
		// Wire metadata: clients do not display it, so it is exempt
		// from the width budget (truncation would corrupt the URL).
		return []string{strings.TrimSpace(".link " + b.Text + " " + b.Label)}

	case TableBlk:
		tl, err := b.Table.Layout(width)
		if err != nil {
			panic(fmt.Sprintf("pica: a table Parse laid out at %d does not lay out: %v", width, err))
		}
		return tl.Lines()

	case Pre:
		out := make([]string, len(b.Lines))
		for i, ln := range b.Lines {
			out[i] = format.Trunc(ln, width)
		}
		return out

	default:
		panic(fmt.Sprintf("pica: unknown block kind %d", b.Kind))
	}
}

// ProseLayout is a prose block set on a monospace grid: its text's
// lines, each with the prefix set before it -- a quote's inset, an
// item's bullet or hang, a term's run-in label or hang -- and the
// lines around them: Head a term's label on a line of its own, Tail a
// quote's attribution, each "" when there is none.
type ProseLayout struct {
	Head   string
	Lines  []string
	Prefix []string // parallel to Lines
	Tail   string
}

// LayProse sets a Para, Quote, Item or Term at width, its text broken
// by set -- the first line on the measure first, the rest on width --
// in the geometry every writer shares, so the text page and the mono
// PDF differ only in how set breaks: a quote is inset QuoteIndent on
// both sides; an item's first line opens with Bullet and a space,
// its turnovers hang ItemIndent; a term's label runs in, TermGap
// before the text and its turnovers hanging ItemIndent, unless it
// leaves too little of the line (termRunIn), when it stands alone
// and every text line hangs.
func LayProse(b Block, width int, set func(para string, first, width int) []string) ProseLayout {
	var lp ProseLayout
	fill := func(first, rest string, measure0, measure int) {
		lp.Lines = set(b.Text, measure0, measure)
		lp.Prefix = make([]string, len(lp.Lines))
		for i := range lp.Prefix {
			lp.Prefix[i] = rest
		}
		if len(lp.Prefix) > 0 {
			lp.Prefix[0] = first
		}
	}
	hang := strings.Repeat(" ", ItemIndent)
	switch b.Kind {
	case Quote:
		inset := strings.Repeat(" ", QuoteIndent)
		fill(inset, inset, width-2*QuoteIndent, width-2*QuoteIndent)
		if b.Attrib != "" {
			lp.Tail = attribLine(b.Attrib, width)
		}
	case Item:
		fill(Bullet+" ", hang, width-ItemIndent, width-ItemIndent)
	case Term:
		if first, runIn := termRunIn(b.Label, width); runIn {
			fill(b.Label+strings.Repeat(" ", TermGap), hang, first, width-ItemIndent)
		} else {
			lp.Head = format.Trunc(b.Label, width)
			fill(hang, hang, width-ItemIndent, width-ItemIndent)
		}
	default:
		fill("", "", width, width)
	}
	return lp
}

// Monospace geometry of the structured prose blocks, shared by
// every writer so the blocks occupy identical line counts: a quote
// is inset QuoteIndent runes on BOTH sides; an item's first line
// opens with Bullet and a space, which together are ItemIndent
// runes wide, and its turnover lines hang ItemIndent runes under
// the bullet. The bullet is U+2022, covered by all four embedded
// faces (a full 600/1000 em cell in JuliaMono).
const (
	QuoteIndent = 2
	ItemIndent  = 2
	Bullet      = "•"
	// TermGap is the run-in gap: the spaces between a .term label
	// and its text on the label's line. A term's turnovers hang
	// ItemIndent runes, the item's geometry without the bullet.
	TermGap = 2
)

// termRunIn decides a .term label's placement at the given width:
// first is the measure the text has on the label's line (the width
// less the label and TermGap), and runIn whether the text runs in
// there at all. A label that leaves less than half the width to
// the text stands on its own line, and the text starts beneath it
// -- troff's .TP rule for an over-long tag. Every writer shares the
// decision, so their line counts agree.
func termRunIn(label string, width int) (first int, runIn bool) {
	first = width - utf8.RuneCountInString(label) - TermGap
	return first, 2*first >= width
}

// attribLine renders a quote attribution right-aligned to the
// quote's right margin (width - QuoteIndent): "-- WHO", truncated
// to the quote measure (width - 2*QuoteIndent) if need be.
func attribLine(attrib string, width int) string {
	s := format.Trunc("-- "+attrib, width-2*QuoteIndent)
	return strings.Repeat(" ", width-QuoteIndent-utf8.RuneCountInString(s)) + s
}
