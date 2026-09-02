// The text writer: renders a Doc to the fixed-width monospace page
// the document describes. Its typographic identity is fixed:
// ragged-right paragraphs, verbatim blocks truncated at width,
// layout commands consumed. See doc.go.
package pica

import (
	"fmt"
	"strings"
)

// Text renders the document at its own Layout.Width: the title
// first (followed by the byline when .by or .date is set), then
// each block -- paragraphs wrapped ragged-right, headings as
// "# text", rules as "---", tables laid out, verbatim lines
// truncated, .link lines re-emitted for the wire. Blocks are
// separated by one blank line unless they were contiguous in the
// source.
func (d *Doc) Text() (string, error) {
	width := d.Layout.Width
	out := []string{TruncLine(d.Title, width)}
	if bl := d.Byline(); bl != "" {
		out = append(out, TruncLine(bl, width))
	}
	for _, b := range d.Blocks {
		lines, err := RenderBlock(b, width)
		if err != nil {
			return "", err
		}
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
		out = append(out, "", TruncLine(d.Rights, width))
	}
	return strings.Join(out, "\n") + "\n", nil
}

// RenderBlock lays out one block at the given width as the text
// writer renders it: the fixed-width lines of that block alone, no
// separator. Exported so a consumer that styles by block kind (a
// cell-grid renderer) gets byte-identical lines to Text without
// duplicating its geometry.
func RenderBlock(b Block, width int) ([]string, error) {
	switch b.Kind {
	case Para:
		return wrapParagraph(b.Text, width), nil

	case Heading:
		marker := "# "
		if b.Level == 2 {
			marker = "## "
		}
		return []string{TruncLine(marker+b.Text, width)}, nil

	case Quote:
		inner := wrapParagraph(b.Text, width-2*QuoteIndent)
		out := make([]string, len(inner), len(inner)+1)
		for i, ln := range inner {
			out[i] = strings.Repeat(" ", QuoteIndent) + ln
		}
		if b.Attrib != "" {
			out = append(out, AttribLine(b.Attrib, width))
		}
		return out, nil

	case Item:
		inner := wrapParagraph(b.Text, width-ItemIndent)
		out := make([]string, len(inner))
		for i, ln := range inner {
			if i == 0 {
				out[i] = Bullet + " " + ln
			} else {
				out[i] = strings.Repeat(" ", ItemIndent) + ln
			}
		}
		return out, nil

	case Term:
		// The label runs in: label, TermGap spaces, then the text,
		// its turnovers hanging ItemIndent runes like an item's. A
		// label that leaves too little of the line stands alone on
		// it and the text starts beneath, every line hanging.
		hang := strings.Repeat(" ", ItemIndent)
		first, runIn := TermRunIn(b.Label, width)
		if !runIn {
			out := []string{TruncLine(b.Label, width)}
			for _, ln := range wrapParagraph(b.Text, width-ItemIndent) {
				out = append(out, hang+ln)
			}
			return out, nil
		}
		inner := wrapParagraphRunIn(b.Text, first, width-ItemIndent)
		out := make([]string, len(inner))
		for i, ln := range inner {
			if i == 0 {
				out[i] = b.Label + strings.Repeat(" ", TermGap) + ln
			} else {
				out[i] = hang + ln
			}
		}
		return out, nil

	case RuleBlk:
		return []string{"---"}, nil

	case LinkBlk:
		// Wire metadata: clients do not display it, so it is exempt
		// from the width budget (truncation would corrupt the URL).
		return []string{".link " + b.Text}, nil

	case TableBlk:
		tl, err := b.Table.Layout(b.TableWidth(width))
		if err != nil {
			return nil, err
		}
		return tl.Lines(), nil

	case Pre:
		out := make([]string, len(b.Lines))
		for i, ln := range b.Lines {
			out[i] = TruncLine(ln, width)
		}
		return out, nil

	default:
		panic(fmt.Sprintf("pica: unknown block kind %d", b.Kind))
	}
}

// Monospace geometry of the structured prose blocks, shared by
// every writer so the blocks occupy identical line counts: a quote
// is inset QuoteIndent runes on BOTH sides; an item's first line
// opens with Bullet and a space, which together are ItemIndent
// runes wide, and its turnover lines hang ItemIndent runes under
// the bullet. The bullet is U+2022, covered by all four embedded
// faces (a full 600/1000 em cell in Fira Mono).
const (
	QuoteIndent = 2
	ItemIndent  = 2
	Bullet      = "•"
	// TermGap is the run-in gap: the spaces between a .term label
	// and its text on the label's line. A term's turnovers hang
	// ItemIndent runes, the item's geometry without the bullet.
	TermGap = 2
)

// TermRunIn decides a .term label's placement at the given width:
// first is the measure the text has on the label's line (the width
// less the label and TermGap), and runIn whether the text runs in
// there at all. A label that leaves less than half the width to
// the text stands on its own line, and the text starts beneath it
// -- troff's .TP rule for an over-long tag. Every writer shares the
// decision, so their line counts agree.
func TermRunIn(label string, width int) (first int, runIn bool) {
	first = width - runeLen(label) - TermGap
	return first, 2*first >= width
}

// AttribLine renders a quote attribution right-aligned to the
// quote's right margin (width - QuoteIndent): "-- WHO", truncated
// to the quote measure (width - 2*QuoteIndent) if need be.
func AttribLine(attrib string, width int) string {
	s := TruncLine("-- "+attrib, width-2*QuoteIndent)
	return strings.Repeat(" ", width-QuoteIndent-runeLen(s)) + s
}

// TruncLine hard-cuts a line to width runes. Byte length bounds rune
// length, so most lines return without allocating.
func TruncLine(s string, width int) string {
	if len(s) <= width {
		return s
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width])
}
