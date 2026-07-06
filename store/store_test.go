package store

import (
	"io"
	"os"
	"path/filepath"
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
	fi, err := writeTruncatedHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = fi

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
func writeTruncatedHeader(path string) (int64, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Write([]byte{0xff, 0x00, 0x00, 0x00}); err != nil {
		return 0, err
	}
	return f.Seek(0, io.SeekCurrent)
}

func TestLogStore_Rewrite(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	path := filepath.Join(t.TempDir(), "entries.log")
	ls, err := OpenLogStore(path)
	if err != nil {
		t.Fatal(err)
	}
	keep := mkEntry(t, rk, 100, "keep", "\x00v")
	drop := mkEntry(t, rk, 200, "drop", "\x00v")
	swap := mkEntry(t, rk, 300, "swap", "\x00old")
	for _, e := range []vault.Entry{keep, drop, swap} {
		if _, err := ls.Add(e); err != nil {
			t.Fatal(err)
		}
	}

	// A failing fn aborts with the log untouched.
	sentinel := io.ErrUnexpectedEOF
	err = ls.Rewrite(func(e vault.Entry) (vault.Entry, bool, error) {
		return vault.Entry{}, false, sentinel
	})
	if err != sentinel {
		t.Fatalf("aborting Rewrite: %v", err)
	}
	if ls.Len() != 3 {
		t.Fatalf("aborted Rewrite changed the set: len=%d", ls.Len())
	}

	// Keep one, drop one, replace one.
	swap2 := mkEntry(t, rk, 400, "swap", "\x00new")
	err = ls.Rewrite(func(e vault.Entry) (vault.Entry, bool, error) {
		switch e.ID() {
		case drop.ID():
			return vault.Entry{}, false, nil
		case swap.ID():
			return swap2, true, nil
		default:
			return e, true, nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	if ls.Len() != 2 || !ls.Has(keep.ID()) || !ls.Has(swap2.ID()) || ls.Has(drop.ID()) || ls.Has(swap.ID()) {
		t.Fatalf("wrong membership after rewrite: len=%d", ls.Len())
	}

	// The store stays usable: a post-rewrite Add persists.
	extra := mkEntry(t, rk, 500, "extra", "\x00v")
	if _, err := ls.Add(extra); err != nil {
		t.Fatal(err)
	}
	if err := ls.Close(); err != nil {
		t.Fatal(err)
	}

	ls2, err := OpenLogStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ls2.Close()
	if ls2.Len() != 3 || !ls2.Has(keep.ID()) || !ls2.Has(swap2.ID()) || !ls2.Has(extra.ID()) {
		t.Fatalf("reloaded: wrong membership: len=%d", ls2.Len())
	}
	if ls2.Has(drop.ID()) || ls2.Has(swap.ID()) {
		t.Fatal("reloaded: rewritten-away entries resurrected")
	}
}
