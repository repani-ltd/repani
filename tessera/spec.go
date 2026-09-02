package tessera

import (
	_ "embed"
	"strings"
)

//go:embed doc.go
var docSource string

// Spec returns the operating reference: this package's documentation
// comment, embedded at build time, so the text is definitionally the
// spec of the compiler in the binary. The CLI prints it (tessera spec)
// with its own usage appended.
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
