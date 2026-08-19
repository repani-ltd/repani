// Command fact validates, canonicalizes, and JSON-converts FACT files.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"repani.com/fact"
	"repani.com/fact/project"
)

const usage = `usage: fact <command> [flags] [file]

Reads the file argument, or stdin if omitted.

commands:
  validate       check a .fact file; report errors, one per line
  fmt [-w]       print canonical form (-w: rewrite the file in place)
  encode         convert .fact to the canonical JSON encoding
  decode         convert the JSON encoding to canonical .fact
  project [-w|-check] [-o path] [dir|import-path]
                 project a Go package's declaration layer to canonical .fact
                 (stdout by default; -w writes <dir>/pkg.fact read-only;
                 -check verifies the target is fresh — the CI gate;
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
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]

	if cmd == "project" {
		fs := flag.NewFlagSet("project", flag.ExitOnError)
		write := fs.Bool("w", false, "write <dir>/pkg.fact instead of stdout")
		check := fs.Bool("check", false, "verify <dir>/pkg.fact is fresh; exit 1 if stale")
		outPath := fs.String("o", "", "write to this path (for read-only source trees, e.g. facts/ mirrors of dependencies)")
		fs.Parse(rest)
		rest = fs.Args()
		dir := "."
		switch len(rest) {
		case 0:
		case 1:
			dir = rest[0]
		default:
			fmt.Fprint(os.Stderr, usage)
			return 2
		}
		out, err := project.File(dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fact:", err)
			return 1
		}
		target := filepath.Join(dir, "pkg.fact")
		if *outPath != "" {
			target = *outPath
		}
		switch {
		case *outPath != "" && !*check:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				fmt.Fprintln(os.Stderr, "fact:", err)
				return 1
			}
			os.Remove(target) // stored read-only per SPEC §11.1
			if err := os.WriteFile(target, out, 0o444); err != nil {
				fmt.Fprintln(os.Stderr, "fact:", err)
				return 1
			}
		case *check:
			existing, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(existing, out) {
				fmt.Fprintf(os.Stderr, "fact: %s is stale (regenerate with: fact project -w %s)\n", target, dir)
				return 1
			}
			fmt.Printf("ok: %s is fresh\n", target)
		case *write:
			os.Remove(target) // stored read-only per SPEC §11.1
			if err := os.WriteFile(target, out, 0o444); err != nil {
				fmt.Fprintln(os.Stderr, "fact:", err)
				return 1
			}
		default:
			os.Stdout.Write(out)
		}
		return 0
	}

	if cmd == "hook" {
		payload, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fact:", err)
			return 0
		}
		ctx, err := project.Hook(payload)
		if err != nil {
			// Never block the edit; the -check gate catches any resulting
			// staleness. Compile errors are not errors here — Hook returns
			// them as context. Any context (e.g. a goimports rewrite) is
			// still worth surfacing alongside the failure.
			fmt.Fprintln(os.Stderr, "fact: hook:", err)
		}
		if ctx != "" {
			out, _ := json.Marshal(map[string]any{
				"hookSpecificOutput": map[string]any{
					"hookEventName":     "PostToolUse",
					"additionalContext": ctx,
				},
			})
			os.Stdout.Write(out)
		}
		return 0
	}

	write := false
	if cmd == "fmt" {
		fs := flag.NewFlagSet("fmt", flag.ExitOnError)
		fs.BoolVar(&write, "w", false, "rewrite the file in place")
		fs.Parse(rest)
		rest = fs.Args()
	}

	var path string
	var data []byte
	var err error
	switch len(rest) {
	case 0:
		data, err = io.ReadAll(os.Stdin)
	case 1:
		path = rest[0]
		data, err = os.ReadFile(path)
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fact:", err)
		return 1
	}

	switch cmd {
	case "validate":
		facts, errs := parseAll(data)
		if report(errs) {
			return 1
		}
		fmt.Printf("ok: %d facts\n", len(facts))
	case "fmt":
		facts, errs := parseAll(data)
		if report(errs) {
			return 1
		}
		out := fact.Canonical(facts)
		if write && path != "" {
			if err := os.WriteFile(path, out, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "fact:", err)
				return 1
			}
		} else {
			os.Stdout.Write(out)
		}
	case "encode":
		facts, errs := parseAll(data)
		if report(errs) {
			return 1
		}
		out, err := fact.EncodeJSON(facts)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fact:", err)
			return 1
		}
		os.Stdout.Write(out)
	case "decode":
		facts, errs := fact.DecodeJSON(data)
		errs = append(errs, fact.Validate(facts)...)
		if report(errs) {
			return 1
		}
		os.Stdout.Write(fact.Canonical(facts))
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	return 0
}

func parseAll(data []byte) ([]fact.Fact, []fact.Error) {
	facts, errs := fact.Parse(data)
	return facts, append(errs, fact.Validate(facts)...)
}

func report(errs []fact.Error) bool {
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Line < errs[j].Line })
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, e.Error())
	}
	return len(errs) > 0
}
