// Command raster compiles raster source and renders it; usageText is
// the reference for its commands.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"repani.com/typeset/raster"
)

func usageText() string {
	return `raster -- rows of colored cells

Usage:
  raster spec                  print the format reference
  raster check FILE            compile, report errors, exit 0 if valid
  raster text FILE             compile and print the rows plain
  raster render FILE           as text, with ANSI colors
  raster html [-theme T] FILE  as one self-contained HTML page
  raster bytes FILE            compile and write the row records

-theme T colours the HTML page: teletext (default) or
teletext-light. FILE may be - for stdin. Exit status is 1 for an
input or compile error and 2 for a usage error.
`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText())
		return 2
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "spec":
		fmt.Fprint(stdout, raster.Spec()+"\n# The raster CLI\n\n"+usageText())
		return 0
	case "check", "text", "render", "html", "bytes":
	default:
		fmt.Fprintf(stderr, "raster: unknown command %q\n%s", cmd, usageText())
		return 2
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	theme := fs.String("theme", "teletext", "html theme")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprint(stderr, usageText())
		return 2
	}
	src, err := read(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "raster: %v\n", err)
		return 1
	}
	r, err := raster.Compile(src)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", fs.Arg(0), err)
		return 1
	}
	switch cmd {
	case "check":
	case "html":
		th, ok := raster.Themes[*theme]
		if !ok {
			fmt.Fprintf(stderr, "raster: unknown theme %q (teletext, teletext-light)\n", *theme)
			return 2
		}
		fmt.Fprint(stdout, raster.HTMLDocument([]*raster.Raster{r}, 1, strings.TrimSuffix(filepath.Base(fs.Arg(0)), ".rt"), th))
	case "bytes":
		if _, err := stdout.Write(r.Bytes()); err != nil {
			fmt.Fprintf(stderr, "raster: %v\n", err)
			return 1
		}
	case "render":
		fmt.Fprint(stdout, strings.Join(r.ANSI(), "\n")+"\n")
	default:
		fmt.Fprint(stdout, strings.Join(r.Text(), "\n")+"\n")
	}
	return 0
}

func read(name string) (string, error) {
	if name == "-" {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	b, err := os.ReadFile(name)
	return string(b), err
}
