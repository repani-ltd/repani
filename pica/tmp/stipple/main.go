// Stipple encoder experiment.
//
// Faithful port of github.com/MaxHalford/pointu (MIT license)
// with anti-aliased rendering via stdlib (no gg dependency).
//
// Usage:
//
//	go run ./tmp/stipple -in image.jpg -out result.png [-points 5000] [-iter 15] [-rmin 0.5] [-rmax 4]
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"math"
	"math/rand"
	"os"
	"time"
)

// --- anti-aliased circle renderer ---

func drawCircleAA(img *image.RGBA, cx, cy, r float64, col color.RGBA) {
	if r < 0.1 {
		return
	}
	ri := int(math.Ceil(r + 1))
	bounds := img.Bounds()
	ixMin := max(int(cx)-ri, bounds.Min.X)
	ixMax := min(int(cx)+ri, bounds.Max.X-1)
	iyMin := max(int(cy)-ri, bounds.Min.Y)
	iyMax := min(int(cy)+ri, bounds.Max.Y-1)

	for py := iyMin; py <= iyMax; py++ {
		dy := float64(py) + 0.5 - cy
		for px := ixMin; px <= ixMax; px++ {
			dx := float64(px) + 0.5 - cx
			dist := math.Sqrt(dx*dx + dy*dy)
			// Coverage: 1.0 inside, 0.0 outside, smooth 1px edge.
			cov := clamp01(r - dist + 0.5)
			if cov <= 0 {
				continue
			}
			bg := img.RGBAAt(px, py)
			img.SetRGBA(px, py, blend(bg, col, cov))
		}
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func blend(bg, fg color.RGBA, a float64) color.RGBA {
	inv := 1 - a
	return color.RGBA{
		R: uint8(float64(fg.R)*a + float64(bg.R)*inv),
		G: uint8(float64(fg.G)*a + float64(bg.G)*inv),
		B: uint8(float64(fg.B)*a + float64(bg.B)*inv),
		A: 255,
	}
}

// --- rendering ---

func renderStipple(points []Point, radii []float64, srcImg image.Image, w, h int, useColor bool, dotColor color.RGBA) *image.RGBA {
	// White background (same as pointu).
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}

	for i, p := range points {
		r := radii[i]
		col := dotColor
		if useColor {
			c := srcImg.At(int(p.X), int(p.Y))
			rr, gg, bb, _ := c.RGBA()
			col = color.RGBA{uint8(rr >> 8), uint8(gg >> 8), uint8(bb >> 8), 255}
		}
		drawCircleAA(img, p.X, p.Y, r, col)
	}
	return img
}

// --- main ---

func main() {
	inPath := flag.String("in", "", "Input image path")
	outPath := flag.String("out", "", "Output PNG path")
	nPoints := flag.Int("points", 5000, "Number of stipple points")
	threshold := flag.Int("threshold", 200, "Importance sampling threshold (0-255)")
	resolution := flag.Float64("resolution", 1, "Resolution multiplier for centroid step")
	iterations := flag.Int("iter", 15, "Number of Lloyd iterations")
	rMin := flag.Float64("rmin", 0.5, "Minimum point radius")
	rMax := flag.Float64("rmax", 4, "Maximum point radius")
	useColor := flag.Bool("color", false, "Use source image color instead of fixed dot color")
	seed := flag.Int64("seed", 0, "Random seed (0 = time-based)")

	// Dot color: dark sepia by default.
	dotR := flag.Int("cr", 112, "Dot color R (0-255)")
	dotG := flag.Int("cg", 66, "Dot color G (0-255)")
	dotB := flag.Int("cb", 20, "Dot color B (0-255)")

	flag.Parse()

	if *inPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: stipple -in image.jpg -out result.png")
		os.Exit(1)
	}

	// Load image.
	f, err := os.Open(*inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s: %v\n", *inPath, err)
		os.Exit(1)
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "decode %s: %v\n", *inPath, err)
		os.Exit(1)
	}
	bounds := img.Bounds()
	fmt.Printf("input: %dx%d\n", bounds.Dx(), bounds.Dy())

	if *seed == 0 {
		*seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(*seed))

	// Convert to grayscale.
	gray := imageToGray(img)

	// Importance sample initial points.
	points := importanceSample(*nPoints, gray, uint8(*threshold), rng)
	fmt.Printf("sampled %d initial points\n", len(points))

	// Build PDF.
	p := makePDF(gray)

	// Lloyd iterations (matching pointu: first call + iterations-2 more = iterations-1 total).
	step := 1.0 / *resolution
	t0 := time.Now()
	var densities []float64
	points, densities = lloydIteration(points, p, bounds, step)
	for i := 1; i < *iterations-1; i++ {
		points, densities = lloydIteration(points, p, bounds, step)
	}
	elapsed := time.Since(t0)
	fmt.Printf("stippled %d dots in %v (%d Lloyd iterations)\n", len(points), elapsed, *iterations-1)

	// Rescale densities to radii (same as pointu's RescaleFloat64s).
	radii := rescaleFloat64s(densities, *rMin, *rMax)

	// Render.
	dotColor := color.RGBA{uint8(*dotR), uint8(*dotG), uint8(*dotB), 255}
	preview := renderStipple(points, radii, img, bounds.Dx(), bounds.Dy(), *useColor, dotColor)

	pf, err := os.Create(*outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create %s: %v\n", *outPath, err)
		os.Exit(1)
	}
	if err := png.Encode(pf, preview); err != nil {
		pf.Close()
		fmt.Fprintf(os.Stderr, "encode png: %v\n", err)
		os.Exit(1)
	}
	pf.Close()
	fmt.Printf("output: %s\n", *outPath)
}

// rescaleFloat64s maps values to [newMin, newMax] linearly.
// Same as pointu's RescaleFloat64s.
func rescaleFloat64s(vals []float64, newMin, newMax float64) []float64 {
	oldMax := 0.0
	for _, v := range vals {
		if v > oldMax {
			oldMax = v
		}
	}
	out := make([]float64, len(vals))
	if oldMax == 0 {
		for i := range out {
			out[i] = newMin
		}
		return out
	}
	for i, v := range vals {
		out[i] = newMin + (newMax-newMin)*v/oldMax
	}
	return out
}
