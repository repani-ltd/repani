// Command row checks, encodes and decodes ROW v1 typed rows; row spec
// prints the reference (package repani.com/row).
//
//	row check FILE   validate text rows, one per line
//	row encode       text rows on stdin to hex, one per line
//	row decode       hex rows on stdin to text, one per line
//	row spec         the reference
//
// Exit codes: 0 ok, 1 invalid input, 2 usage.
package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"repani.com/row"
)

const usage = `usage: row check FILE   validate text rows, one per line (blank lines skipped)
       row encode       text rows on stdin to hex, one row per line
       row decode       hex rows on stdin to text, one row per line
       row spec         the reference
       row help         this text
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "check":
		if len(os.Args) != 3 {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		f, err := os.Open(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "row:", err)
			os.Exit(1)
		}
		defer f.Close()
		os.Exit(check(f, os.Args[2], os.Stdout))
	case "encode":
		os.Exit(lines(os.Stdin, os.Stdout, "stdin", encodeLine))
	case "decode":
		os.Exit(lines(os.Stdin, os.Stdout, "stdin", decodeLine))
	case "spec":
		fmt.Print(row.Spec())
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "row: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

// check reports every invalid row in r as NAME:LINE:COL: CODE message,
// and returns the exit code.
func check(r io.Reader, name string, w io.Writer) int {
	code := 0
	sc := scanner(r)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if _, err := row.Parse(line); err != nil {
			report(w, name, n, err)
			code = 1
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(w, "%s: %v\n", name, err)
		return 1
	}
	return code
}

func encodeLine(line string) (string, error) {
	items, err := row.Parse(line)
	if err != nil {
		return "", err
	}
	b, err := row.Encode(items)
	return hex.EncodeToString(b), err
}

func decodeLine(line string) (string, error) {
	b, err := hex.DecodeString(strings.TrimSpace(line))
	if err != nil {
		return "", fmt.Errorf("not hex: %v", err)
	}
	items, err := row.Decode(b)
	if err != nil {
		return "", err
	}
	return row.Format(items)
}

// lines maps each line of r through f, writing results to w and
// errors to stderr; it returns the exit code.
func lines(r io.Reader, w io.Writer, name string, f func(string) (string, error)) int {
	code := 0
	sc := scanner(r)
	out := bufio.NewWriter(w)
	defer out.Flush()
	for n := 1; sc.Scan(); n++ {
		res, err := f(sc.Text())
		if err != nil {
			report(os.Stderr, name, n, err)
			code = 1
			continue
		}
		fmt.Fprintln(out, res)
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		return 1
	}
	return code
}

func report(w io.Writer, name string, n int, err error) {
	var re *row.Error
	if errors.As(err, &re) {
		fmt.Fprintf(w, "%s:%d:%d: %s %s\n", name, n, re.Offset+1, re.Code, re.Msg)
		return
	}
	fmt.Fprintf(w, "%s:%d: %v\n", name, n, err)
}

// scanner reads lines up to 1 MiB: a row is at most an ASCON-KV
// value (64 KiB), twice that as hex.
func scanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	return sc
}
