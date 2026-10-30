// The fmt subcommand: pica.Format over source documents, gofmt's
// shape -- stdout by default, -w to rewrite, -l to list.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"repani.com/durable"
	"repani.com/pica"
)

func fmtCmd(args []string) int {
	fs := newFlags("fmt")
	write := fs.Bool("w", false, "rewrite each file whose formatting differs")
	list := fs.Bool("l", false, "list each file whose formatting differs")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) == 0 || (len(pos) == 1 && pos[0] == "-") {
		if *write || *list {
			fmt.Fprintln(stderr, "pica fmt: -w and -l want files, not stdin")
			return 2
		}
		src, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "pica fmt: %v\n", err)
			return 1
		}
		out, err := pica.Format(string(src))
		if err != nil {
			fmt.Fprintf(stderr, "pica fmt: %v\n", err)
			return 1
		}
		return writeOutput("fmt", "", []byte(out))
	}
	rc := 0
	for _, path := range pos {
		if err := fmtFile(path, *write, *list); err != nil {
			fmt.Fprintf(stderr, "pica fmt: %s: %v\n", path, err)
			rc = 1
		}
	}
	return rc
}

// fmtFile formats one file: printed when neither -w nor -l is set,
// else listed and/or rewritten only when its formatting differs.
func fmtFile(path string, write, list bool) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := pica.Format(string(src))
	if err != nil {
		return err
	}
	if !write && !list {
		_, err = io.WriteString(stdout, out)
		return err
	}
	if bytes.Equal(src, []byte(out)) {
		return nil
	}
	if list {
		fmt.Fprintln(stdout, path)
	}
	if write {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		return durable.WriteFile(path, []byte(out), info.Mode().Perm())
	}
	return nil
}
