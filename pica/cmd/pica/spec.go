// The spec and check subcommands: the two oracles that make
// reading the pica source unnecessary. spec prints the language
// reference embedded in this binary (pica.Spec — doc.go's
// package comment, so it cannot drift from the parser); check
// parses a document and reports its errors without rendering.
package main

import (
	"fmt"

	"repani.com/pica"
)

func specCmd(args []string) int {
	fs := newFlags("spec")
	out := fs.String("o", "", "output file (default stdout)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 0 {
		fmt.Fprintln(stderr, "pica spec: takes no input (the spec ships in the binary)")
		return 2
	}
	return writeOutput("spec", *out, []byte(pica.Spec()))
}

func checkCmd(args []string) int {
	doc, rc := loadDoc("check", newFlags("check"), args)
	if doc == nil {
		return rc
	}
	return 0
}
