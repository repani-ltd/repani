// Command pica renders pica source documents (see the pica
// package for the language) to text pages, newspaper PDFs, and
// report PDFs.
//
// One generation stage, then a writer:
//
//	pica render <template> <data>   Go template + data -> source doc
//	pica text   [file|-]            source doc -> fixed-width text page
//	pica pdf    [file|-]            source doc -> N-column newspaper PDF
//	pica report [file|-]            source doc -> single-column report PDF
//
// Two oracles, so learning the language never requires the source:
//
//	pica spec                       print the language reference
//	pica check  [file|-]            parse a source doc, report errors
//
// A typical pipeline:
//
//	pica render page.tmpl data.json | pica text     > page.txt
//	pica render page.tmpl data.json | pica pdf -o page.pdf
//	pica render page.tmpl data.json | pica report -o page.pdf
//
// Documents are self-contained: width, paper, columns, and body face
// come from the document's layout trailer (.width/.paper/.cols/
// .font), never from flags, so the same source always produces the
// same output -- the PDF byte-identically so.
//
// # Templates
//
// render executes a Go text/template with value-formatting helpers
// (round, decimal, trunc, pad, shortTime, shortDate, dur) and the
// data-driven "table" helper (emits a .table block from a rows
// slice plus field names); the template's OUTPUT is a pica
// source document -- templates contain no layout calls.
//
// Data is FACT (a *.fact file or -fact), JSON (default otherwise),
// or, with -txtar, a txtar archive pairing facts with prose:
//
//	data.fact     a FACT document (typed key: type = value lines);
//	              keys bind to template fields -- dotted keys nest,
//	              kind:id instances become rows, list(ref(kind))
//	              binds an ordered row slice for range. Required
//	              (may be empty).
//	*.txt         any other .txt member is plain content, injected
//	              verbatim as a string field named after the file
//	              ("body.txt" -> .body). Trailing whitespace
//	              trimmed. Facts are data; prose is content.
//
// A .txt member whose name collides with a data.fact key is rejected
// at load time (the FACT duplicate rule, extended to the archive).
// Non-.txt members other than data.fact are ignored.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"repani.com/fact"

	"repani.com/pica"
)

