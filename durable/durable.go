// Package durable writes a file so that a crash leaves either the old
// contents or the new, never a torn mix, and the new contents are on
// disk when the call returns.
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
//
// CreateFile is the same write for a file that must never replace
// another, such as a key that cannot be recovered: the whole temporary
// file is hard-linked to the target, which fails if the target exists.
// Of creators racing on one path exactly one succeeds.
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
	dir, tmp, err := writeTemp(path, data, perm)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return SyncDir(dir)
}

// CreateFile creates path with data, mode perm, set exactly, and fails
// with an error wrapping fs.ErrExist if path exists, leaving it as it
// was. Like WriteFile, a crash leaves path absent or whole. The
// filesystem must support hard links, as every local filesystem Go
// runs on does.
func CreateFile(path string, data []byte, perm os.FileMode) error {
	dir, tmp, err := writeTemp(path, data, perm)
	if err != nil {
		return err
	}
	err = os.Link(tmp, path)
	os.Remove(tmp)
	if err != nil {
		return err
	}
	return SyncDir(dir)
}

// writeTemp writes data, mode perm, to a new temporary file beside
// path, synced and closed, and returns path's directory and the
// temporary file's name. On failure it leaves no temporary file.
func writeTemp(path string, data []byte, perm os.FileMode) (dir, tmp string, err error) {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	f, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return "", "", err
	}
	tmp = f.Name()
	fail := func(err error) (string, string, error) {
		f.Close()
		os.Remove(tmp)
		return "", "", err
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
		return "", "", err
	}
	return dir, tmp, nil
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
