// Package durable writes files so that a crash leaves either the old
// contents or the new, never a torn mix, and the change is on disk when
// the call returns.
package durable

import (
	"os"
	"path/filepath"
)

// WriteFile replaces path with data, mode perm: it writes a temporary
// file beside path, syncs it, renames it over path, and syncs the
// directory, so a crash at any point leaves path whole.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	// OpenFile applies perm only when it creates the file; a stale
	// temporary file may have looser bits.
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	SyncDir(filepath.Dir(path))
	return nil
}

// SyncDir flushes dir's entries, so a file just created, renamed or
// removed in it survives a crash. Best-effort: some filesystems refuse
// to sync a directory.
func SyncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}
