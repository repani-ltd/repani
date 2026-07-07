// Portrait experiment: how recognizable can a 1-bit author portrait
// be at wire-feasible resolutions, across cell mappings (half-block,
// quadrant, braille) and dither algorithms?
//
//	go run ./portrait -in ../dither/testdata/portrait.jpg -crop 190,40,890,915
//
// Outputs to ./out: per-config PNG previews (aspect-correct, x6),
// block-char text renderings, and .img X/. source dumps.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func mathPow(x, g float64) float64 { return math.Pow(x, g) }

// Cell geometry from the typeset/pdf identity: a cell is 0.6 em wide
// and lineSpacing=1.25 em tall.
const (
	cellW = 0.6
	cellH = 1.25
)

// mode is a pixel-per-cell mapping.
type mode struct {
	name   string
	pxX    int // pixels per cell horizontally
	pxY    int // pixels per cell vertically
	render func(bw [][]bool) []string
}

var modes = []mode{
	{"hb", 1, 2, renderHalfBlocks},
	{"quad", 2, 2, renderQuadrants},
	{"br", 2, 4, renderBraille},
}

type algo struct {
	name   string
	kernel [][3]float64 // {dx, dy, weight}
}

var algos = []algo{
	{"thresh", nil},
	{"fs", [][3]float64{{1, 0, 7.0 / 16}, {-1, 1, 3.0 / 16}, {0, 1, 5.0 / 16}, {1, 1, 1.0 / 16}}},
	{"atkinson", [][3]float64{{1, 0, 1.0 / 8}, {2, 0, 1.0 / 8}, {-1, 1, 1.0 / 8}, {0, 1, 1.0 / 8}, {1, 1, 1.0 / 8}, {0, 2, 1.0 / 8}}},
	{"sierra", [][3]float64{{1, 0, 5.0 / 32}, {2, 0, 3.0 / 32}, {-2, 1, 2.0 / 32}, {-1, 1, 4.0 / 32}, {0, 1, 5.0 / 32}, {1, 1, 4.0 / 32}, {2, 1, 2.0 / 32}, {-1, 2, 2.0 / 32}, {0, 2, 3.0 / 32}, {1, 2, 2.0 / 32}}},
}

func main() {
	in := flag.String("in", "../dither/testdata/portrait.jpg", "input image")
	cropSpec := flag.String("crop", "", "crop x0,y0,x1,y1 (default full frame)")
	colsSpec := flag.String("cols", "40,32", "cell column counts to try")
	contrast := flag.Float64("c", 0.4, "contrast stretch")
	strength := flag.Float64("s", 0.85, "error diffusion strength")
	gamma := flag.Float64("g", 0.8, "gamma (<1 lifts midtones/skin)")
	phi := flag.Int("phi", 98, "highlight percentile mapped to white")
	outDir := flag.String("out", "out", "output directory")
	flag.Parse()

	img := load(*in)
	if *cropSpec != "" {
		r := parseCrop(*cropSpec)
		img = crop(img, r)
	}
	b := img.Bounds()
	aspect := float64(b.Dx()) / float64(b.Dy()) // w/h of the crop
	os.MkdirAll(*outDir, 0o755)

	gray := grayscale(img)

	for _, colsStr := range strings.Split(*colsSpec, ",") {
		cols, _ := strconv.Atoi(strings.TrimSpace(colsStr))
		for _, m := range modes {
			// Lines so the DISPLAYED aspect matches the crop:
			// display w:h = cols*cellW : lines*cellH.
			lines := int(float64(cols)*cellW/aspect/cellH + 0.5)
			pw, ph := cols*m.pxX, lines*m.pxY

			small := boxScale(gray, b, pw, ph)
			autoLevels(small, *gamma, *phi)
			for _, a := range algos {
				bw := dither(small, pw, ph, a, *contrast, *strength)
				tag := fmt.Sprintf("c%d_%s_%s", cols, m.name, a.name)

				writePNG(filepath.Join(*outDir, tag+".png"), bw, pw, ph, m)
				txt := m.render(bw)
				os.WriteFile(filepath.Join(*outDir, tag+".txt"),
					[]byte(strings.Join(txt, "\n")+"\n"), 0o644)
				writeImgBlock(filepath.Join(*outDir, tag+".img"), bw)

				bytes := 0
				for _, ln := range txt {
					bytes += len(ln) + 1
				}
				fmt.Printf("%-22s %3dx%-3d px  %2dx%-2d cells  wire ~%4d B\n",
					tag, pw, ph, cols, lines, bytes)
			}
		}
	}
	writeContactSheet(*outDir)
}

