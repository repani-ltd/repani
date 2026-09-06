// Command fact validates, canonicalizes, and JSON-converts FACT files.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"repani.com/fact"
)

const usage = `usage: fact <command> [flags] [file]

Reads the file argument, or stdin if omitted.

commands:
  spec           print the FACT reference embedded in this binary
  validate       check a .fact file; report errors, one per line
  fmt [-w]       print canonical form (-w: rewrite the file in place)
  encode         convert .fact to the canonical JSON encoding
  decode         convert the JSON encoding to canonical .fact
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes one command and returns the process exit code: 0 ok,
// 1 failure (invalid input, stale projection, I/O), 2 usage.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	fail := func(err error) int {
		fmt.Fprintln(stderr, "fact:", err)
		return 1
	}

	// Input-free commands dispatch before the input read: reading
	// stdin first left "fact spec" and "fact -h" hanging on a
	// terminal (found 2026-08-20).
	switch cmd {
	case "spec":
		if len(rest) > 0 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		// The reference teaches the format; the same command
		// teaches the tool: usage is appended from the same
		// string the CLI prints, so neither can drift.
		fmt.Fprint(stdout, fact.Spec()+"\n# The fact CLI\n\n"+usage)
		return 0
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "validate", "fmt", "encode", "decode":
		// input-reading commands, handled below
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}

	write := false
	if cmd == "fmt" {
		fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
		fs.SetOutput(stderr)
		fs.BoolVar(&write, "w", false, "rewrite the file in place")
		if err := fs.Parse(rest); err != nil {
			return flagExit(err)
		}
		rest = fs.Args()
	}

	var path string
	var data []byte
	var err error
	switch len(rest) {
	case 0:
		data, err = io.ReadAll(stdin)
	case 1:
		path = rest[0]
		data, err = os.ReadFile(path)
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
	if err != nil {
		return fail(err)
	}

	// report prints errs one per line; true means the input is invalid.
	report := func(errs []fact.Error) bool {
		for _, e := range errs {
			fmt.Fprintln(stderr, e.Error())
		}
		return len(errs) > 0
	}

	switch cmd {
	case "validate":
		facts, errs := fact.Load(data)
		if report(errs) {
			return 1
		}
		fmt.Fprintf(stdout, "ok: %d facts\n", len(facts))
	case "fmt":
		facts, errs := fact.Load(data)
		if report(errs) {
			return 1
		}
		out := fact.Canonical(facts)
		if write && path != "" {
			if err := os.WriteFile(path, out, 0o644); err != nil {
				return fail(err)
			}
		} else {
			stdout.Write(out)
		}
	case "encode":
		facts, errs := fact.Load(data)
		if report(errs) {
			return 1
		}
		out, err := fact.EncodeJSON(facts)
		if err != nil {
			return fail(err)
		}
		stdout.Write(out)
	case "decode":
		facts, errs := fact.DecodeJSON(data)
		if report(append(errs, fact.Validate(facts)...)) {
			return 1
		}
		stdout.Write(fact.Canonical(facts))
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
	return 0
}

// flagExit maps a flag-parsing error to an exit status: -h/-help is
// a request that was served (0), anything else a usage error (2).
func flagExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}
