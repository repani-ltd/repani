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
func lockFile(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("logstore: %s is locked by another process (is another kv running?): %w", f.Name(), err)
	}
	return nil
}