func load(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		panic(err)
	}
	return img
}

func parseCrop(s string) image.Rectangle {
	p := strings.Split(s, ",")
	if len(p) != 4 {
		panic("crop wants x0,y0,x1,y1")
	}
	v := make([]int, 4)
	for i := range p {
		v[i], _ = strconv.Atoi(strings.TrimSpace(p[i]))
	}
	return image.Rect(v[0], v[1], v[2], v[3])
}

func crop(img image.Image, r image.Rectangle) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			out.Set(x, y, img.At(r.Min.X+x, r.Min.Y+y))
		}
	}
	return out
}

// grayscale converts to a float luma buffer.
func grayscale(img image.Image) []float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			out[y*w+x] = 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(bl>>8)
		}
	}
	return out
}

// boxScale area-averages the source luma down to pw x ph (handles
// anisotropic pixel grids -- x and y scale independently).
func boxScale(src []float64, b image.Rectangle, pw, ph int) []float64 {
	sw, sh := b.Dx(), b.Dy()
	out := make([]float64, pw*ph)
	for y := 0; y < ph; y++ {
		sy0, sy1 := y*sh/ph, (y+1)*sh/ph
		if sy1 == sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < pw; x++ {
			sx0, sx1 := x*sw/pw, (x+1)*sw/pw
			if sx1 == sx0 {
				sx1 = sx0 + 1
			}
			sum, n := 0.0, 0
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					sum += src[sy*sw+sx]
					n++
				}
			}
			out[y*pw+x] = sum / float64(n)
		}
	}
	return out
}

// autoLevels normalizes luma so the 2nd..98th percentile spans the
// full range, then applies gamma (<1 brightens midtones). Without
// this, mid-tone skin sits at the dither threshold and the face
// drowns.
func autoLevels(buf []float64, gamma float64, phi int) {
	sorted := make([]float64, len(buf))
	copy(sorted, buf)
	slicesSort(sorted)
	lo := sorted[len(sorted)*2/100]
	hi := sorted[len(sorted)*phi/100]
	if hi-lo < 1 {
		return
	}
	for i, v := range buf {
		n := (v - lo) / (hi - lo)
		n = min(1, max(0, n))
		buf[i] = pow(n, gamma) * 255
	}
}

func slicesSort(s []float64) { sort.Float64s(s) }

func pow(x, g float64) float64 {
	if x <= 0 {
		return 0
	}
	// math.Pow without importing math for one call would be silly;
	// see import below.
	return mathPow(x, g)
}

// dither returns bw[y][x], true = ink (dark).
func dither(src []float64, w, h int, a algo, contrast, strength float64) [][]bool {
	buf := make([]float64, len(src))
	copy(buf, src)
	// Contrast stretch around mid-gray.
	for i, v := range buf {
		v += (v - 128) * contrast
		buf[i] = min(255, max(0, v))
	}
	out := make([][]bool, h)
	for y := range out {
		out[y] = make([]bool, w)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := buf[y*w+x]
			var target float64
			if v < 128 {
				out[y][x] = true // ink
				target = 0
			} else {
				target = 255
			}
			if a.kernel == nil {
				continue
			}
			err := (v - target) * strength
			for _, k := range a.kernel {
				nx, ny := x+int(k[0]), y+int(k[1])
				if nx >= 0 && nx < w && ny < h {
					buf[ny*w+nx] += err * k[2]
				}
			}
		}
	}
	return out
}

// writePNG renders the bitmap aspect-correct: each pixel becomes an
// sx x sy rectangle so the preview looks like the final display.
func writePNG(path string, bw [][]bool, pw, ph int, m mode) {
	// Display size of one pixel in em: (cellW/pxX) x (cellH/pxY).
	// Scale so the image is ~600px wide.
	exw := cellW / float64(m.pxX)
	exh := cellH / float64(m.pxY)
	s := 600.0 / (float64(pw) * exw)
	sx, sy := max(1, int(exw*s+0.5)), max(1, int(exh*s+0.5))

	img := image.NewGray(image.Rect(0, 0, pw*sx, ph*sy))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			if !bw[y][x] {
				continue
			}
			for dy := 0; dy < sy; dy++ {
				for dx := 0; dx < sx; dx++ {
					img.SetGray(x*sx+dx, y*sy+dy, color.Gray{0})
				}
			}
		}
	}
	f, _ := os.Create(path)
	png.Encode(f, img)
	f.Close()
}

