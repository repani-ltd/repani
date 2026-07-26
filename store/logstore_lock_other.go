//go:build !unix && !windows

package store

import "os"

// lockFile is a no-op on platforms without flock or LockFileEx;
// cross-process append exclusion is unavailable there.
func lockFile(*os.File) error { return nil }
