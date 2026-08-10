// The text writer: renders a Doc to the fixed-width monospace page
// the document describes. Its typographic identity is fixed:
// ragged-right paragraphs, verbatim blocks truncated at width,
// layout commands consumed. See doc.go.
package typeset

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
	out := []string{truncLine(d.Title, width)}
	if bl := d.Byline(); bl != "" {
		out = append(out, truncLine(bl, width))
	}
	h := defaultHyphenator
	for _, b := range d.Blocks {
		lines, err := renderBlock(b, width, h)
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
		out = append(out, "", truncLine(d.Rights, width))
	}
	return strings.Join(out, "\n") + "\n", nil
}

// renderBlock lays out one block at the given width.
func renderBlock(b Block, width int, h *hyphenator) ([]string, error) {
	switch b.Kind {
	case Para:
		return wrapParagraph(b.Text, width, h), nil

	case Heading:
		marker := "# "
		if b.Level == 2 {
			marker = "## "
		}
		return []string{truncLine(marker+b.Text, width)}, nil

	case Quote:
		inner := wrapParagraph(b.Text, width-2*quoteIndent, h)
		out := make([]string, len(inner), len(inner)+1)
		for i, ln := range inner {
			out[i] = strings.Repeat(" ", quoteIndent) + ln
		}
		if b.Attrib != "" {
			out = append(out, attribLine(b.Attrib, width))
		}
		return out, nil

	case Item:
		inner := wrapParagraph(b.Text, width-itemIndent, h)
		out := make([]string, len(inner))
		for i, ln := range inner {
			if i == 0 {
				out[i] = bullet + " " + ln
			} else {
				out[i] = "  " + ln
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
			out[i] = truncLine(ln, width)
		}
		return out, nil

	default:
		panic(fmt.Sprintf("typeset: unknown block kind %d", b.Kind))
	}
}

// Monospace indents for the structured prose blocks: a quote is
// inset quoteIndent runes on BOTH sides; an item hangs its
// continuation lines itemIndent runes under the bullet. Writers
// share these so the blocks occupy identical line counts. The
// bullet is U+2022, covered by all four embedded faces (a full
// 600/1000 em cell in Fira Mono).
const (
	quoteIndent = 2
	itemIndent  = 2
	bullet      = "•"
)

// attribLine renders a quote attribution right-aligned to the
// quote's right margin (width - quoteIndent): "-- WHO", truncated
// to the quote measure if need be.
func attribLine(attrib string, width int) string {
	s := truncLine("-- "+attrib, width-2*quoteIndent)
	return strings.Repeat(" ", width-quoteIndent-runeLen(s)) + s
}

// truncLine hard-cuts a line to width runes. Byte length bounds rune
// length, so most lines return without allocating.
func truncLine(s string, width int) string {
	if len(s) <= width {
		return s
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width])
}
