package durable

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFile: a write creates the file with its mode, a second
// replaces it, a stale temporary file's looser mode does not carry
// over, and no temporary file is left behind.
func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", []byte("stale"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path+".tmp", 0o666); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "two" {
		t.Fatalf("contents %q, %v; want two", b, err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v; want 0600", fi.Mode().Perm(), err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file left behind: %v", err)
	}
}
