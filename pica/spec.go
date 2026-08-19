package typeset

import (
	_ "embed"
	"strings"
)

//go:embed doc.go
var docSource string

// Spec returns the language reference: this package's documentation
// comment, embedded at build time, so the text is definitionally
// the spec of the parser compiled into the binary. Tools print it
// (pica spec) so authors and agents learn the language without ever
// opening the package source.
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
