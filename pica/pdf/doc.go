/*
Package pdf is an absolutely minimal PDF writer for typeset text
pages: enough of PDF 1.3 to put subset-embedded TrueType text and
hairlines on fixed-size pages, and nothing else.

Four fonts are embedded (SIL OFL; see fonts/README.md), covering
Latin, Greek, and Cyrillic. Fira Mono Regular and Bold have a
uniform 600/1000 em advance -- so rune-counted monospace layout
(the typeset package's output) maps to exact geometry: a line of N
runes at font size S is N * 0.6 * S points wide. Fira Sans Regular
and Bold are proportional: their text is measured through Measurer
(per-codepoint advances plus GPOS pair kerning) and drawn word by
word with Page.Words.

Usage:

	var p pdf.Page
	p.SetFont(pdf.Regular, 8)
	p.Text(72, 770, "Hello, page one")

	doc := &pdf.Doc{Title: "demo", Compress: true}
	doc.Add(&p)
	os.WriteFile("out.pdf", doc.Bytes(), 0o644)

Fonts are subset per document to the runes actually shown, and a
ToUnicode CMap is emitted so text remains selectable, copyable, and
searchable in viewers. Text encoding is Identity-H with the
codepoint as CID; codepoints above the Basic Multilingual Plane are
replaced with U+FFFD.

The writer originates from an internal reporting tool and was
trimmed to this surface: subset text, hairlines, grayscale, and
link annotations (Page.Link) -- no images, no outlines, no colors.

# Press readiness (PDF/X-1a)

The output is already offset-friendly in substance: fonts embedded,
pure vector, DeviceGray only, no transparency or encryption, and
PDF/X-1a:2001 is itself based on PDF 1.3. Formal compliance, should
a print shop require it, needs a "press profile" adding roughly
100-200 lines:

  - an OutputIntent (/S /GTS_PDFX) with an embedded grayscale ICC
    profile (a dot-gain gray profile is a few hundred bytes)
  - TrimBox on every page (equal to MediaBox; nothing bleeds)
  - Info keys: /GTS_PDFXVersion (PDF/X-1a:2001), /Trapped /False,
    /Title, and the REQUIRED CreationDate/ModDate -- stamp a fixed
    epoch date to keep byte-determinism
  - omit link annotations (PDF/X wants annotations outside the trim
    area; URIs are meaningless on paper)
  - validate with veraPDF

Envelope metadata only -- the rendered marks stay byte-identical --
so a flag or option on Doc would not violate the self-contained-
document rule. Imposition, crop marks, and bleed remain the print
shop's job.
*/
package pdf
