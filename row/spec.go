package row

import (
	_ "embed"
	"strings"
)

//go:embed doc.go
var docSource string

// Spec returns the reference: this package's documentation comment,
// embedded at build time, so it is definitionally the spec of the
// code compiled into the binary ("tools explain themselves"). row spec
// prints it. The normative standard is ROW.t.
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
