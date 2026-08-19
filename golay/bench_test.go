package golay

import "testing"

func BenchmarkGolayDecode(b *testing.B) {
	cw := Encode(0xABC) ^ 0x00100401 // three bit errors
	b.ReportAllocs()
	for i := range b.N {
		if _, ok := Decode(cw ^ uint32(i&1)); !ok {
			b.Fatal("decode failed")
		}
	}
}
