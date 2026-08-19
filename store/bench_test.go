package store

import (
	"fmt"
	"path/filepath"
	"testing"

	"repani.com/kv/vault"
)

// benchEntries returns n distinct sealed entries of realistic size.
func benchEntries(b *testing.B, n int) []vault.Entry {
	b.Helper()
	rk, err := vault.NewRandomRootKey()
	if err != nil {
		b.Fatal(err)
	}
	aead, err := rk.AEAD()
	if err != nil {
		b.Fatal(err)
	}
	out := make([]vault.Entry, n)
	for i := range n {
		e, err := vault.Seal(aead, uint64(i+1),
			fmt.Appendf(nil, "key/%06d", i), []byte("value-0123456789abcdef"))
		if err != nil {
			b.Fatal(err)
		}
		out[i] = e
	}
	return out
}

// benchLog writes n entries into a fresh log and returns its path.
func benchLog(b *testing.B, n int) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "entries.log")
	ls, err := OpenLogStore(path, 0)
	if err != nil {
		b.Fatal(err)
	}
	for _, e := range benchEntries(b, n) {
		if _, err := ls.Add(e); err != nil {
			b.Fatal(err)
		}
	}
	if err := ls.Close(); err != nil {
		b.Fatal(err)
	}
	return path
}

// BenchmarkReplay measures OpenLogStore on an existing log: the fixed
// cost every CLI invocation pays before its first operation.
func BenchmarkReplay(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			path := benchLog(b, n)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				ls, err := OpenLogStore(path, 0)
				if err != nil {
					b.Fatal(err)
				}
				if err := ls.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAddBatch measures durable batch appends of a pull-page-sized
// group: the per-entry cost when the fsync is amortized.
func BenchmarkAddBatch(b *testing.B) {
	const batch = 100
	path := filepath.Join(b.TempDir(), "entries.log")
	ls, err := OpenLogStore(path, 0)
	if err != nil {
		b.Fatal(err)
	}
	defer ls.Close()
	entries := benchEntries(b, 100000)
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		lo := (i * batch) % (len(entries) - batch)
		if _, err := ls.AddBatch(entries[lo : lo+batch]); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

// BenchmarkAdd measures single durable appends: the fsync floor of one
// local put.
func BenchmarkAdd(b *testing.B) {
	path := filepath.Join(b.TempDir(), "entries.log")
	ls, err := OpenLogStore(path, 0)
	if err != nil {
		b.Fatal(err)
	}
	defer ls.Close()
	entries := benchEntries(b, 100000)
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		if _, err := ls.Add(entries[i%len(entries)]); err != nil {
			b.Fatal(err)
		}
		i++
	}
}
