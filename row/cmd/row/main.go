// Command row checks, formats, encodes and decodes ROW typed rows;
// row spec prints the reference (package repani.com/row).
//
//	row check FILE      validate a text, labelled or not; warn if not canonical
//	row fmt [-w] FILE   print the canonical text (-w: rewrite FILE)
//	row encode          a one-row text on stdin to hex
//	row decode          hex on stdin to the canonical text
//	row spec            the reference
//
// Exit codes: 0 ok, 1 invalid input, 2 usage.
package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"repani.com/durable"
	"repani.com/row"
)

const usage = `usage: row check FILE      validate a text, labelled or not; warn if not canonical
       row fmt [-w] FILE   print the canonical text (-w: rewrite FILE)
       row encode          a one-row text on stdin to hex
       row decode          hex on stdin to the canonical text
       row spec            the reference
       row help            this text
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "check":
		if len(args) != 2 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return check(args[1], stdout)
	case "fmt":
		write := len(args) == 3 && args[1] == "-w"
		if len(args) != 2 && !write {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return format(args[len(args)-1], write, stdout, stderr)
	case "encode":
		text, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintln(stderr, "row:", err)
			return 1
		}
		r, err := row.Parse(string(text))
		if err == nil {
			var b []byte
			if b, err = row.Encode(r); err == nil {
				fmt.Fprintln(stdout, hex.EncodeToString(b))
				return 0
			}
		}
		report(stderr, "stdin", err)
		return 1
	case "decode":
		in, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintln(stderr, "row:", err)
			return 1
		}
		b, err := hex.DecodeString(strings.TrimSpace(string(in)))
		if err != nil {
			fmt.Fprintln(stderr, "row: stdin is not hex:", err)
			return 1
		}
		r, err := row.Decode(b)
		if err == nil {
			var s string
			if s, err = row.Format(r); err == nil {
				fmt.Fprint(stdout, s)
				return 0
			}
		}
		report(stderr, "stdin", err)
		return 1
	case "spec":
		fmt.Fprint(stdout, row.Spec())
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "row: unknown command %q\n\n%s", args[0], usage)
	return 2
}

// canonical parses text, labelled when its first line is, and returns
// its canonical text.
func canonical(text string) (string, error) {
	if labelled(text) {
		rows, err := row.ParseLabeled(text)
		if err != nil {
			return "", err
		}
		return row.FormatLabeled(rows)
	}
	r, err := row.Parse(text)
	if err != nil {
		return "", err
	}
	return row.Format(r)
}

func labelled(text string) bool {
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimLeft(l, " \t"); l != "" {
			return l[0] == '"'
		}
	}
	return false
}

func check(name string, w io.Writer) int {
	b, err := os.ReadFile(name)
	if err != nil {
		fmt.Fprintln(w, "row:", err)
		return 1
	}
	c, err := canonical(string(b))
	if err != nil {
		report(w, name, err)
		return 1
	}
	if c != string(b) {
		fmt.Fprintf(w, "%s: valid, not canonical (row fmt -w %s)\n", name, name)
	}
	return 0
}

func format(name string, write bool, stdout, stderr io.Writer) int {
	b, err := os.ReadFile(name)
	if err != nil {
		fmt.Fprintln(stderr, "row:", err)
		return 1
	}
	c, err := canonical(string(b))
	if err != nil {
		report(stderr, name, err)
		return 1
	}
	if !write {
		fmt.Fprint(stdout, c)
		return 0
	}
	if c == string(b) {
		return 0
	}
	info, err := os.Stat(name)
	if err == nil {
		err = durable.WriteFile(name, []byte(c), info.Mode().Perm())
	}
	if err != nil {
		fmt.Fprintln(stderr, "row:", err)
		return 1
	}
	return 0
}

// report writes an error as NAME:LINE: CODE message (NAME: for one
// with no line).
func report(w io.Writer, name string, err error) {
	var re *row.Error
	if errors.As(err, &re) {
		where := name
		if re.Line > 0 {
			where = fmt.Sprintf("%s:%d", name, re.Line)
		}
		fmt.Fprintf(w, "%s: %s\n", where, strings.TrimPrefix(re.Error(), "row: "))
		return
	}
	fmt.Fprintf(w, "%s: %v\n", name, err)
}
