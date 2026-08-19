package fact

import (
	_ "embed"
	"strings"
)

//go:embed doc.go
var docSource string

// Spec returns the format reference: this package's documentation
// comment, embedded at build time, so the text is definitionally
// the spec of the parser compiled into the binary (the "tools
// explain themselves" rule). The CLI prints it (fact spec) with
// its own usage appended. The normative standard is SPEC.t.
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
