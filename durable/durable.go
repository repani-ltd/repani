// Package durable replaces a file's contents so that a crash leaves
// either the old contents or the new, never a torn mix, and the new
// contents are on disk when the call returns.
//
// It replaces ONE file. Several writes are never one transaction:
// state that must change together lives in a single file, replaced
// whole with WriteFile.
//
// WriteFile writes a temporary file with a unique name in the target's
// directory, syncs and closes it, renames it over the target, and
// syncs the directory so the rename itself survives a crash. On any
// failure before the rename the temporary file is removed and the
// target is untouched. Writers racing on one path each land whole;
// the last rename wins.
package durable

import (
	"os"
	"path/filepath"
	"runtime"
)

// WriteFile replaces path with data, mode perm, set exactly: the umask
// does not apply. An error from the final directory
// sync means the new contents are in place but may not survive a
// crash.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	f, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	fail := func(err error) error {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Chmod(perm); err != nil {
		return fail(err)
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return SyncDir(dir)
}

// SyncDir flushes dir's entries, so a file just created, renamed or
// removed in it survives a crash. On Windows, where a directory cannot
// be synced and the filesystem journals renames itself, it does
// nothing.
func SyncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}
