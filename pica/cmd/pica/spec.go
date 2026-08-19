// The spec and check subcommands: the two oracles that make
// reading the pica source unnecessary. spec prints the language
// reference embedded in this binary (pica.Spec — doc.go's
// package comment, so it cannot drift from the parser); check
// parses a document and reports its errors without rendering.
package main

import (
	"flag"
	"fmt"
	"os"

	"repani.com/pica"
)

func specCmd(args []string) int {
	fs := flag.NewFlagSet("spec", flag.ExitOnError)
	out := fs.String("o", "", "output file (default stdout)")
	pos := parseMixed(fs, args)
	if len(pos) > 0 {
		fmt.Fprintln(os.Stderr, "pica spec: takes no input (the spec ships in the binary)")
		return 1
	}
	return writeOutput(*out, []byte(pica.Spec()))
}

func checkCmd(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	pos := parseMixed(fs, args)
	if len(pos) > 1 {
		fmt.Fprintln(os.Stderr, "pica check: at most one input file (default stdin)")
		return 1
	}
	src, err := readInput(pos)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica check: %v\n", err)
		return 1
	}
	if _, err := pica.Parse(string(src)); err != nil {
		fmt.Fprintf(os.Stderr, "pica check: %v\n", err)
		return 1
	}
	return 0
}
