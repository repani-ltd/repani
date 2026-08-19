// Page content-stream builder: text blocks, positioned text, lines,
// grayscale, and the rune-usage tracking that drives font subsetting.
package pdf

import (
	"fmt"
	"strings"

	"repani.com/pica/pdf/ttf"
)

// Page builds one PDF page content stream. Get pages into a document
// with Doc.Add. The zero value is an empty page; call SetFont before
// the first text operation.
type Page struct {
	buf    strings.Builder
	font   Font
	size   float64
	used   map[Font]map[rune]bool
	annots []linkAnnot
}

// linkAnnot is a clickable URI region on the page.
type linkAnnot struct {
	x0, y0, x1, y1 float64
	url            string
}

// Link makes the rectangle (x0,y0)-(x1,y1) a clickable link to url.
// It draws nothing; pair it with Text for the visible anchor.
func (p *Page) Link(x0, y0, x1, y1 float64, url string) {
	p.annots = append(p.annots, linkAnnot{x0, y0, x1, y1, url})
}

// SetFont selects the font and size for subsequent text operations.
func (p *Page) SetFont(font Font, size float64) {
	p.font = font
	p.size = size
}

// Text draws s at (x, y) in the current font.
func (p *Page) Text(x, y float64, s string) {
	p.buf.WriteString("BT\n")
	fmt.Fprintf(&p.buf, "/%s %s Tf\n", string(p.font), ff(p.size))
	fmt.Fprintf(&p.buf, "%s %s Td\n", ff(x), ff(y))
	p.show(s)
	p.buf.WriteString("ET\n")
}

// show emits s as a TJ array of hex-encoded Identity-H runs
// (codepoint as CID) split by kerning adjustments, and records rune
// usage for subsetting. BMP-only: runes above U+FFFF are replaced.
func (p *Page) show(s string) {
	p.buf.WriteString("[ ")
	p.tjRuns(s)
	p.buf.WriteString(" ] TJ\n")
}

// tjRuns writes the TJ elements for s: hex runs split wherever a
// kerning pair applies. TJ subtracts its numbers from the advance,
// so a kern of k (negative tightens) is emitted as -k.
func (p *Page) tjRuns(s string) {
	f := fontByID(p.font)
	prev := rune(-1)
	start := 0
	for i, r := range s {
		if prev >= 0 {
			if k := f.Kern(prev, r); k != 0 {
				p.hexString(s[start:i])
				fmt.Fprintf(&p.buf, " %d ", -k)
				start = i
			}
		}
		prev = r
	}
	p.hexString(s[start:])
}

// hexString writes s as an Identity-H hex string, recording rune
// usage for subsetting.
func (p *Page) hexString(s string) {
	p.buf.WriteByte('<')
	for _, r := range s {
		if r > 0xFFFF {
			r = 0xFFFD
		}
		fmt.Fprintf(&p.buf, "%04X", r)
		p.recordRune(r)
	}
	p.buf.WriteByte('>')
}

// Words draws words on one line starting at (x, y) in the current
// font, advancing gaps[i] thousandths of an em between words[i] and
// words[i+1] (so len(gaps) == len(words)-1). Identity-H text ignores
// the Tw word-spacing operator, so justification is expressed as TJ
// position adjustments -- integers, keeping output deterministic.
func (p *Page) Words(x, y float64, words []string, gaps []int) {
	if len(words) == 0 {
		return
	}
	p.buf.WriteString("BT\n")
	fmt.Fprintf(&p.buf, "/%s %s Tf\n", string(p.font), ff(p.size))
	fmt.Fprintf(&p.buf, "%s %s Td\n", ff(x), ff(y))
	p.buf.WriteString("[ ")
	for i, w := range words {
		if i > 0 {
			// TJ subtracts the number from the advance; negative
			// values move right by gap/1000 em.
			fmt.Fprintf(&p.buf, " %d ", -gaps[i-1])
		}
		p.tjRuns(w)
	}
	p.buf.WriteString(" ] TJ\nET\n")
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

// Bytes finalizes and returns the content stream.
func (p *Page) Bytes() []byte {
	return []byte(p.buf.String())
}

// EmWidth returns the font's default advance in ems (0.6 for Fira
// Mono). Monospace layout math belongs on this number, not on a
// caller-side constant, so geometry follows the embedded font.
func EmWidth(font Font) float64 {
	return float64(fontByID(font).DefaultWidth) / 1000.0
}

// Width returns the rendered width of s in points for the given
// font and size, using per-codepoint advance widths plus pair
// kerning, matching what Text draws.
func Width(s string, font Font, size float64) float64 {
	return float64(widthUnits(s, fontByID(font))) * size / 1000.0
}

// widthUnits measures s in thousandths of an em: advance widths plus
// kerning between adjacent runes. This is the measurement that TJ
// rendering reproduces exactly.
func widthUnits(s string, f *ttf.TTFont) int {
	total := 0
	prev := rune(-1)
	for _, r := range s {
		if w, ok := f.CIDWidths[int(r)]; ok {
			total += w
		} else {
			total += f.DefaultWidth
		}
		if prev >= 0 {
			total += f.Kern(prev, r)
		}
		prev = r
	}
	return total
}

// Measurer reports text widths for one font in thousandths of an
// em, independent of point size. It satisfies typeset.Measurer
// structurally, connecting the line breakers to real font metrics.
type Measurer struct{ f *ttf.TTFont }

// Measure returns the Measurer for an embedded font.
func Measure(font Font) Measurer { return Measurer{fontByID(font)} }

// Width returns the advance width of s in thousandths of an em,
// including pair kerning between adjacent runes.
func (m Measurer) Width(s string) int {
	return widthUnits(s, m.f)
}

// Space returns the interword space advance in thousandths of an em.
func (m Measurer) Space() int {
	if w, ok := m.f.CIDWidths[' ']; ok {
		return w
	}
	return m.f.DefaultWidth
}

// AvgAdvance returns the font's average lowercase a-z advance in
// thousandths of an em: the "characters per line" unit that lets a
// proportional layout honor a rune-count .width. For a monospace
// font this equals the uniform advance.
func AvgAdvance(font Font) int {
	f := fontByID(font)
	total := 0
	for r := 'a'; r <= 'z'; r++ {
		if w, ok := f.CIDWidths[int(r)]; ok {
			total += w
		} else {
			total += f.DefaultWidth
		}
	}
	return total / 26
}

// ff formats a float for PDF output (up to 4 decimals, trimmed).
func ff(v float64) string {
	s := fmt.Sprintf("%.4f", v)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}
