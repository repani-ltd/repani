//go:build windows

package store

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock on f via LockFileEx so two processes
// cannot append to the same log at once. The lock releases when the
// handle closes.
//
// Windows byte-range locks are mandatory, not advisory: a locked range
// blocks reads and writes through every other handle, not just other
// lockFile callers. The lock therefore covers one byte far beyond any
// real log size, so it acts as a pure mutex and never blocks a backup
// tool reading the log's actual bytes.
func lockFile(f *os.File) error {
	ol := &windows.Overlapped{OffsetHigh: 0x8000_0000} // byte at offset 1<<63
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, ol)
	switch err {
	case nil:
		return nil
	case windows.ERROR_LOCK_VIOLATION:
		return fmt.Errorf("%w: %s (is another kv running?)", ErrLocked, f.Name())
	default:
		return fmt.Errorf("logstore: cannot lock %s: %w", f.Name(), err)
	}
}
