// Sierra-3 dithering with column-delta zero-run RLE compression.
// Auto-sizes image to fill QC page budget via binary search.
//
// Usage:
//
//	go run ./tmp/dither -in image.jpg -out result.png [-s 0.9] [-c 0.5]
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"

	"golang.org/x/image/draw"
)

// Sierra-3 error diffusion kernel.
var kernel = [3][5]float64{
	{0, 0, 0, 5.0 / 32, 3.0 / 32},
	{2.0 / 32, 4.0 / 32, 5.0 / 32, 4.0 / 32, 2.0 / 32},
	{0, 2.0 / 32, 3.0 / 32, 2.0 / 32, 0},
}

const centerX = 2

func main() {
	inPath := flag.String("in", "", "Input image")
	outPath := flag.String("out", "", "Output PNG")
	strength := flag.Float64("s", 0.9, "Error diffusion strength (0-1)")
	contrast := flag.Float64("c", 0.5, "Contrast stretch (0-1)")
	budget := flag.Int("budget", 15000, "Wire budget in bytes (header + compressed data)")
	flag.Parse()

	if *inPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: dither -in image.jpg -out result.png")
		os.Exit(1)
	}

	src := loadImage(*inPath)
	sb := src.Bounds()
	aspect := float64(sb.Dx()) / float64(sb.Dy())
	fmt.Printf("input: %dx%d\n", sb.Dx(), sb.Dy())

	wireBudget := *budget - 4 // subtract 4-byte header

	// Binary search for max resolution that fits budget.
	// Start with an optimistic estimate: assume ~65% compression ratio.
	// Raw pixels = budget * 8 / 0.65
	loPixels := wireBudget * 8               // pessimistic: no compression
	hiPixels := int(float64(loPixels) / 0.5) // optimistic: 50% compression

	var bestW, bestH int
	var bestBW *image.Gray
	var bestEnc []byte

	for iter := 0; iter < 6; iter++ {
		midPixels := (loPixels + hiPixels) / 2
		h := int(math.Sqrt(float64(midPixels) / aspect))
		w := int(float64(h) * aspect)
		if w < 1 || h < 1 {
			hiPixels = midPixels
			continue
		}

		scaled := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), src, sb, draw.Src, nil)
		bw := dither(scaled, *contrast, *strength)
		raw := packBits(bw)
		enc := colDeltaRLE(raw, w, h)

		fmt.Printf("  try %dx%d (%d px): raw %d, rle %d", w, h, w*h, len(raw), len(enc))

		if len(enc) <= wireBudget {
			fmt.Printf(" <= %d OK\n", wireBudget)
			bestW, bestH, bestBW, bestEnc = w, h, bw, enc
			loPixels = midPixels + 1
		} else {
			fmt.Printf(" > %d OVER\n", wireBudget)
			hiPixels = midPixels - 1
		}
	}

	if bestBW == nil {
		fmt.Fprintln(os.Stderr, "could not fit any resolution in budget")
		os.Exit(1)
	}

	// Write PNG preview.
	of, _ := os.Create(*outPath)
	png.Encode(of, bestBW)
	of.Close()

	// Write wire format.
	wirePath := (*outPath)[:len(*outPath)-len(".png")] + ".qdi"
	hdr := []byte{byte(bestW), byte(bestW >> 8), byte(bestH), byte(bestH >> 8)}
	os.WriteFile(wirePath, append(hdr, bestEnc...), 0644)

	ratio := float64(len(bestEnc)) * 100 / float64((bestW*bestH+7)/8)
	fmt.Printf("result: %dx%d, wire %d bytes (%.0f%% of raw), s=%.2f c=%.2f\n",
		bestW, bestH, 4+len(bestEnc), ratio, *strength, *contrast)
}

func loadImage(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "decode: %v\n", err)
		os.Exit(1)
	}
	return img
}

func dither(img *image.RGBA, contrast, strength float64) *image.Gray {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	gray := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			v := (0.3*float64(c.R) + 0.58*float64(c.G) + 0.11*float64(c.B)) * float64(c.A) / 255
			v += (v - 128) * contrast
			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			gray[y*w+x] = v
		}
	}

	out := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := gray[y*w+x]
			var bw uint8
			if v >= 128 {
				bw = 255
			}
			out.Pix[y*out.Stride+x] = bw
			err := (v - float64(bw)) * strength
			for ky := 0; ky < 3; ky++ {
				for kx := 0; kx < 5; kx++ {
					wt := kernel[ky][kx]
					if wt == 0 {
						continue
					}
					nx, ny := x+kx-centerX, y+ky
					if nx >= 0 && nx < w && ny < h {
						gray[ny*w+nx] += err * wt
					}
				}
			}
		}
	}
	return out
}

func packBits(img *image.Gray) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rowBytes := (w + 7) / 8
	out := make([]byte, rowBytes*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if img.Pix[y*img.Stride+x] == 0 {
				out[y*rowBytes+x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	return out
}

func colDeltaRLE(raw []byte, width, height int) []byte {
	rowBytes := (width + 7) / 8

	// Transpose to column-major.
	tr := make([]byte, len(raw))
	for col := 0; col < rowBytes; col++ {
		for row := 0; row < height; row++ {
			tr[col*height+row] = raw[row*rowBytes+col]
		}
	}

	// Delta encode each column.
	for col := 0; col < rowBytes; col++ {
		base := col * height
		for i := height - 1; i >= 1; i-- {
			tr[base+i] ^= tr[base+i-1]
		}
	}

	// Zero-run RLE.
	var out []byte
	for i := 0; i < len(tr); {
		if tr[i] != 0 {
			out = append(out, tr[i])
			i++
			continue
		}
		j := i
		for j < len(tr) && tr[j] == 0 {
			j++
		}
		run := j - i
		for run > 0 {
			n := run
			if n > 255 {
				n = 255
			}
			out = append(out, 0x00, byte(n))
			run -= n
		}
		i = j
	}
	return out
}
