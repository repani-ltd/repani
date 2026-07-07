// Command pica renders typeset source documents (see the typeset
// package for the language) for monospace surfaces.
//
// Three orthogonal stages:
//
//	pica render <template> <data>   Go template + data -> source doc
//	pica text   [file|-]            source doc -> fixed-width text page
//	pica pdf    [file|-]            source doc -> N-column newspaper PDF
//
// A typical pipeline:
//
//	pica render page.tmpl data.json | pica text     > page.txt
//	pica render page.tmpl data.json | pica pdf -o page.pdf
//
// Documents are self-contained: width, paper, and columns come from
// the document's layout trailer (.width/.paper/.cols), never from
// flags, so the same source always produces the same output -- the
// PDF byte-identically so.
//
// # Templates
//
// render executes a Go text/template with value-formatting helpers
// (round, decimal, trunc, pad, shortTime, shortDate, dur); the
// template's OUTPUT is a typeset source document -- templates
// contain no layout calls. Data is JSON (default) or, with -txtar,
// a txtar archive:
//
//	data.yaml     a YAML document; each top-level key becomes a
//	              template field. Required (may be empty).
//	body.txt      plain prose; injected as the "body" key.
//	              Optional. Trailing whitespace trimmed.
//	sources.txt   one URL per line; injected as the "sources" key,
//	              a []string. Optional; blank lines dropped.
//
// data.yaml MUST NOT contain top-level "body" or "sources" keys --
// those are reserved for body.txt and sources.txt and a collision
// is rejected at load time. Other files in the archive are ignored.
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

	"golang.org/x/tools/txtar"
	"gopkg.in/yaml.v3"

	"github.com/pavlos/typeset"
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
	fmt.Fprint(os.Stderr, `pica -- monospace typesetting

Usage:
  pica render [-txtar] <template> <data|->   template -> source doc
  pica text [file|-]                         source -> text page
  pica pdf [-o FILE] [file|-]                source -> newspaper PDF

Layout (width, paper, columns) comes from the document's trailer
(.width/.paper/.cols), not from flags. See the typeset package
documentation for the source language.
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
	useTxtar := fs.Bool("txtar", false, "parse data as a txtar archive (data.yaml + body.txt + sources.txt) instead of JSON")
	fs.Parse(args)
	rest := fs.Args()
	if len(rest) != 2 {
		fmt.Fprintln(os.Stderr, "pica render: need <template> <data>")
		return 1
	}
	tmplPath := rest[0]

	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica: read template: %v\n", err)
		return 1
	}
	dataBytes, err := readInput(rest[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica: read data: %v\n", err)
		return 1
	}

	var data any
	if *useTxtar {
		data, err = parseTxtar(dataBytes)
	} else {
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
	out := buf.String()
	fmt.Print(out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Println()
	}
	return 0
}

// parseTxtar parses a txtar archive into a template data map. See
// the package comment for the file conventions.
func parseTxtar(data []byte) (any, error) {
	archive := txtar.Parse(data)
	if archive == nil {
		return nil, errors.New("txtar parse: empty archive")
	}

	var (
		dataYAMLPresent bool
		dataYAML        []byte
		bodyPresent     bool
		body            string
		sourcesPresent  bool
		sources         []string
	)
	for _, f := range archive.Files {
		switch f.Name {
		case "data.yaml":
			dataYAMLPresent = true
			dataYAML = f.Data
		case "body.txt":
			bodyPresent = true
			body = strings.TrimRight(string(f.Data), " \t\r\n")
		case "sources.txt":
			sourcesPresent = true
			for _, line := range strings.Split(string(f.Data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				sources = append(sources, line)
			}
		}
	}

	if !dataYAMLPresent {
		return nil, errors.New("txtar: missing required file data.yaml")
	}

	out := map[string]any{}
	if len(dataYAML) > 0 {
		if err := yaml.Unmarshal(dataYAML, &out); err != nil {
			return nil, fmt.Errorf("txtar: parse data.yaml: %w", err)
		}
	}

	// Reserved-key collision check. body.txt and sources.txt own
	// the "body" and "sources" keys; data.yaml may not also set
	// them.
	if bodyPresent {
		if _, exists := out["body"]; exists {
			return nil, errors.New("txtar: data.yaml has 'body' key but body.txt is also present")
		}
		out["body"] = body
	}
	if sourcesPresent {
		if _, exists := out["sources"]; exists {
			return nil, errors.New("txtar: data.yaml has 'sources' key but sources.txt is also present")
		}
		out["sources"] = sources
	}

	return out, nil
}
