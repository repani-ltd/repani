// The pdf and report subcommands: thin doors to the press. The
// default presentation and the report live in repani.com/pica/press;
// here is only flag surface.
package main

import (
	"fmt"

	"repani.com/pica"
	"repani.com/pica/press"
)

func pdfCmd(args []string) int    { return pressCmd("pdf", press.PDF, args) }
func reportCmd(args []string) int { return pressCmd("report", press.Report, args) }

// pressCmd is a subcommand that prints a document through one of the
// press's presentations.
func pressCmd(name string, print func(*pica.Doc, bool) ([]byte, error), args []string) int {
	fs := newFlags(name)
	out := fs.String("o", "", "output file (default stdout)")
	mark := fs.Bool("mark", false, "paint the Repani mark top-right of page one")
	doc, rc := loadDoc(name, fs, args)
	if doc == nil {
		return rc
	}
	b, err := print(doc, *mark)
	if err != nil {
		fmt.Fprintf(stderr, "pica %s: %v\n", name, err)
		return 1
	}
	return writeOutput(name, *out, b)
}
