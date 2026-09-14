package raster

import (
	"strings"
	"testing"
)

// A terminal answer: one line or ten.
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
		if _, err := Compile(benchOneRow); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompile10(b *testing.B) {
	for b.Loop() {
		if _, err := Compile(benchTenRows); err != nil {
			b.Fatal(err)
		}
	}
}

// Compile and render to ANSI the way a terminal app would.
func BenchmarkCompileRender10(b *testing.B) {
	for b.Loop() {
		r, err := Compile(benchTenRows)
		if err != nil {
			b.Fatal(err)
		}
		_ = strings.Join(r.ANSI(), "\n")
	}
}

func BenchmarkRender10(b *testing.B) {
	r, _ := Compile(benchTenRows)
	for b.Loop() {
		_ = strings.Join(r.ANSI(), "\n")
	}
}

func BenchmarkBytes10(b *testing.B) {
	r, _ := Compile(benchTenRows)
	for b.Loop() {
		_ = r.Bytes()
	}
}

func BenchmarkRead10(b *testing.B) {
	r, _ := Compile(benchTenRows)
	bytes := r.Bytes()
	for b.Loop() {
		if _, err := Read(bytes); err != nil {
			b.Fatal(err)
		}
	}
}
