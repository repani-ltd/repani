// Template helper functions: value formatting only -- layout belongs
// to the writers, never to templates. Documented in main.go.
package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// funcMap returns the pica template function set.
func funcMap() template.FuncMap {
	return template.FuncMap{
		// Numeric.
		"round":   round,
		"decimal": decimal,

		// String.
		"trunc": trunc,
		"pad":   pad,

		// Time / date / duration.
		"shortTime": shortTime,
		"shortDate": shortDate,
		"dur":       dur,
	}
}

// toFloat widens any numeric template value to float64. JSON data
// arrives as float64, but YAML (txtar mode) yields int for whole
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

// trunc truncates a string to n runes.
func trunc(s string, n int) string {
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

// shortTime extracts HH:MM from a time string. Accepts ISO 8601
// datetimes, time-only strings, and space-separated datetimes.
// Returns the original string if no time can be parsed (template
// helpers must always return a usable string).
func shortTime(s string) string {
	formats := []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04Z",
		"2006-01-02 15:04:05",
		"15:04:05",
		"15:04",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.Format("15:04")
		}
	}
	return s
}

// shortDate accepts an ISO 8601 date or datetime string and returns
// the short form "Mon DD" (3-letter weekday + day). Returns the
// input unchanged if it cannot be parsed.
func shortDate(s string) string {
	formats := []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04Z",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.Format("Mon 02")
		}
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
