/*
Package pdf is an absolutely minimal PDF writer for monospace text
pages: enough of PDF 1.3 to put subset-embedded TrueType text and
hairlines on fixed-size pages, and nothing else.

Two fonts are embedded, Fira Mono Regular and Bold (SIL OFL; see
fonts/README.md), covering Latin, Greek, and Cyrillic with a uniform
600/1000 em advance -- so rune-counted monospace layout (the typeset
package's output) maps to exact geometry: a line of N runes at font
size S is N * 0.6 * S points wide.

Usage:

	var p pdf.Page
	p.SetFont(pdf.Regular, 8)
	tb := p.BeginText()
	tb.Move(72, 770)
	tb.Show("Hello, page one")
	tb.End()

	doc := &pdf.Doc{Title: "demo", Compress: true}
	doc.Add(&p)
	os.WriteFile("out.pdf", doc.Bytes(), 0o644)

Fonts are subset per document to the runes actually shown, and a
ToUnicode CMap is emitted so text remains selectable, copyable, and
searchable in viewers. Text encoding is Identity-H with the
codepoint as CID; codepoints above the Basic Multilingual Plane are
replaced with U+FFFD.

The writer originates from an internal reporting tool and was
trimmed to this surface: no images, no links, no outlines, no
colors beyond grayscale.
*/
package pdf
