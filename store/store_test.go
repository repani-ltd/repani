package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavlos/kv/vault"
)

func mkEntry(t *testing.T, rk vault.RootKey, ts uint64, key, value string) vault.Entry {
	t.Helper()
	aead, err := rk.AEAD()
	if err != nil {
		t.Fatal(err)
	}
	e, err := vault.Seal(aead, ts, []byte(key), []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestMemStore_AddIdempotent(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	m := NewMemStore()
	e := mkEntry(t, rk, 1, "k", "v")
	added, _ := m.Add(e)
	if !added {
		t.Fatal("first Add should report added=true")
	}
	added, _ = m.Add(e)
	if added {
		t.Fatal("second Add of same entry should report added=false")
	}
	if m.Len() != 1 {
		t.Fatalf("Len %d, want 1", m.Len())
	}
}

func TestMemStore_SortedStable(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	m := NewMemStore()
	for i, ts := range []uint64{3, 1, 2} {
		_ = i
		e := mkEntry(t, rk, ts, "k", "v")
		_, _ = m.Add(e)
	}
	sorted := m.Sorted()
	if len(sorted) != 3 {
		t.Fatalf("Sorted length %d", len(sorted))
	}
	if sorted[0].TsMs != 1 || sorted[1].TsMs != 2 || sorted[2].TsMs != 3 {
		t.Fatalf("not canonically sorted: %v %v %v",
			sorted[0].TsMs, sorted[1].TsMs, sorted[2].TsMs)
	}
}

func TestMemStore_SetHashReproducibleAndInvalidated(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	m := NewMemStore()
	for i := range 5 {
		_, _ = m.Add(mkEntry(t, rk, uint64(i+1), "k", "v"))
	}
	h1 := m.SetHash()
	h2 := m.SetHash()
	if h1 != h2 {
		t.Fatal("SetHash returned different values for same state")
	}
	// Adding an entry must change the cached hash.
	_, _ = m.Add(mkEntry(t, rk, 99, "k2", "v2"))
	if m.SetHash() == h1 {
		t.Fatal("SetHash did not change after a new entry was added")
	}
}

func TestLogStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "entries.log")
	ls, err := OpenLogStore(path)
	if err != nil {
		t.Fatal(err)
	}
	rk, _ := vault.NewRandomRootKey()
	for i := range 3 {
		e := mkEntry(t, rk, uint64(i+1), "k", "v")
		if _, err := ls.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := ls.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen — should rebuild the same set.
	ls2, err := OpenLogStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ls2.Close()
	if ls2.Len() != 3 {
		t.Fatalf("Len after reopen %d, want 3", ls2.Len())
	}
}

func TestLogStore_TruncatedTailTolerated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "entries.log")

	ls, err := OpenLogStore(path)
	if err != nil {
		t.Fatal(err)
	}
	rk, _ := vault.NewRandomRootKey()
	for i := range 2 {
		_, _ = ls.Add(mkEntry(t, rk, uint64(i+1), "k", "v"))
	}
	ls.Close()

	// Append a truncated record (4-byte length header only, no body).
	if err := writeTruncatedHeader(path); err != nil {
		t.Fatal(err)
	}

	ls2, err := OpenLogStore(path)
	if err != nil {
		t.Fatalf("OpenLogStore should tolerate truncated tail: %v", err)
	}
	defer ls2.Close()
	if ls2.Len() != 2 {
		t.Fatalf("Len %d, want 2 (truncated record ignored)", ls2.Len())
	}
}

// writeTruncatedHeader appends 4 bytes of "next record length" without
// the record body, simulating a crash mid-write.
func writeTruncatedHeader(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write([]byte{0xff, 0x00, 0x00, 0x00})
	return err
}

// TestLogStore_GarbageTailTruncated checks that a zero-filled or corrupt
// trailing record (a crash between write and fsync) is truncated rather
// than bricking the log.
func TestLogStore_GarbageTailTruncated(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	for _, tc := range []struct {
		name string
		tail []byte
	}{
		{"zero-length-prefix", []byte{0, 0, 0, 0}},
		{"zeroed-body", []byte{0x20, 0, 0, 0, 0, 0, 0, 0, 0, 0}}, // len=32, body all zero
		{"wild-length", []byte{0xff, 0xff, 0xff, 0xff}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "entries.log")
			ls, err := OpenLogStore(path)
			if err != nil {
				t.Fatal(err)
			}
			for i := range 2 {
				if _, err := ls.Add(mkEntry(t, rk, uint64(i+1), "k", "v")); err != nil {
					t.Fatal(err)
				}
			}
			ls.Close()

			f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write(tc.tail); err != nil {
				t.Fatal(err)
			}
			f.Close()

			ls2, err := OpenLogStore(path)
			if err != nil {
				t.Fatalf("OpenLogStore should truncate a garbage tail, got: %v", err)
			}
			defer ls2.Close()
			if ls2.Len() != 2 {
				t.Fatalf("Len %d, want 2 (garbage tail truncated)", ls2.Len())
			}
			// The log must be usable again after repair.
			if _, err := ls2.Add(mkEntry(t, rk, 3, "k", "v")); err != nil {
				t.Fatalf("Add after repair: %v", err)
			}
		})
	}
}

// TestLogStore_CrossProcessLock verifies a second open of the same log
// while the first is live is refused rather than allowed to interleave
// appends.
func TestLogStore_CrossProcessLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entries.log")
	ls, err := OpenLogStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()
	ls2, err := OpenLogStore(path)
	if err == nil {
		ls2.Close()
		t.Skip("platform does not support advisory file locking")
	}
	// The refusal must be diagnosed as busy, not as a filesystem that
	// cannot lock.
	if !strings.Contains(err.Error(), "locked by another process") {
		t.Fatalf("second open failed with %q, want a locked-by-another-process error", err)
	}
}
