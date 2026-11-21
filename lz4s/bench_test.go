package lz4s

import (
	"bytes"
	"math/rand"
	"testing"
)

func benchInputs() map[string][]byte {
	rnd := rand.New(rand.NewSource(1))
	random := make([]byte, 4096)
	rnd.Read(random)
	text := bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog; "), 4096/45+1)[:4096]
	return map[string][]byte{"text": text, "random": random}
}

func BenchmarkCompress(b *testing.B) {
	for name, src := range benchInputs() {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(src)))
			for range b.N {
				Compress(src)
			}
		})
	}
}

func BenchmarkDecompress(b *testing.B) {
	for name, src := range benchInputs() {
		b.Run(name, func(b *testing.B) {
			comp := Compress(src)
			b.ReportAllocs()
			b.SetBytes(int64(len(src)))
			for range b.N {
				if _, ok := Decompress(comp, len(src)); !ok {
					b.Fatal("decompress failed")
				}
			}
		})
	}
}
