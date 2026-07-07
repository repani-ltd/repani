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
// first, then each block -- paragraphs wrapped ragged-right,
// headings as "# text", rules as "---", tables laid out, verbatim
// lines truncated, .link lines re-emitted for the wire. Blocks are
// separated by one blank line unless they were contiguous in the
// source.
func (d *Doc) Text() (string, error) {
	width := d.Layout.Width
	out := []string{truncLine(d.Title, width)}
	for _, b := range d.Blocks {
		lines, err := renderBlock(b, width)
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
	return strings.Join(out, "\n") + "\n", nil
}

// renderBlock lays out one block at the given width.
func renderBlock(b Block, width int) ([]string, error) {
	switch b.Kind {
	case Para:
		return wrapParagraph(b.Text, width), nil

	case Heading:
		return []string{truncLine("# "+b.Text, width)}, nil

	case RuleBlk:
		return []string{"---"}, nil

	case LinkBlk:
		// Wire metadata: clients do not display it, so it is exempt
		// from the width budget (truncation would corrupt the URL).
		return []string{".link " + b.Text}, nil

	case SetBlk:
		// Wire metadata, same exemption (truncation would corrupt
		// the value).
		return []string{".set " + b.Text}, nil

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
