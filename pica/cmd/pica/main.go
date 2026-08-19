// Command pica renders typeset source documents (see the typeset
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
// slice plus field names); the template's OUTPUT is a typeset
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
	case "spec":
		os.Exit(specCmd(os.Args[2:]))
	case "check":
		os.Exit(checkCmd(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "pica: unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `pica -- troff-inspired typesetting

Usage:
  pica render [-txtar|-fact] [-o FILE] <template> <data|->
  pica text [-o FILE] [file|-]
  pica pdf [-o FILE] [file|-]
  pica report [-o FILE] [file|-]
  pica spec [-o FILE]
  pica check [file|-]

render executes a Go template over JSON, FACT (-fact, implied by a
.fact filename), or txtar (-txtar) data and emits a typeset source
document; text, pdf, and report render a source document (default
stdin) to a fixed-width text page, an N-column newspaper PDF, or a
single-column report PDF (hairline table rules, page footer). spec
prints the language reference embedded in this binary; check parses
a document and reports errors without rendering. Layout
(width, paper, columns, font) comes from the document's trailer
(.width/.paper/.cols/.font), not from flags. See the typeset
package documentation for the source language.
`)
}

// readInput reads the single optional positional input (default
// stdin).
func readInput(pos []string) ([]byte, error) {
	if len(pos) == 0 || pos[0] == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(pos[0])
}

// writeOutput writes data to path, or stdout when path is empty.
func writeOutput(path string, data []byte) int {
	var err error
	if path == "" {
		_, err = os.Stdout.Write(data)
	} else {
		err = os.WriteFile(path, data, 0o644)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica: write: %v\n", err)
		return 1
	}
	return 0
}

// parseMixed parses args against fs, allowing flags before, between,
// and after positional arguments (stdlib flag stops at the first
// non-flag token, so it is re-invoked past each positional). Returns
// the positionals in order.
func parseMixed(fs *flag.FlagSet, args []string) []string {
	var pos []string
	fs.Parse(args)
	rem := fs.Args()
	for len(rem) > 0 {
		pos = append(pos, rem[0])
		fs.Parse(rem[1:])
		rem = fs.Args()
	}
	return pos
}

// ── text ────────────────────────────────────────────────────────────

func textCmd(args []string) int {
	fs := flag.NewFlagSet("text", flag.ExitOnError)
	out := fs.String("o", "", "output file (default stdout)")
	pos := parseMixed(fs, args)
	if len(pos) > 1 {
		fmt.Fprintln(os.Stderr, "pica text: at most one input file (default stdin)")
		return 1
	}
	src, err := readInput(pos)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica text: %v\n", err)
		return 1
	}
	doc, err := typeset.Parse(string(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica text: %v\n", err)
		return 1
	}
	page, err := doc.Text()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica text: %v\n", err)
		return 1
	}
	return writeOutput(*out, []byte(page))
}

// ── render ──────────────────────────────────────────────────────────

func renderCmd(args []string) int {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	useTxtar := fs.Bool("txtar", false, "parse data as a txtar archive (data.fact + *.txt content members)")
	useFact := fs.Bool("fact", false, "parse data as FACT (implied by a .fact data filename)")
	out := fs.String("o", "", "output file (default stdout)")
	pos := parseMixed(fs, args)
	if len(pos) != 2 {
		fmt.Fprintln(os.Stderr, "pica render: need <template> <data>")
		return 1
	}
	tmplPath := pos[0]

	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica: read template: %v\n", err)
		return 1
	}
	dataBytes, err := readInput(pos[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica: read data: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "pica: parse data: %v\n", err)
		return 1
	}

	tmpl, err := template.New(tmplPath).
		Option("missingkey=zero").
		Funcs(funcMap()).
		Parse(string(tmplBytes))
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica: parse template: %v\n", err)
		return 1
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		fmt.Fprintf(os.Stderr, "pica: execute template: %v\n", err)
		return 1
	}
	doc := buf.String()
	if !strings.HasSuffix(doc, "\n") {
		doc += "\n"
	}
	return writeOutput(*out, []byte(doc))
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
func parseTxtar(data []byte) (any, error) {
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
