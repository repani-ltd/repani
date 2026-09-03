// Package desk writes pica copy: a Go template plus bound data
// becomes a validated pica source document. The desk turns wire
// data into copy; the press (repani.com/pica/press) prints it; pica
// is the language between them. The house style it writes in is
// the stylebook (repani.com/stylebook), shared with every Repani
// language that generates pages; what this package adds is pica's
// own: the helper that emits a .table block, and the check that a
// rendered document parses.
//
// Data arrives already bound (fact.Bind, encoding/json, or plain
// Go values); loading and scheduling belong to the caller.
package desk

import (
	"fmt"
	"strings"
	"text/template"

	"repani.com/pica"
	"repani.com/stylebook"
)

// Funcs returns the pica template function set: the stylebook's
// vocabulary plus "table".
func Funcs() template.FuncMap {
	funcs := stylebook.Funcs()
	funcs["table"] = table
	return funcs
}

// Render executes the template src over data with Funcs and returns
// the generated pica source, newline terminated, parsed before it
// is returned: an invalid document is an error, never a result
// (stylebook.Render with pica.Parse as the check; the parse
// position indexes the output, and the label says so).
func Render(name, src string, data any) ([]byte, error) {
	return stylebook.Render(name, src, data, template.FuncMap{"table": table}, func(doc string) error {
		_, err := pica.Parse(doc)
		return err
	})
}

// table renders a data-driven .table block, sparing templates the
// range boilerplate when every cell is a plain field:
//
//	{{table "9L 9L 5L" "Spot | When | Level" .Tides "Spot" "When" "Kind"}}
//
// spec passes through verbatim (including a fixed width or the
// headerless "-" marker); header is the header row, or "" to emit
// none (pair with a "-" spec). rows must be a slice of objects
// (JSON objects and FACT instances both bind to maps); each cell is
// the named field formatted with %v (stylebook.Rows). A missing
// field or a non-object row is an error -- never a silently blank
// cell.
func table(spec, header string, rows any, fields ...string) (string, error) {
	data, err := stylebook.Rows(rows, fields...)
	if err != nil {
		return "", fmt.Errorf("table: %w", err)
	}
	var b strings.Builder
	b.WriteString(".table ")
	b.WriteString(spec)
	b.WriteString("\n")
	if header != "" {
		b.WriteString(header)
		b.WriteString("\n")
	}
	for _, cells := range data {
		b.WriteString(strings.Join(cells, " | "))
		b.WriteString("\n")
	}
	b.WriteString(".end")
	return b.String(), nil
}
