// The pdf and report subcommands: thin doors to the press. The
// default presentation and the report live in repani.com/pica/press;
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
	bytes, err := press.PDF(doc, *mark)
	if err != nil {
		fmt.Fprintf(stderr, "pica pdf: %v\n", err)
		return 1
	}
	return writeOutput("pdf", *out, bytes)
}

func reportCmd(args []string) int {
	fs := newFlags("report")
	out := fs.String("o", "", "output file (default stdout)")
	mark := fs.Bool("mark", false, "paint the Repani mark top-right of page one")
	doc, rc := loadDoc("report", fs, args)
	if doc == nil {
		return rc
	}
	bytes, err := press.Report(doc, *mark)
	if err != nil {
		fmt.Fprintf(stderr, "pica report: %v\n", err)
		return 1
	}
	return writeOutput("report", *out, bytes)
}