func writeImgBlock(path string, bw [][]bool) {
	var b strings.Builder
	b.WriteString(".img\n")
	for _, row := range bw {
		for _, ink := range row {
			if ink {
				b.WriteByte('X')
			} else {
				b.WriteByte('.')
			}
		}
		b.WriteByte('\n')
	}
	b.WriteString(".end\n")
	os.WriteFile(path, []byte(b.String()), 0o644)
}

// --- cell renderings ---

func renderHalfBlocks(bw [][]bool) []string {
	h, w := len(bw), len(bw[0])
	var out []string
	for y := 0; y+1 < h+1; y += 2 {
		var b strings.Builder
		for x := 0; x < w; x++ {
			top := bw[y][x]
			bot := y+1 < h && bw[y+1][x]
			switch {
			case top && bot:
				b.WriteRune('█')
			case top:
				b.WriteRune('▀')
			case bot:
				b.WriteRune('▄')
			default:
				b.WriteRune(' ')
			}
		}
		out = append(out, b.String())
	}
	return out
}

// quadrant glyphs indexed by bits TL=8 TR=4 BL=2 BR=1.
var quadGlyphs = [16]rune{
	' ', '▗', '▖', '▄', '▝', '▐', '▞', '▟',
	'▘', '▚', '▌', '▙', '▀', '▜', '▛', '█',
}

func renderQuadrants(bw [][]bool) []string {
	h, w := len(bw), len(bw[0])
	at := func(y, x int) bool { return y < h && x < w && bw[y][x] }
	var out []string
	for y := 0; y < h; y += 2 {
		var b strings.Builder
		for x := 0; x < w; x += 2 {
			idx := 0
			if at(y, x) {
				idx |= 8
			}
			if at(y, x+1) {
				idx |= 4
			}
			if at(y+1, x) {
				idx |= 2
			}
			if at(y+1, x+1) {
				idx |= 1
			}
			b.WriteRune(quadGlyphs[idx])
		}
		out = append(out, b.String())
	}
	return out
}

// braille: 2x4 dots per cell, U+2800 base. Dot bit positions:
// (0,0)=1 (0,1)=2 (0,2)=4 (1,0)=8 (1,1)=16 (1,2)=32 (0,3)=64 (1,3)=128
func renderBraille(bw [][]bool) []string {
	h, w := len(bw), len(bw[0])
	at := func(y, x int) bool { return y < h && x < w && bw[y][x] }
	bits := [4][2]int{{1, 8}, {2, 16}, {4, 32}, {64, 128}}
	var out []string
	for y := 0; y < h; y += 4 {
		var b strings.Builder
		for x := 0; x < w; x += 2 {
			v := 0
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					if at(y+dy, x+dx) {
						v |= bits[dy][dx]
					}
				}
			}
			b.WriteRune(rune(0x2800 + v))
		}
		out = append(out, b.String())
	}
	return out
}

// writeContactSheet emits an HTML page showing every .txt rendering
// in monospace with line-height 1, for browser-side judgment.
func writeContactSheet(dir string) {
	entries, _ := os.ReadDir(dir)
	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><style>
body{font-family:monospace;background:#fff;color:#000;display:flex;flex-wrap:wrap;gap:24px;padding:16px}
figure{margin:0}figcaption{font:12px sans-serif;margin-bottom:4px}
pre{line-height:1;font-size:10px;margin:0;letter-spacing:0}
</style>`)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		b.WriteString("<figure><figcaption>" + e.Name() + "</figcaption><pre>")
		b.WriteString(strings.NewReplacer("&", "&amp;", "<", "&lt;").Replace(string(data)))
		b.WriteString("</pre></figure>\n")
	}
	os.WriteFile(filepath.Join(dir, "sheet.html"), []byte(b.String()), 0o644)
}
