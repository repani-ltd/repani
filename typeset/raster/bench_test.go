package raster

import (
	"strings"
	"testing"
)

// A terminal answer: one panel of 40 by 10, one line or ten.
var bg40 = Geometry{Cols: 40, Rows: 10, Panels: 1}

const benchOneRow = ".fg cyan\nKEA\n.fg default\n+ 28°C N 5 moderate [tides] [more]\n"

var benchTenRows = func() string {
	var b strings.Builder
	b.WriteString(".bg blue\n.fill 0\n.fg white\n.at 0 2\nRESULTS · ΑΠΟΤΕΛΕΣΜΑΤΑ\n.fg default\n.bg default\n.at 2\n")
	for range 8 {
		b.WriteString(".fg yellow\nLAVRIO\n.fg default\n+ 09:30 MARMARI ON TIME [book]\n")
	}
	return b.String()
}()

func BenchmarkCompile1(b *testing.B) {
	for b.Loop() {
		if _, err := Compile(bg40, benchOneRow); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompile10(b *testing.B) {
	for b.Loop() {
		if _, err := Compile(bg40, benchTenRows); err != nil {
			b.Fatal(err)
		}
	}
}

// Compile and render to ANSI the way a terminal app would with no
// state kept between queries.
func BenchmarkCompileRender10(b *testing.B) {
	for b.Loop() {
		p, err := Compile(bg40, benchTenRows)
		if err != nil {
			b.Fatal(err)
		}
		_ = strings.Join(p.ANSI(0), "\n")
	}
}

func BenchmarkRender10(b *testing.B) {
	p, _ := Compile(bg40, benchTenRows)
	for b.Loop() {
		_ = strings.Join(p.ANSI(0), "\n")
	}
}

func BenchmarkBytes10(b *testing.B) {
	p, _ := Compile(bg40, benchTenRows)
	for b.Loop() {
		_ = p.Bytes()
	}
}

// The same, with the page and the output buffer kept between
// queries: the steady state of a terminal app.
func BenchmarkReuse10(b *testing.B) {
	p := New(bg40)
	var buf []byte
	for b.Loop() {
		if err := p.Compile(benchTenRows); err != nil {
			b.Fatal(err)
		}
		buf = buf[:0]
		for r := range p.Rows {
			buf = p.AppendANSI(buf, 0, r)
			buf = append(buf, '\n')
		}
	}
}

func BenchmarkReuse1(b *testing.B) {
	p := New(bg40)
	var buf []byte
	for b.Loop() {
		if err := p.Compile(benchOneRow); err != nil {
			b.Fatal(err)
		}
		buf = p.AppendANSI(buf[:0], 0, 0)
	}
}
