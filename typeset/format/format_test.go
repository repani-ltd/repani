package format

import "testing"

// Known answers: the house forms, one per function.
func TestFormat(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{Round(25.7), "26"}, {Round(25.4), "25"}, {Round(-1.5), "-2"}, {Round(0), "0"}, {Round(100), "100"},
		{Decimal(25.726, 1), "25.7"}, {Decimal(25, 2), "25.00"}, {Decimal(-0.05, 1), "-0.1"},
		{Trunc("Αλεξανδρούπολη", 5), "Αλεξα"}, {Trunc("abc", 5), "abc"}, {Trunc("", 3), ""},
		{Pad("Kos", 6), "Kos   "}, {Pad("Ρόδος", 3), "Ρόδ"}, {Pad("", 2), "  "},
		{ShortTime("2026-04-10T14:30:25Z"), "14:30"}, {ShortTime("2026-04-10T14:30:25+03:00"), "14:30"},
		{ShortTime("2026-04-10 14:30:25"), "14:30"}, {ShortTime("14:30:25"), "14:30"}, {ShortTime("14:30"), "14:30"},
		{ShortTime("noon"), "noon"},
		{ShortDate("2026-04-10"), "Fri 10"}, {ShortDate("2026-04-10T09:00Z"), "Fri 10"}, {ShortDate("soon"), "soon"},
		{Duration("45s"), "45s"}, {Duration("90s"), "1m"}, {Duration("2h30m"), "2h"}, {Duration("48h"), "2d"},
		{Duration("1d"), "1d"}, {Duration("later"), "later"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
