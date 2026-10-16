package row

import (
	"reflect"
	"regexp"
	"testing"
)

// Temporary: the hand matchers against the regexps they replace.

var (
	oDatetime = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})\.(\d{3})Z$`)
	oDate     = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	oTime     = regexp.MustCompile(`^(\d{2}):(\d{2}):(\d{2})\.(\d{3})$`)
	oDuration = regexp.MustCompile(`^(-?)(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)(?:\.(\d{1,3}))?s)?$`)
	oFloat    = regexp.MustCompile(`^-?\d+(?:\.\d+)?[eE][+-]?\d+$`)
	oDecimal  = regexp.MustCompile(`^(-?)(\d+)\.(\d*)$`)
	oInt      = regexp.MustCompile(`^(-?)(\d+)$`)
	oBytes    = regexp.MustCompile(`^0[xX]((?:[0-9a-fA-F]{2})*)$`)
)

func FuzzRegexDiff(f *testing.F) {
	for _, s := range []string{"", "-", "1h", "1h2m3.5s", "1.1234s", "5", "-0s", "1m1h", "1.s", "2026-10-16", "2026-10-16T01:02:03.004Z",
		"01:02:03.004", "1e5", "-1.5E+3", "1.e5", "1.", "-0.50", "0x", "0xAbC0", "0x123", "0X", "-12", "1.2.3", "１2", "1h.5s", "h", "s", "1s2"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		check := func(name string, re *regexp.Regexp, got []string) {
			want := re.FindStringSubmatch(s)
			if !reflect.DeepEqual(want, got) && !(want == nil && got == nil) {
				t.Fatalf("%s(%q): regexp %q, hand %q", name, s, want, got)
			}
		}
		check("datetime", oDatetime, matchShape(s, shapeDatetime))
		check("date", oDate, matchShape(s, shapeDate))
		check("time", oTime, matchShape(s, shapeTime))
		check("duration", oDuration, matchDuration(s))
		check("decimal", oDecimal, matchDecimal(s))
		check("int", oInt, matchInt(s))
		check("bytes", oBytes, matchBytes(s))
		if oFloat.MatchString(s) != isFloat(s) {
			t.Fatalf("float(%q): regexp %v", s, oFloat.MatchString(s))
		}
	})
}
