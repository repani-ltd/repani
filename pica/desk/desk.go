// Package desk writes pica copy: a Go template plus bound data
// becomes a validated pica source document. The desk turns wire
// data into copy; the press (repani.com/pica/press) prints it; pica
// is the language between them. The values are written the house
// way (repani.com/typeset/format); what this package adds is the
// template: the vocabulary a template composes with, the helper
// that emits a .table block from rows of data, and Render,
// which parses the result before returning it, so a template bug is
// an error with a line number, never an invalid document on air.
//
// Data arrives already bound (fact.Bind, encoding/json, or plain
// Go values); loading and scheduling belong to the caller.
package desk

import (
	"fmt"
	"reflect"
	"strings"
	"text/template"

	"repani.com/pica"
	"repani.com/typeset/format"
)

// Funcs returns the template function set: the house formatting
// (round, decimal, trunc, pad, shortTime, shortDate, dur), join,
// and table. The numeric helpers accept any numeric value,
// since JSON binds numbers as float64 and FACT as int.
func Funcs() template.FuncMap {
	return template.FuncMap{
		"round": func(v any) (string, error) {
			f, err := toFloat(v)
			if err != nil {
				return "", err
			}
			return format.Round(f), nil
		},
		"decimal": func(v any, n int) (string, error) {
			f, err := toFloat(v)
			if err != nil {
				return "", err
			}
			return format.Decimal(f, n), nil
		},
		"trunc":     format.Trunc,
		"pad":       format.Pad,
		"join":      strings.Join,
		"shortTime": format.ShortTime,
		"shortDate": format.ShortDate,
		"dur":       format.Duration,
		"table":     table,
	}
}

// toFloat widens any numeric template value to float64.
func toFloat(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	default:
		return 0, fmt.Errorf("expected a number, got %T", v)
	}
}

// Missing is what a template does with a key its map data does not
// hold.
type Missing int

const (
	// Blank renders the key as Go's zero value -- "<no value>" over
	// map data; a template that must not ship a missing fact tests it
	// with "if". pica render's rule: a feed's optional fields.
	Blank Missing = iota
	// Refuse makes it an error: copy that states a fact must find it
	// in the data. pica html's rule for a page's NAME.t.tmpl, which
	// states every fact it names.
	Refuse
)

// Render executes the template src over data with Funcs, a missing
// key as missing says, and returns the generated pica source,
// newline terminated, parsed before it is returned: an invalid
// document is an error labelled "name: rendered document:", since
// the parse position indexes the output, not the template. It is the
// one expansion of pica copy; every command that turns a template
// into a document calls it.
func Render(name, src string, data any, missing Missing) ([]byte, error) {
	opt := "missingkey=zero"
	if missing == Refuse {
		opt = "missingkey=error"
	}
	tmpl, err := template.New(name).
		Option(opt).
		Funcs(Funcs()).
		Parse(src)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	doc := buf.String()
	if !strings.HasSuffix(doc, "\n") {
		doc += "\n"
	}
	if _, err := pica.Parse(doc); err != nil {
		return nil, fmt.Errorf("%s: rendered document: %w", name, err)
	}
	return []byte(doc), nil
}

// Rows extracts the named fields from a slice of objects as one
// string per cell, formatted with %v: the row shape the data-driven
// helpers share (JSON objects and FACT instances both bind to maps).
// A missing field or a non-object row is an error, never a silently
// blank cell.
func Rows(rows any, fields ...string) ([][]string, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("no fields given")
	}
	rv := reflect.ValueOf(rows)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return nil, fmt.Errorf("rows is %T, want a slice", rows)
	}
	out := make([][]string, rv.Len())
	for i := range rv.Len() {
		row, ok := rv.Index(i).Interface().(map[string]any)
		if !ok {
			return nil, fmt.Errorf("row %d is %T, want an object", i, rv.Index(i).Interface())
		}
		out[i] = make([]string, len(fields))
		for j, f := range fields {
			v, ok := row[f]
			if !ok {
				return nil, fmt.Errorf("row %d has no field %q", i, f)
			}
			out[i][j] = fmt.Sprintf("%v", v)
		}
	}
	return out, nil
}

// table renders a data-driven .table block, sparing templates the
// range boilerplate when every cell is a plain field:
//
//	{{table "9L 9L 5L" "Spot | When | Level" .Tides "Spot" "When" "Kind"}}
//
// spec passes through verbatim (including a narrowing width); header
// is the header row, which the helper marks "^", or "" to emit none.
// rows must be a slice of objects; each cell is the named field
// formatted with %v (Rows). A missing field or a non-object row is an
// error -- never a silently blank cell -- and so is a value the table
// language would read as syntax rather than text (plainCell): the
// language has no escape, and data must never change a row's role.
func table(spec, header string, rows any, fields ...string) (string, error) {
	data, err := Rows(rows, fields...)
	if err != nil {
		return "", fmt.Errorf("table: %w", err)
	}
	for i, cells := range data {
		for j, c := range cells {
			if err := plainCell(c, j == 0); err != nil {
				return "", fmt.Errorf("table: row %d field %q: %w", i, fields[j], err)
			}
		}
	}
	var b strings.Builder
	b.WriteString(".table ")
	b.WriteString(spec)
	b.WriteString("\n")
	if header != "" {
		b.WriteString("^")
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

// plainCell refuses a value that would not read back as its own text
// in a table row: a "|", which splits cells; a leading ":" or "@", a
// colour or link mark; and in a row's first cell a leading "^", "="
// or "..", a role, or the value "---", a rule.
func plainCell(v string, first bool) error {
	s := strings.TrimSpace(v)
	switch {
	case strings.Contains(s, "|"):
		return fmt.Errorf("%q holds |, which splits cells", v)
	case strings.HasPrefix(s, ":") || strings.HasPrefix(s, "@"):
		return fmt.Errorf("%q begins with a colour or link mark", v)
	case first && (strings.HasPrefix(s, "^") || strings.HasPrefix(s, "=") || strings.HasPrefix(s, "..") || s == "---"):
		return fmt.Errorf("%q would mark the row", v)
	}
	return nil
}
