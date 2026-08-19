package ascon

import "testing"

func BenchmarkXOF(b *testing.B) {
	for _, n := range []int{16, 1024} {
		b.Run(itoa(n), func(b *testing.B) {
			data := make([]byte, n)
			var out [XOFSize]byte
			b.ReportAllocs()
			b.SetBytes(int64(n))
			for range b.N {
				XOF(data, out[:])
			}
		})
	}
}

func BenchmarkSeal(b *testing.B) {
	a, _ := NewAEAD(make([]byte, AEADKeySize))
	nonce := make([]byte, AEADNonceSize)
	pt := make([]byte, 1024)
	dst := make([]byte, 0, len(pt)+AEADTagSize)
	b.ReportAllocs()
	b.SetBytes(int64(len(pt)))
	for range b.N {
		a.Seal(dst, nonce, pt, nil)
	}
}

func BenchmarkOpen(b *testing.B) {
	a, _ := NewAEAD(make([]byte, AEADKeySize))
	nonce := make([]byte, AEADNonceSize)
	ct := a.Seal(nil, nonce, make([]byte, 1024), nil)
	dst := make([]byte, 0, len(ct))
	b.ReportAllocs()
	b.SetBytes(1024)
	for range b.N {
		if _, err := a.Open(dst, nonce, ct, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for ; n > 0; n /= 10 {
		i--
		buf[i] = byte('0' + n%10)
	}
	return string(buf[i:])
}
