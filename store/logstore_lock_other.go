//go:build !unix

package store

import "os"

// lockFile is a no-op on platforms without flock; cross-process append
// exclusion is unavailable there.
func lockFile(*os.File) error { return nil }
