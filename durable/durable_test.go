package durable

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// noTemps fails if dir holds anything but the names given.
func noTemps(t *testing.T, dir string, want ...string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ents {
		got = append(got, e.Name())
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("directory holds %v, want %v", got, want)
	}
}

// TestWriteFile: a write creates the file, a second replaces it, and
// nothing else is left in the directory.
func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	for _, want := range []string{"one", "two"} {
		if err := WriteFile(path, []byte(want), 0o600); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(path); err != nil || string(b) != want {
			t.Fatalf("contents %q, %v; want %q", b, err, want)
		}
	}
	noTemps(t, dir, "key")
}

// TestWriteFile_Mode: the mode is set exactly, whatever the umask and
// whatever the mode of the file replaced.
func TestWriteFile_Mode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix modes")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := os.WriteFile(path, []byte("old"), 0o666); err != nil {
		t.Fatal(err)
	}
	os.Chmod(path, 0o666)
	for _, perm := range []os.FileMode{0o600, 0o644} {
		if err := WriteFile(path, []byte("new"), perm); err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != perm {
			t.Fatalf("mode %v, %v; want %v", fi.Mode().Perm(), err, perm)
		}
	}
}

// TestWriteFile_FailureKeepsOld: a write that cannot complete leaves
// the target as it was and no temporary file behind.
func TestWriteFile_FailureKeepsOld(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on a directory in place of the target")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A rename cannot replace a non-empty directory: the write fails
	// at its last step, after the temporary file is whole.
	blocked := filepath.Join(dir, "blocked")
	os.MkdirAll(filepath.Join(blocked, "x"), 0o700)
	if err := WriteFile(blocked, []byte("new"), 0o600); err == nil {
		t.Fatal("write over a non-empty directory succeeded")
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Fatalf("other file changed: %q", b)
	}
	noTemps(t, dir, "blocked", "key")

	// A directory that cannot be written: the write fails at its first
	// step, and the target is untouched.
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	os.Chmod(dir, 0o500)
	defer os.Chmod(dir, 0o700)
	if err := WriteFile(path, []byte("new"), 0o600); err == nil {
		t.Fatal("write into a read-only directory succeeded")
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Fatalf("target changed after a failed write: %q", b)
	}
}

// TestWriteFile_Concurrent: writers racing on one path each land whole
// (the file is always one writer's data, never a mix), and no
// temporary file is left behind.
func TestWriteFile_Concurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	const writers, size = 16, 64 << 10
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			if err := WriteFile(path, bytes.Repeat([]byte{byte('a' + i)}, size), 0o600); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	b, err := os.ReadFile(path)
	if err != nil || len(b) != size || !bytes.Equal(b, bytes.Repeat(b[:1], size)) {
		t.Fatalf("file is not one writer's data whole (len %d, %v)", len(b), err)
	}
	noTemps(t, dir, "key")
}

// TestCreateFile: a create makes the file with its mode exactly; a
// second, onto the existing file, fails with fs.ErrExist and leaves it
// as it was; nothing else is left in the directory.
func TestCreateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := CreateFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CreateFile(path, []byte("two"), 0o644); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("create over an existing file: %v, want fs.ErrExist", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "one" {
		t.Fatalf("contents %q after a refused create, want one", b)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v, %v; want 0600", fi.Mode().Perm(), err)
		}
	}
	noTemps(t, dir, "key")
}

// TestCreateFile_Concurrent: of creators racing on one path exactly one
// succeeds, the rest get fs.ErrExist, and the file is the winner's
// data whole.
func TestCreateFile_Concurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	const creators, size = 16, 64 << 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := -1
	for i := range creators {
		wg.Go(func() {
			err := CreateFile(path, bytes.Repeat([]byte{byte('a' + i)}, size), 0o600)
			switch {
			case err == nil:
				mu.Lock()
				if won >= 0 {
					t.Errorf("creators %d and %d both succeeded", won, i)
				}
				won = i
				mu.Unlock()
			case !errors.Is(err, fs.ErrExist):
				t.Error(err)
			}
		})
	}
	wg.Wait()
	b, err := os.ReadFile(path)
	if err != nil || won < 0 || !bytes.Equal(b, bytes.Repeat([]byte{byte('a' + won)}, size)) {
		t.Fatalf("file is not the winner's data whole (winner %d, len %d, %v)", won, len(b), err)
	}
	noTemps(t, dir, "key")
}

// TestWriteFile_RelativePath: a bare file name writes in the working
// directory.
func TestWriteFile_RelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := WriteFile("key", []byte("v"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile("key"); string(b) != "v" {
		t.Fatalf("contents %q", b)
	}
}
