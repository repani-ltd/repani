// The vocabulary: value formatting only -- layout belongs to the
// writers, never to templates. Its admission rule is in the package
// comment (stylebook.go).

package stylebook

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"

	"repani.com/typeset/tab"
)

// Funcs returns the house function set, the helpers every template
// composes with (also available standalone for callers driving
// text/template themselves). Render adds a language's own on top.
func Funcs() template.FuncMap {
	return template.FuncMap{
		// Numeric.
		"round":   round,
		"decimal": decimal,

		// String.
		"trunc": trunc,
		"pad":   pad,
		"join":  strings.Join,

		// Time / date / duration.
		"shortTime": shortTime,
		"shortDate": shortDate,
		"dur":       dur,

		// Columns.
		"cells": cells,
	}
}

// toFloat widens any numeric template value to float64. JSON data
// arrives as float64, but FACT binding yields int for whole
// numbers -- the numeric helpers accept both.
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
	case uint64:
		return float64(n), nil
	default:
		return 0, fmt.Errorf("expected a number, got %T", v)
	}
}

// round formats a number as a rounded integer string.
//
//	round(25.7)  -> "26"
//	round(-1.5)  -> "-2"
func round(v any) (string, error) {
	f, err := toFloat(v)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(int64(math.Round(f)), 10), nil
}

// decimal formats a number with n decimal places.
//
//	decimal(25.726, 1)  -> "25.7"
//	decimal(25.0, 2)    -> "25.00"
func decimal(v any, n int) (string, error) {
	f, err := toFloat(v)
	if err != nil {
		return "", err
	}
	return strconv.FormatFloat(f, 'f', n, 64), nil
}

// trunc hard-cuts s to n runes.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// pad pads s with spaces on the right to width n runes. If s is
// longer than n runes, it is truncated.
func pad(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s + strings.Repeat(" ", n-len(r))
}

// dateTimeLayouts are the ISO 8601 datetime forms the time helpers
// accept (the Z07:00 layout also parses a literal "Z"); shortTime
// and shortDate each add their own non-datetime forms.
var dateTimeLayouts = []string{
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04Z",
}

// parseAny parses s against the datetime layouts, then extra.
func parseAny(s string, extra ...string) (time.Time, bool) {
	for _, f := range slices.Concat(dateTimeLayouts, extra) {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// shortTime extracts HH:MM from a time string. Accepts ISO 8601
// datetimes, time-only strings, and space-separated datetimes.
// Returns the original string if no time can be parsed (template
// helpers must always return a usable string).
func shortTime(s string) string {
	if t, ok := parseAny(s, "2006-01-02 15:04:05", "15:04:05", "15:04"); ok {
		return t.Format("15:04")
	}
	return s
}

// shortDate accepts an ISO 8601 date or datetime string and returns
// the short form "Mon DD" (3-letter weekday + day). Returns the
// input unchanged if it cannot be parsed.
func shortDate(s string) string {
	if t, ok := parseAny(s, "2006-01-02"); ok {
		return t.Format("Mon 02")
	}
	return s
}

// dur parses a Go duration string and returns the compact
// single-unit form (s, m, h, or d). Note: Go's time.ParseDuration
// accepts only ns/us/ms/s/m/h; inputs like "1d" round-trip
// unchanged because parsing fails and the input is returned
// verbatim -- which happens to match the ">=24h" output format.
func dur(s string) string {
	d, err := time.ParseDuration(s)
	if err != nil {
		return s
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Rows extracts the named fields from a slice of objects as one
// string per cell, formatted with %v: the row shape every
// data-driven helper shares (JSON objects and FACT instances both
// bind to maps). A missing field or a non-object row is an error,
// never a silently blank cell.
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

// cells lays rows into tab stops (repani.com/typeset/tab) and returns each
// row as its padded, aligned cells -- runes, not a table: the
// template joins them with a space for a grid, or places them one by
// one to put its own marks between the columns.
//
//	{{range cells "6L 8L 8L *L" 34 .Ferries "dep" "to" "vessel" "status"}}{{join . " "}}
//	{{end}}
//
// spec is tab's column spec (widths, "*" for the auto column, L R C
// N); width is the measure the columns fit, or 0 to take exactly
// the fixed widths (an auto column then needs a width). Every row
// is measured before any is formatted, so an N column's decimal
// points align across the whole set; a cell wider than its column
// is clipped. rows and fields are as for Rows.
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
