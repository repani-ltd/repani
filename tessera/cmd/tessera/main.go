// Command tessera compiles page source to the 3,808-byte raster and
// renders it.
//
//	tessera spec                       print the format reference
//	tessera check FILE                 compile, report errors, exit 0 if valid
//	tessera text [-across N] FILE      compile and print the page plain
//	tessera render [-across N] FILE    as text, with ANSI colors
//	tessera page FILE                  compile and write the 3,808 bytes
//
// -across N lays the four panels N to a row (default 2: the
// two-by-two page). FILE may be - for stdin.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"repani.com/tessera"
)

func usageText() string {
	return `tessera -- one page of sixteen tiles

Usage:
  tessera spec                       print the format reference
  tessera check FILE                 compile, report errors, exit 0 if valid
  tessera text [-across N] FILE      compile and print the page plain
  tessera render [-across N] FILE    as text, with ANSI colors
  tessera page FILE                  compile and write the 3,808 bytes

-across N lays the four panels N to a row (default 2). FILE may be -
for stdin. Exit status is 1 for an input or compile error and 2 for
a usage error.
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
		fmt.Fprint(stdout, tessera.Spec()+"\n# The tessera CLI\n\n"+usageText())
		return 0
	case "check", "text", "render", "page":
	default:
		fmt.Fprintf(stderr, "tessera: unknown command %q\n%s", cmd, usageText())
		return 2
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	across := fs.Int("across", 2, "panels per row")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprint(stderr, usageText())
		return 2
	}
	src, err := read(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "tessera: %v\n", err)
		return 1
	}
	page, err := tessera.Compile(src)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", fs.Arg(0), err)
		return 1
	}
	switch cmd {
	case "check":
	case "page":
		if _, err := stdout.Write(page[:]); err != nil {
			fmt.Fprintf(stderr, "tessera: %v\n", err)
			return 1
		}
	default:
		panels := make([][]string, tessera.Panels)
		for i := range panels {
			if cmd == "render" {
				panels[i] = page.ANSI(i)
			} else {
				panels[i] = page.Text(i)
			}
		}
		fmt.Fprint(stdout, strings.Join(tessera.Layout(panels, *across), "\n")+"\n")
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
