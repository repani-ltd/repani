// Weighted Voronoi stippling algorithm.
// Adapted from github.com/MaxHalford/pointu (MIT license).
package main

import (
	"image"
	"math"
	"math/rand"
	"sort"
)

type Point struct {
	X, Y float64
}

func (p Point) distSq(q Point) float64 {
	dx, dy := p.X-q.X, p.Y-q.Y
	return dx*dx + dy*dy
}

// --- importance sampling ---

func importanceSample(n int, gray *image.Gray, threshold uint8, rng *rand.Rand) []Point {
	bounds := gray.Bounds()
	hist := make(map[uint8][]Point)
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			intensity := gray.GrayAt(x, y).Y
			if intensity <= threshold {
				hist[intensity] = append(hist[intensity], Point{float64(x), float64(y)})
			}
		}
	}
	roulette := make([]int, threshold+1)
	roulette[0] = 256 * len(hist[0])
	for i := 1; i < len(roulette); i++ {
		roulette[i] = roulette[i-1] + (256-i)*len(hist[uint8(i)])
	}
	if roulette[len(roulette)-1] == 0 {
		// Image is all white above threshold; scatter randomly.
		pts := make([]Point, n)
		for i := range pts {
			pts[i] = Point{
				X: float64(bounds.Min.X) + rng.Float64()*float64(bounds.Dx()),
				Y: float64(bounds.Min.Y) + rng.Float64()*float64(bounds.Dy()),
			}
		}
		return pts
	}
	pts := make([]Point, n)
	for i := range pts {
		ball := rng.Intn(roulette[len(roulette)-1])
		bin := uint8(sort.SearchInts(roulette, ball))
		p := hist[bin][rng.Intn(len(hist[bin]))]
		p.X += rng.Float64()
		p.Y += rng.Float64()
		pts[i] = p
	}
	return pts
}

// --- PDF ---

type pdf [][]float64

func makePDF(gray *image.Gray) pdf {
	bounds := gray.Bounds()
	p := make(pdf, bounds.Dx())
	for x := 0; x < bounds.Dx(); x++ {
		p[x] = make([]float64, bounds.Dy())
		for y := 0; y < bounds.Dy(); y++ {
			p[x][y] = 255 - float64(gray.GrayAt(x, y).Y)
		}
	}
	return p
}

// --- kd-tree ---

type rect struct{ min, max Point }

type kdNode struct {
	p           Point
	splitByX    bool
	left, right *kdNode
}

func buildKD(pts []Point, bounds rect) *kdNode {
	var split func(pts []Point, byX bool) *kdNode
	split = func(pts []Point, byX bool) *kdNode {
		if len(pts) == 0 {
			return nil
		}
		if byX {
			sort.Slice(pts, func(i, j int) bool { return pts[i].X < pts[j].X })
		} else {
			sort.Slice(pts, func(i, j int) bool { return pts[i].Y < pts[j].Y })
		}
		med := len(pts) / 2
		return &kdNode{
			p:        pts[med],
			splitByX: byX,
			left:     split(pts[:med], !byX),
			right:    split(pts[med+1:], !byX),
		}
	}
	return split(pts, true)
}

func kdNearest(root *kdNode, bounds rect, target Point) (Point, float64) {
	var search func(node *kdNode, r rect, maxSq float64) (Point, float64)
	search = func(node *kdNode, r rect, maxSq float64) (Point, float64) {
		if node == nil {
			return Point{}, math.Inf(1)
		}
		var leftBox, rightBox rect
		var targetInLeft bool
		if node.splitByX {
			leftBox = rect{r.min, Point{node.p.X, r.max.Y}}
			rightBox = rect{Point{node.p.X, r.min.Y}, r.max}
			targetInLeft = target.X <= node.p.X
		} else {
			leftBox = rect{r.min, Point{r.max.X, node.p.Y}}
			rightBox = rect{Point{r.min.X, node.p.Y}, r.max}
			targetInLeft = target.Y <= node.p.Y
		}
		var nearNode, farNode *kdNode
		var nearBox, farBox rect
		if targetInLeft {
			nearNode, nearBox = node.left, leftBox
			farNode, farBox = node.right, rightBox
		} else {
			nearNode, nearBox = node.right, rightBox
			farNode, farBox = node.left, leftBox
		}
		best, bestSq := search(nearNode, nearBox, maxSq)
		if bestSq < maxSq {
			maxSq = bestSq
		}
		var d float64
		if node.splitByX {
			d = node.p.X - target.X
		} else {
			d = node.p.Y - target.Y
		}
		if d*d > maxSq {
			return best, bestSq
		}
		if dsq := node.p.distSq(target); dsq < bestSq {
			best, bestSq = node.p, dsq
			maxSq = bestSq
		}
		if tb, tsq := search(farNode, farBox, maxSq); tsq < bestSq {
			best, bestSq = tb, tsq
		}
		return best, bestSq
	}
	return search(root, bounds, math.Inf(1))
}

// --- Lloyd iteration ---

func lloydIteration(sites []Point, p pdf, bounds image.Rectangle, step float64) ([]Point, []float64) {
	box := rect{
		Point{float64(bounds.Min.X), float64(bounds.Min.Y)},
		Point{float64(bounds.Max.X), float64(bounds.Max.Y)},
	}
	root := buildKD(sites, box)

	type accum struct {
		wx, wy, w, n float64
	}
	acc := make(map[Point]*accum, len(sites))
	for _, s := range sites {
		acc[s] = &accum{}
	}

	for i := box.min.X; i < box.max.X; i += step {
		for j := box.min.Y; j < box.max.Y; j += step {
			pt := Point{i, j}
			nn, _ := kdNearest(root, box, pt)
			a := acc[nn]
			if a == nil {
				continue
			}
			w := p[int(i)][int(j)]
			a.wx += w * i
			a.wy += w * j
			a.w += w
			a.n++
		}
	}

	centroids := make([]Point, 0, len(sites))
	densities := make([]float64, 0, len(sites))
	for _, a := range acc {
		if a.w == 0 {
			continue
		}
		centroids = append(centroids, Point{a.wx / a.w, a.wy / a.w})
		densities = append(densities, a.w/a.n)
	}
	return centroids, densities
}

// --- stipple ---

// Stipple runs the full weighted Voronoi stippling algorithm.
func Stipple(img image.Image, nPoints, iterations int, rng *rand.Rand) ([]Point, []float64) {
	gray := imageToGray(img)
	bounds := gray.Bounds()
	points := importanceSample(nPoints, gray, 200, rng)
	p := makePDF(gray)
	var densities []float64
	for i := 0; i < iterations; i++ {
		points, densities = lloydIteration(points, p, bounds, 1.0)
	}
	return points, densities
}

func imageToGray(img image.Image) *image.Gray {
	bounds := img.Bounds()
	gray := image.NewGray(bounds)
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			gray.Set(x, y, img.At(x, y))
		}
	}
	return gray
}
