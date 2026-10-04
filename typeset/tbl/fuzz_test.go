package tbl

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// checkErr checks that err, from parsing s at column 1, is an *Error
// whose column lies in s or one past its end.
func checkErr(t *testing.T, what, s string, err error) {
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("%s(%q): %v is not an *Error", what, s, err)
	}
	if n := utf8.RuneCountInString(s); e.Col < 1 || e.Col > n+1 {
		t.Fatalf("%s(%q): column %d outside 1..%d", what, s, e.Col, n+1)
	}
}

func FuzzApply(f *testing.F) {
	for _, s := range []string{"5L 13L 7L *L", "w/b 30 31L 8R", "c", "", "L L S Lg", "*N! 2C/r", "5S", "0L", "5L!r", "r//b"} {
		f.Add("6L 6L 6L 6L", s)
	}
	f.Fuzz(func(t *testing.T, full, spec string) {
		var fs Formats
		if _, ok, err := fs.Apply(full, 1); err != nil || !ok {
			return
		}
		fm, isFull, err := fs.Apply(spec, 1)
		if err != nil {
			checkErr(t, "Apply", spec, err)
			return
		}
		auto := 0
		for i, c := range fm.Cols {
			if !c.Auto && c.Width < 1 {
				t.Fatalf("Apply(%q): column %d width %d", spec, i, c.Width)
			}
			if c.Auto {
				auto++
			}
			if c.Align == 'S' && i == 0 {
				t.Fatalf("Apply(%q): S first", spec)
			}
		}
		if auto > 1 || len(fm.Cols) == 0 {
			t.Fatalf("Apply(%q) after %q = %+v, full %v", spec, full, fm, isFull)
		}
	})
}

func FuzzParseRow(f *testing.F) {
	for _, s := range []string{"", "---", "a | b", "^:c TIME | @888 [1]", "= total | 1.50", ".. note", "~5 | ~100 ms", ":r :g x", "@a%2 b", "| |", "\t^ λ |"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		r, err := ParseRow(line, 1)
		if err != nil {
			checkErr(t, "ParseRow", line, err)
			return
		}
		if r.Kind != Data {
			return
		}
		n := utf8.RuneCountInString(line)
		for _, c := range r.Cells {
			if c.Col < 1 || c.Col > n+1 {
				t.Fatalf("ParseRow(%q): cell column %d outside 1..%d", line, c.Col, n+1)
			}
			if c.Text != strings.Trim(c.Text, " \t") || strings.ContainsRune(c.Text, '|') {
				t.Fatalf("ParseRow(%q): cell text %q", line, c.Text)
			}
			if c.Text != "" && (c.Text[0] == ':' || c.Text[0] == '@') {
				t.Fatalf("ParseRow(%q): content %q starts with a mark", line, c.Text)
			}
		}
	})
}

func FuzzLayout(f *testing.F) {
	f.Add("5L 13L 7L *L", "22:50 | Athens | A3 903 | :r Gate closed", 40)
	f.Add("6L 6L! *R", "Isolated thunderstorms | clipped text | ok", 20)
	f.Add("6L 8N", "Tue | (3.25)", 15)
	f.Add("w/b 20 *L 5R", "narrow", 40)
	f.Add("8N 4L", "12.25", 13)
	f.Fuzz(func(t *testing.T, spec, line string, measure int) {
		if measure < 1 || measure > 200 {
			return
		}
		var fs Formats
		fm, _, err := fs.Apply(spec, 1)
		if err != nil {
			return
		}
		if _, err := fit(fm, measure); err != nil {
			return
		}
		r, err := ParseRow(line, 1)
		if err != nil {
			return
		}
		var tb Table
		if tb.Add(fm, r) != nil {
			return
		}
		_, rows, err := tb.Layout(measure)
		if errors.Is(err, ErrNumber) {
			return
		}
		if err != nil {
			t.Fatalf("Layout after a fitting format: %v", err)
		}
		for _, lr := range rows {
			if lr.Kind == Data && len(lr.Lines) == 0 {
				t.Fatalf("%q: a data row with no lines", line)
			}
			for _, ln := range lr.Lines {
				if len(ln) != len(r.Cells) {
					t.Fatalf("%q: %d boxes for %d cells", line, len(ln), len(r.Cells))
				}
				end := 0
				for _, p := range ln {
					if p.Start < end || p.End <= p.Start || p.End > measure {
						t.Fatalf("%q on %q at %d: box %d..%d after %d", line, spec, measure, p.Start, p.End, end)
					}
					if n := utf8.RuneCountInString(p.Text); n != p.End-p.Start {
						t.Fatalf("%q on %q at %d: %q is %d code points in a box of %d", line, spec, measure, p.Text, n, p.End-p.Start)
					}
					end = p.End
				}
			}
		}
	})
}
