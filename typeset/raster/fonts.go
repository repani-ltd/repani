package raster

import (
	"embed"
	"encoding/base64"
	"fmt"
)

// The face the HTML renderer embeds: JuliaMono (SIL Open Font
// License, fonts/OFL.txt), the one open monospace family found to
// hold every glyph of the cell repertoire (2026-09-05: JetBrains
// Mono, Fira Mono, Source Code Pro, Cascadia, Hack and Iosevka each
// lack some of the weather set and the marks; DejaVu Sans Mono has
// all but advances 0.602em). Its advance is 0.6em, so a cell is a
// whole pixel at 15px and 20px. Each face is subset to the
// repertoire -- the space included, since a blank cell is a glyph
// too -- with every layout feature stripped, so a page draws the
// same glyphs in every browser without a network and the browser
// shapes nothing. Any glyph a subset lacked would be drawn from the
// next face in the stack, whose advance differs by a hair, and
// every cell of it would move the rest of the row: a cell is a
// glyph, one to one, and the subset holds every glyph a page can
// show, blanks first. Regular is what HTMLDocument embeds; Bold and
// Italic are here for a renderer that says hierarchy with weight.
// The subsets are made with pyftsubset --layout-features=''
// --no-hinting, about 12K a face.
//
//go:embed fonts/*.woff2
var fontFiles embed.FS

// fontFace returns an @font-face rule for one weight and style,
// the file as a data URI.
func fontFace(file, weight, style string) string {
	data, err := fontFiles.ReadFile("fonts/" + file)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("@font-face { font-family: \"JuliaMono\"; font-weight: %s; font-style: %s; src: url(data:font/woff2;base64,%s) format(\"woff2\"); }\n",
		weight, style, base64.StdEncoding.EncodeToString(data))
}

// FontCSS is the embedded regular face, which HTMLDocument always
// includes.
func FontCSS() string { return fontFace("JuliaMono-Regular.woff2", "400", "normal") }
