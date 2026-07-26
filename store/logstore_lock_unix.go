//go:build unix

package store

import (
	"fmt"
	"os"
	"syscall"
)

// lockFile takes an exclusive, non-blocking advisory lock on f so two
// processes cannot append to the same log at once (which would interleave
// records and corrupt it). The lock releases automatically when f closes.
//
// flock is used rather than POSIX fcntl locks deliberately: fcntl locks
// do not conflict between descriptors in one process and are all dropped
// when any descriptor to the file closes, while a flock lock belongs to
// the open file description, so a double-open within one process is
// refused too. flock cannot be relied on to exclude a writer on another
// machine over a network filesystem; keep the data directory on a local
// filesystem.
func lockFile(f *os.File) error {
	var err error
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err != syscall.EINTR {
			break
		}
	}
	switch err {
	case nil:
		return nil
	case syscall.EWOULDBLOCK:
		return fmt.Errorf("%w: %s (is another kv running?)", ErrLocked, f.Name())
	default:
		// ENOTSUP, EOPNOTSUPP, ENOLCK, ...: the filesystem cannot
		// lock, which is not the same thing as busy.
		return fmt.Errorf("logstore: cannot lock %s (filesystem without flock support?): %w", f.Name(), err)
	}
}