// The process seams, swappable by tests that drive the subcommands
// in-process.
var (
	stdin  io.Reader = os.Stdin
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "render":
		os.Exit(renderCmd(os.Args[2:]))
	case "text":
		os.Exit(textCmd(os.Args[2:]))
	case "pdf":
		os.Exit(pdfCmd(os.Args[2:]))
	case "report":
		os.Exit(reportCmd(os.Args[2:]))
	case "html":
		os.Exit(htmlCmd(os.Args[2:]))
	case "spec":
		os.Exit(specCmd(os.Args[2:]))
	case "check":
		os.Exit(checkCmd(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(stderr, "pica: unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(stderr, `pica -- troff-inspired typesetting

Usage:
  pica render [-txtar|-fact] [-o FILE] <template> <data|->
  pica text [-o FILE] [file|-]
  pica pdf [-mark] [-o FILE] [file|-]
  pica report [-mark] [-o FILE] [file|-]
  pica html [-o FILE] [file|-]
  pica html -txtar -page NAME [-o FILE] [archive|-]
  pica spec [-o FILE]
  pica check [file|-]

render executes a Go template over JSON, FACT (-fact, implied by a
.fact filename), or txtar (-txtar) data and emits a pica source
document; text, pdf, and report render a source document (default
stdin) to a fixed-width text page, an N-column newspaper PDF, or a
single-column report PDF (hairline table rules, page footer); html
renders one document to a semantic <article> fragment, or, with
-txtar, assembles a whole page from an archive: NAME.t is the
document (or NAME.t.tmpl, a text/template over data.fact that
emits it -- pica render inline), page.tmpl the html/template,
data.fact typed values,
*.html and *.svg raw trusted fragments (see the html subcommand's
source for the data the template sees). spec
prints the language reference embedded in this binary; check parses
a document and reports errors without rendering. Layout
(width, paper, columns, font) comes from the document's trailer
(.width/.paper/.cols/.font), not from flags. -mark paints the
Repani mark top-right of page one: a publishing choice made where
the PDF is rendered, never in the document. See the pica package
documentation for the source language.

Flags may appear before, between, or after the positionals; "--"
ends flag parsing. Exit status is 1 for an input, parse, render,
or write error and 2 for a usage error.
`)
}

// newFlags returns the flag set of subcommand cmd. Flag errors are
// reported (with the set's usage) on stderr and returned, never
// exited on, so every subcommand owns its exit code.
func newFlags(cmd string) *flag.FlagSet {
	fs := flag.NewFlagSet("pica "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parseMixed parses args against fs, allowing flags before, between,
// and after positional arguments (stdlib flag stops at the first
// non-flag token, so it is re-invoked past each positional). A "--"
// token ends flag parsing: everything after it is positional, so a
// file named "-x" can be passed. Returns the positionals in order;
// a flag error (or -h) has already been reported on fs.Output().
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for i := 0; i < len(args); {
		a := args[i]
		switch {
		case a == "--":
			return append(pos, args[i+1:]...), nil
		case a == "-" || !strings.HasPrefix(a, "-"):
			pos = append(pos, a)
			i++
		default:
			// One flag: "-name=value" is a single token; "-name"
			// takes the next token as its value unless it is a bool
			// flag. Unknown names reach Parse as a single token,
			// which reports them (and -h) the standard way.
			n := 1
			name := strings.TrimLeft(a, "-")
			if f := fs.Lookup(name); f != nil && !strings.Contains(a, "=") {
				if b, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !b.IsBoolFlag() {
					n = 2
				}
			}
			if err := fs.Parse(args[i:min(i+n, len(args))]); err != nil {
				return nil, err
			}
			i += n
		}
	}
	return pos, nil
}

// flagExit maps a flag-parsing error to an exit status: -h/-help
// is a request that was served (0), anything else a usage error (2).
func flagExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

// readInput reads the single optional positional input (default
// stdin).
func readInput(pos []string) ([]byte, error) {
	if len(pos) == 0 || pos[0] == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(pos[0])
}

// writeOutput writes data to path, or stdout when path is empty,
// reporting a failure as subcommand cmd's.
func writeOutput(cmd, path string, data []byte) int {
	var err error
	if path == "" {
		_, err = stdout.Write(data)
	} else {
		err = os.WriteFile(path, data, 0o644)
	}
	if err != nil {
		fmt.Fprintf(stderr, "pica %s: write: %v\n", cmd, err)
		return 1
	}
	return 0
}

// loadDoc is the shared prologue of the subcommands that consume a
// source document: parse args against fs plus the single optional
// input positional, read it, parse it. Callers define their own
// flags on fs first. A nil doc means the error has been reported
// under "pica <cmd>:" and rc is the exit code.
func loadDoc(cmd string, fs *flag.FlagSet, args []string) (doc *pica.Doc, rc int) {
	pos, err := parseMixed(fs, args)
	if err != nil {
		return nil, flagExit(err)
	}
	if len(pos) > 1 {
		fmt.Fprintf(stderr, "pica %s: at most one input file (default stdin)\n", cmd)
		return nil, 2
	}
	src, err := readInput(pos)
	if err != nil {
		fmt.Fprintf(stderr, "pica %s: %v\n", cmd, err)
		return nil, 1
	}
	doc, err = pica.Parse(string(src))
	if err != nil {
		fmt.Fprintf(stderr, "pica %s: %v\n", cmd, err)
		return nil, 1
	}
	return doc, 0
}

// ── text ────────────────────────────────────────────────────────────

func textCmd(args []string) int {
	fs := newFlags("text")
	out := fs.String("o", "", "output file (default stdout)")
	doc, rc := loadDoc("text", fs, args)
	if doc == nil {
		return rc
	}
	page, err := doc.Text()
	if err != nil {
		fmt.Fprintf(stderr, "pica text: %v\n", err)
		return 1
	}
	return writeOutput("text", *out, []byte(page))
}

// ── render ──────────────────────────────────────────────────────────

func renderCmd(args []string) int {
	fs := newFlags("render")
	useTxtar := fs.Bool("txtar", false, "parse data as a txtar archive (data.fact + *.txt content members)")
	useFact := fs.Bool("fact", false, "parse data as FACT (implied by a .fact data filename)")
	out := fs.String("o", "", "output file (default stdout)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) != 2 {
		fmt.Fprintln(stderr, "pica render: need <template> <data>")
		return 2
	}
	tmplPath := pos[0]

	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		fmt.Fprintf(stderr, "pica render: read template: %v\n", err)
		return 1
	}
	dataBytes, err := readInput(pos[1:])
	if err != nil {
		fmt.Fprintf(stderr, "pica render: read data: %v\n", err)
		return 1
	}

	var data any
	switch {
	case *useTxtar:
		data, err = parseTxtar(dataBytes)
	case *useFact || strings.HasSuffix(pos[1], ".fact"):
		data, err = bindFacts(dataBytes)
	default:
		err = json.Unmarshal(dataBytes, &data)
	}
	if err != nil {
		fmt.Fprintf(stderr, "pica render: parse data: %v\n", err)
		return 1
	}

	tmpl, err := template.New(tmplPath).
		Option("missingkey=zero").
		Funcs(funcMap()).
		Parse(string(tmplBytes))
	if err != nil {
		fmt.Fprintf(stderr, "pica render: parse template: %v\n", err)
		return 1
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		fmt.Fprintf(stderr, "pica render: execute template: %v\n", err)
		return 1
	}
	doc := buf.String()
	if !strings.HasSuffix(doc, "\n") {
		doc += "\n"
	}
	return writeOutput("render", *out, []byte(doc))
}

// bindFacts parses, validates, and binds a FACT document into
// template data. All three stages are loud: a malformed line, a bad
// value, a dangling ref, or a duplicate key fails the render.
func bindFacts(src []byte) (map[string]any, error) {
	facts, errs := fact.Parse(src)
	errs = append(errs, fact.Validate(facts)...)
	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return nil, errors.New("data.fact: " + strings.Join(msgs, "; "))
	}
	return fact.Bind(facts)
}

// parseTxtar parses a txtar archive into a template data map. See
// the package comment for the member conventions: data.fact carries
// the facts, every other .txt member is content injected as a string
// field named after the file.
func parseTxtar(data []byte) (map[string]any, error) {
	files := parseArchive(string(data))
	if len(files) == 0 {
		return nil, errors.New("txtar parse: empty archive")
	}

	var factSrc []byte
	factPresent := false
	content := map[string]string{}
	for _, f := range files {
		switch {
		case f.name == "data.fact":
			factPresent = true
			factSrc = []byte(f.data)
		case strings.HasSuffix(f.name, ".txt"):
			key := strings.TrimSuffix(f.name, ".txt")
			if _, dup := content[key]; dup {
				return nil, fmt.Errorf("txtar: duplicate member %q", f.name)
			}
			content[key] = strings.TrimRight(f.data, " \t\r\n")
		}
	}
	if !factPresent {
		return nil, errors.New("txtar: missing required file data.fact")
	}

	out, err := bindFacts(factSrc)
	if err != nil {
		return nil, fmt.Errorf("txtar: %w", err)
	}

	// Content members own their keys; data.fact may not also set
	// them (the FACT duplicate rule, extended to the archive).
	for key, text := range content {
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("txtar: data.fact has %q key but %s.txt is also present", key, key)
		}
		out[key] = text
	}
	return out, nil
}
