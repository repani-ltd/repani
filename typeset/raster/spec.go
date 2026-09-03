package raster

import (
	_ "embed"
	"strings"
)

//go:embed doc.go
var docSource string

// Spec returns the operating reference: this package's documentation
// comment, embedded at build time, so the text is definitionally the
// spec of the compiler in the binary. A CLI over a raster format
// prints it after its own reference (tessera spec does).
func Spec() string {
	s := docSource
	if i := strings.Index(s, "/*"); i >= 0 {
		s = s[i+2:]
	}
	if i := strings.LastIndex(s, "*/"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s) + "\n"
}
