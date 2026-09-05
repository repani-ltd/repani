// Package format writes values as text the way the house writes
// them: a number rounded or to a fixed number of places, a string
// cut or padded to a measure in runes, a time as HH:MM, a date as
// "Mon 02", a duration in its largest whole unit. It serves every
// writer that puts values on a page -- a pica desk, a raster
// vocabulary, a report -- and knows none of their languages: a
// function here returns runes, never syntax. The time and date
// functions take strings, because that is how a value arrives from
// JSON and FACT, and return their input unchanged when it does not
// parse, so a page never shows a blank where a value was.
package format

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Round formats v as a rounded integer: 25.7 → "26", -1.5 → "-2".
func Round(v float64) string { return strconv.FormatInt(int64(math.Round(v)), 10) }

// Decimal formats v with n places: 25.726 → "25.7" for n 1.
func Decimal(v float64, n int) string { return strconv.FormatFloat(v, 'f', n, 64) }

// Trunc cuts s to n runes.
func Trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Pad pads s with spaces on the right to n runes, cutting it if it
// is longer.
func Pad(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s + strings.Repeat(" ", n-len(r))
}

// dateTimeLayouts are the ISO 8601 datetime forms the time functions
// accept (the Z07:00 layout also parses a literal "Z").
var dateTimeLayouts = []string{
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04Z",
}

func parseAny(s string, extra ...string) (time.Time, bool) {
	for _, f := range slices.Concat(dateTimeLayouts, extra) {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ShortTime is HH:MM of a time: an ISO 8601 datetime, a time alone,
// or a space-separated datetime. Unparsed input comes back as is.
func ShortTime(s string) string {
	if t, ok := parseAny(s, "2006-01-02 15:04:05", "15:04:05", "15:04"); ok {
		return t.Format("15:04")
	}
	return s
}

// ShortDate is "Mon 02" of an ISO 8601 date or datetime. Unparsed
// input comes back as is.
func ShortDate(s string) string {
	if t, ok := parseAny(s, "2006-01-02"); ok {
		return t.Format("Mon 02")
	}
	return s
}

// Duration is a Go duration string in its largest whole unit: "45s",
// "3m", "2h", "5d". Go parses no days, so "1d" comes back as is,
// which is the same form.
func Duration(s string) string {
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
