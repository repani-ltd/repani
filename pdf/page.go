// Page content-stream builder: text blocks, positioned text, lines,
// grayscale, and the rune-usage tracking that drives font subsetting.
package pdf

import (
	"fmt"
	"strings"
)

// Page builds one PDF page content stream. Get pages into a document
// with Doc.Add. Zero value is ready to use.
type Page struct {
	buf    strings.Builder
	inText bool
	font   Font
	size   float64
	used   map[Font]map[rune]bool
}

// SetFont selects the font and size for subsequent text operations.
func (p *Page) SetFont(font Font, size float64) {
	p.font = font
	p.size = size
	if p.inText {
		fmt.Fprintf(&p.buf, "/%s %s Tf\n", string(font), ff(size))
	}
}

// TextBlock is an open BT..ET scope for runs of positioned text
// (cheaper than Text when writing many lines). End it before any
// non-text operation.
type TextBlock struct {
	page *Page
	done bool
}

// BeginText opens a text block using the page's current font.
func (p *Page) BeginText() *TextBlock {
	if p.inText {
		panic("pdf: text block already open")
	}
	p.buf.WriteString("BT\n")
	fmt.Fprintf(&p.buf, "/%s %s Tf\n", string(p.font), ff(p.size))
	p.inText = true
	return &TextBlock{page: p}
}

// Move positions the text cursor at absolute coordinates.
func (tb *TextBlock) Move(x, y float64) {
	tb.check()
	fmt.Fprintf(&tb.page.buf, "1 0 0 1 %s %s Tm\n", ff(x), ff(y))
}

// MoveRel moves the text cursor by a relative offset.
func (tb *TextBlock) MoveRel(dx, dy float64) {
	tb.check()
	fmt.Fprintf(&tb.page.buf, "%s %s Td\n", ff(dx), ff(dy))
}

// Show draws s at the current cursor position.
func (tb *TextBlock) Show(s string) {
	tb.check()
	tb.page.show(s)
}

// SetFont changes font and size within the block.
func (tb *TextBlock) SetFont(font Font, size float64) {
	tb.check()
	tb.page.SetFont(font, size)
}

// End closes the text block.
func (tb *TextBlock) End() {
	tb.check()
	tb.page.buf.WriteString("ET\n")
	tb.page.inText = false
	tb.done = true
}

func (tb *TextBlock) check() {
	if tb.done {
		panic("pdf: TextBlock used after End")
	}
}

// Text draws s at (x, y) as a one-shot operation.
func (p *Page) Text(x, y float64, s string) {
	if p.inText {
		panic("pdf: Text inside an open TextBlock")
	}
	p.buf.WriteString("BT\n")
	fmt.Fprintf(&p.buf, "/%s %s Tf\n", string(p.font), ff(p.size))
	fmt.Fprintf(&p.buf, "%s %s Td\n", ff(x), ff(y))
	p.show(s)
	p.buf.WriteString("ET\n")
}

// show emits hex-encoded Identity-H text (codepoint as CID) and
// records rune usage for subsetting. BMP-only: runes above U+FFFF
// are replaced.
func (p *Page) show(s string) {
	p.buf.WriteByte('<')
	for _, r := range s {
		if r > 0xFFFF {
			r = 0xFFFD
		}
		fmt.Fprintf(&p.buf, "%04X", r)
		p.recordRune(r)
	}
	p.buf.WriteString("> Tj\n")
}

func (p *Page) recordRune(r rune) {
	if p.used == nil {
		p.used = make(map[Font]map[rune]bool)
	}
	if p.used[p.font] == nil {
		p.used[p.font] = make(map[rune]bool)
	}
	p.used[p.font][r] = true
}

// Line strokes a line from (x1, y1) to (x2, y2).
func (p *Page) Line(x1, y1, x2, y2, width float64) {
	if p.inText {
		panic("pdf: Line inside an open TextBlock")
	}
	fmt.Fprintf(&p.buf, "%s w\n", ff(width))
	fmt.Fprintf(&p.buf, "%s %s m %s %s l S\n", ff(x1), ff(y1), ff(x2), ff(y2))
}

// Gray sets the non-stroking (text) gray level: 0 black, 1 white.
func (p *Page) Gray(level float64) {
	fmt.Fprintf(&p.buf, "%s g\n", ff(level))
}

// StrokeGray sets the stroking (line) gray level.
func (p *Page) StrokeGray(level float64) {
	fmt.Fprintf(&p.buf, "%s G\n", ff(level))
}

// Bytes finalizes and returns the content stream. Panics if a
// TextBlock is still open.
func (p *Page) Bytes() []byte {
	if p.inText {
		panic("pdf: unclosed TextBlock")
	}
	return []byte(p.buf.String())
}

// Width returns the rendered width of s in points for the given
// font and size, using per-codepoint advance widths.
func Width(s string, font Font, size float64) float64 {
	f := fontByID(font)
	total := 0
	for _, r := range s {
		if w, ok := f.CIDWidths[int(r)]; ok {
			total += w
		} else {
			total += f.DefaultWidth
		}
	}
	return float64(total) * size / 1000.0
}

// ff formats a float for PDF output (up to 4 decimals, trimmed).
func ff(v float64) string {
	s := fmt.Sprintf("%.4f", v)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}
