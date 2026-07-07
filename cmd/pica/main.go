// Command pica renders Go text/template files with typesetting
// helpers into fixed-width monospace text.
//
// Usage:
//
//	pica render <template> <data.json>
//	pica render <template> -                      # data on stdin
//
//	pica render -txtar <template> <data.txtar>    # txtar input format
//	pica render -txtar <template> -               # txtar on stdin
//
// The template executes with the helper functions below, then .table
// blocks in the output are expanded into rendered tables (see the
// typeset package for the block syntax).
//
// # Template helpers
//
// Layout -- width is the first argument, so both call styles work:
// {{wrap 40 .body}} and {{.body | wrap 40}}.
//
//	wrap W S      reflow S to W columns, ragged-right, hyphenated
//	justify W S   reflow and justify S to W columns
//	truncl W S    hard-truncate every line of S to W runes
//
// Value formatting:
//
//	round F       float to rounded integer string: 25.7 -> "26"
//	decimal F N   float with N decimals: 25.726 1 -> "25.7"
//	trunc S N     truncate S to N runes
//	pad S N       right-pad (or truncate) S to exactly N runes
//	shortTime S   HH:MM from an ISO 8601 or time string
//	shortDate S   "Mon DD" from an ISO 8601 date or datetime
//	dur S         compact duration: "90m" -> "1h" style single unit
//
// # Input formats
//
// Default: a JSON document representing the template's data
// (object, array, or scalar). Same shape that the template
// references via {{.Field}} expressions.
//
// With -txtar: a txtar archive (golang.org/x/tools/txtar) containing
// up to three files with conventional names:
//
//	data.yaml     a YAML document; unmarshalled into the template
//	              data map. Each top-level key becomes a template
//	              field. Required (use an empty file -- "-- data.yaml --"
//	              with no body -- if you have nothing structured).
//	body.txt      plain prose text; injected as the "body" key in
//	              the template data map. Optional. Trailing whitespace
//	              is trimmed.
//	sources.txt   one URL per line; injected as the "sources" key,
//	              a []string. Empty lines are dropped, leading and
//	              trailing whitespace per line is trimmed. Optional.
//
// data.yaml MUST NOT contain top-level "body" or "sources" keys --
// those are reserved for body.txt and sources.txt and a collision
// is rejected at load time. The txtar archive may contain other
// files; they are ignored. The order of files in the archive is
// not significant.
//
// The template references all fields with the case the YAML keys
// were written in (typically lowercase, e.g. {{.title}}, {{.tags}}),
// plus {{.body}} and {{range .sources}} for the injected fields.
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
	fmt.Fprint(os.Stderr, `pica -- monospace text formatter

Usage:
  pica render [flags] <template> <data.json>
  pica render [flags] <template> -             # data on stdin
  pica render -txtar [flags] <template> <data.txtar>
  pica render -txtar [flags] <template> -      # txtar on stdin
  pica pdf [flags] [file|-]                    # text -> newspaper PDF

Render flags:
  -txtar         Parse the data file as a txtar archive instead of
                 JSON. The archive must contain a data.yaml file
                 (template fields), and may contain body.txt
                 (injected as .body) and sources.txt (one URL per
                 line, injected as .sources []string).

PDF flags (input is monospace text, e.g. pica render output):
  -o FILE        Output path (default stdout).
  -cols N        Columns per page (default 3).
  -paper SIZE    a4 (default), a5, or letter.
  -pt N          Font size; default fits the widest input line to
                 one column.
  -title TEXT    Masthead text (default: first input line).
  -nomast        No masthead; everything flows into columns.

The PDF layout keeps blocks intact where possible: splits leave at
least 2 lines on each side of a column break, single-line headings
stay with what follows, and split tables repeat their header rows.

Template helpers: wrap W S, justify W S, truncl W S (layout, width
first: {{wrap 40 .body}}), round, decimal, trunc, pad, shortTime,
shortDate, dur (formatting). .table blocks in the output are
expanded into fixed-width tables.
`)
}

func renderCmd(args []string) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	useTxtar := fs.Bool("txtar", false, "parse data as a txtar archive (data.yaml + body.txt + sources.txt) instead of JSON")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	rest := fs.Args()
	if len(rest) != 2 {
		fmt.Fprintln(os.Stderr, "pica render: need <template> <data>")
		return 1
	}
	tmplPath := rest[0]
	dataPath := rest[1]

	// Load template.
	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica: read template: %v\n", err)
		return 1
	}

	// Load data (file or stdin).
	var dataBytes []byte
	if dataPath == "-" {
		dataBytes, err = io.ReadAll(os.Stdin)
	} else {
		dataBytes, err = os.ReadFile(dataPath)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica: read data: %v\n", err)
		return 1
	}

	// Parse data into the template-data shape. Two paths:
	// JSON (default) or txtar archive (-txtar flag).
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

	// Stage 1: Go text/template execution.
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

	// Stage 2: expand .table blocks.
	out, err = typeset.ExpandTables(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica: expand tables: %v\n", err)
		return 1
	}

	fmt.Print(out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Println()
	}
	return 0
}

// parseTxtar parses a txtar archive into a template data map.
//
// Conventional file names inside the archive:
//
//	data.yaml     YAML document; each top-level key becomes a
//	              template field. Required.
//	body.txt      plain prose; injected as the "body" key
//	              (string). Optional. Trailing whitespace trimmed.
//	sources.txt   one URL per line; injected as the "sources" key
//	              ([]string). Optional. Empty lines dropped.
//
// data.yaml MUST NOT contain top-level "body" or "sources" keys --
// those are reserved for body.txt and sources.txt and a collision
// is rejected at load time.
//
// Other files in the archive are ignored.
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
