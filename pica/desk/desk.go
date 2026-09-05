// Package desk writes pica copy: a Go template plus bound data
// becomes a validated pica source document. The desk turns wire
// data into copy; the press (repani.com/pica/press) prints it; pica
// is the language between them. The values are written the house
// way (repani.com/typeset/format); what this package adds is the
// template: the vocabulary a template composes with, the helpers
// that lay rows into columns and emit a .table block, and Render,
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
	"repani.com/typeset/tab"
)

// Funcs returns the template function set: the house formatting
// (round, decimal, trunc, pad, shortTime, shortDate, dur), join,
// cells, and table. The numeric helpers accept any numeric value,
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
		"cells":     cells,
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

// Render executes the template src over data with Funcs and returns
// the generated pica source, newline terminated, parsed before it
// is returned: an invalid document is an error labelled "name:
// rendered document:", since the parse position indexes the output,
// not the template. Missing keys render as Go's zero ("<no value>"
// over map data); a template that must not ship a missing fact
// tests with "if".
func Render(name, src string, data any) ([]byte, error) {
	tmpl, err := template.New(name).
		Option("missingkey=zero").
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

// cells lays rows into tab stops (repani.com/typeset/tab) and returns
// each row as its padded, aligned cells -- runes, not a table: the
// template joins them with a space for a grid, or places them one by
// one to put its own marks between the columns.
//
//	{{range cells "6L 8L 8L *L" 34 .Ferries "dep" "to" "vessel" "status"}}{{join . " "}}
//	{{end}}
//
// spec is tab's column spec; width is the measure the columns fit,
// or 0 to take exactly the fixed widths (an auto column then needs a
// width). Every row is measured before any is formatted, so an N
// column's decimal points align across the whole set; a cell wider
// than its column is clipped. rows and fields are as for Rows.
func cells(spec string, width int, rows any, fields ...string) ([][]string, error) {
	cols, err := tab.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("cells: %w", err)
	}
	data, err := Rows(rows, fields...)
	if err != nil {
		return nil, fmt.Errorf("cells: %w", err)
	}
	if width <= 0 {
		for _, c := range cols {
			width += c.Width
		}
		width += len(cols) - 1
	}
	fitted, err := tab.Fit(cols, width, 1)
	if err != nil {
		return nil, fmt.Errorf("cells: %w", err)
	}
	g := tab.New(fitted, 1)
	for _, r := range data {
		g.Measure(r)
	}
	out := make([][]string, len(data))
	for i, r := range data {
		out[i] = make([]string, len(fitted))
		for j := range fitted {
			var s string
			if j < len(r) {
				s = r[j]
			}
			out[i][j] = g.Cell(j, s)
		}
	}
	return out, nil
}

// table renders a data-driven .table block, sparing templates the
// range boilerplate when every cell is a plain field:
//
//	{{table "9L 9L 5L" "Spot | When | Level" .Tides "Spot" "When" "Kind"}}
//
// spec passes through verbatim (including a fixed width or the
// headerless "-" marker); header is the header row, or "" to emit
// none (pair with a "-" spec). rows must be a slice of objects; each
// cell is the named field formatted with %v (Rows). A missing field
// or a non-object row is an error -- never a silently blank cell.
func table(spec, header string, rows any, fields ...string) (string, error) {
	data, err := Rows(rows, fields...)
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
