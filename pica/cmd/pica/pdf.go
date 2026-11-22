// The pdf subcommand: a thin door to the press, which sets the
// document in the layout its trailer names (repani.com/pica/press);
// here is only flag surface.
package main

import (
	"fmt"

	"repani.com/pica/press"
)

func pdfCmd(args []string) int {
	fs := newFlags("pdf")
	out := fs.String("o", "", "output file (default stdout)")
	mark := fs.Bool("mark", false, "paint the Repani mark top-right of page one")
	doc, rc := loadDoc("pdf", fs, args)
	if doc == nil {
		return rc
	}
	b, err := press.PDF(doc, *mark)
	if err != nil {
		fmt.Fprintf(stderr, "pica pdf: %v\n", err)
		return 1
	}
	return writeOutput("pdf", *out, b)
}
