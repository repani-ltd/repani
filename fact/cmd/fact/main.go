// Command fact validates, canonicalizes, and JSON-converts FACT files.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"repani.com/fact"
	"repani.com/fact/project"
)

const usage = `usage: fact <command> [flags] [file]

Reads the file argument, or stdin if omitted.

commands:
  spec           print the FACT reference embedded in this binary
  validate       check a .fact file; report errors, one per line
  fmt [-w]       print canonical form (-w: rewrite the file in place)
  encode         convert .fact to the canonical JSON encoding
  decode         convert the JSON encoding to canonical .fact
  project [-w|-o path] [-check] [dir|import-path]
                 project a Go package's declaration layer to canonical .fact
                 (stdout by default; -w writes <dir>/pkg.fact read-only;
                 -o writes to path instead; -check verifies the target is
                 fresh instead of writing — the CI gate;
                 an import path projects a dependency resolved through this
                 module's go.mod — use with -o, e.g.
                 fact project -o facts/<import-path>/pkg.fact <import-path>)
  hook           Claude Code PostToolUse hook: reads the hook payload on
                 stdin; after an edit to a .go file in a package carrying a
                 pkg.fact, runs goimports on the edited file, regenerates
                 the projection, and reports the projection diff — or the
                 compile errors — to the agent as the impact report
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

	if cmd == "project" {
		fs := flag.NewFlagSet("project", flag.ContinueOnError)
		fs.SetOutput(stderr)
		write := fs.Bool("w", false, "write <dir>/pkg.fact instead of stdout")
		check := fs.Bool("check", false, "verify the target is fresh; exit 1 if stale")
		outPath := fs.String("o", "", "write to this path (for read-only source trees, e.g. facts/ mirrors of dependencies)")
		if err := fs.Parse(rest); err != nil {
			return flagExit(err)
		}
		rest = fs.Args()
		dir := "."
		switch {
		case len(rest) > 1, *write && *outPath != "", *write && *check:
			fmt.Fprint(stderr, usage)
			return 2
		case len(rest) == 1:
			dir = rest[0]
		}
		out, err := project.File(dir)
		if err != nil {
			return fail(err)
		}
		target, hint := filepath.Join(dir, "pkg.fact"), "fact project -w "+dir
		if *outPath != "" {
			target, hint = *outPath, "fact project -o "+*outPath+" "+dir
		}
		switch {
		case *check:
			existing, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(existing, out) {
				fmt.Fprintf(stderr, "fact: %s is stale (regenerate with: %s)\n", target, hint)
				return 1
			}
			fmt.Fprintf(stdout, "ok: %s is fresh\n", target)
		case *write, *outPath != "":
			if _, err := project.WriteReadOnly(target, out); err != nil {
				return fail(err)
			}
		default:
			stdout.Write(out)
		}
		return 0
	}

	if cmd == "hook" {
		payload, err := io.ReadAll(stdin)
		if err != nil {
			return fail(err)
		}
		ctx, err := project.Hook(payload)
		if err != nil {
			// Never block the edit; the -check gate catches any resulting
			// staleness. Compile errors are not errors here — Hook returns
			// them as context. Any context (e.g. a goimports rewrite) is
			// still worth surfacing alongside the failure.
			fmt.Fprintln(stderr, "fact: hook:", err)
		}
		if ctx != "" {
			out, _ := json.Marshal(map[string]any{
				"hookSpecificOutput": map[string]any{
					"hookEventName":     "PostToolUse",
					"additionalContext": ctx,
				},
			})
			stdout.Write(out)
		}
		return 0
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
