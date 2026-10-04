package tbl

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func apply(t *testing.T, f *Formats, spec string) (Format, bool) {
	t.Helper()
	fm, full, err := f.Apply(spec, 1)
	if err != nil {
		t.Fatalf("Apply(%q): %v", spec, err)
	}
	return fm, full
}

func fg(c Color) Code    { return Code{FG: c, HasFG: true} }
func bg(c Color) Code    { return Code{BG: c, HasBG: true} }
func on(f, b Color) Code { return Code{FG: f, BG: b, HasFG: true, HasBG: true} }

func TestFullFormat(t *testing.T) {
	var f Formats
	fm, full := apply(t, &f, "  w/b   30 5L 13Lc *R/y! 7S  ")
	want := Format{Row: on(White, Blue), Narrow: 30, Cols: []Column{
		{Width: 5, Align: 'L'},
		{Width: 13, Align: 'L', Code: fg(Cyan)},
		{Auto: true, Align: 'R', Code: bg(Yellow), Clip: true},
		{Width: 7, Align: 'S'},
	}}
	if !full || !reflect.DeepEqual(fm, want) {
		t.Fatalf("Apply = %+v, %v\nwant %+v", fm, full, want)
	}
}

func TestRelativeFormat(t *testing.T) {
	var f Formats
	apply(t, &f, "c/b 30 5L! 13Lr *R/y")

	// Bare: the full format again.
	fm, full := apply(t, &f, "")
	if full || fm.Row != on(Cyan, Blue) || fm.Narrow != 30 || !fm.Cols[0].Clip || fm.Cols[1].Code != fg(Red) {
		t.Errorf("bare = %+v, %v", fm, full)
	}

	// Row code only: each half over the full's.
	fm, _ = apply(t, &f, "y")
	if fm.Row != on(Yellow, Blue) || len(fm.Cols) != 3 {
		t.Errorf("row only = %+v", fm)
	}

	// Every column: alignment and clip as written, widths and unset
	// halves from the full format.
	fm, _ = apply(t, &f, "/g R C/m S")
	want := []Column{
		{Width: 5, Align: 'R'},                          // no ! means wrap
		{Width: 13, Align: 'C', Code: on(Red, Magenta)}, // fg kept, bg set
		{Auto: true, Align: 'S', Code: bg(Yellow)},      // S joins; its own code empty
	}
	if fm.Row != on(Cyan, Green) || fm.Narrow != 30 || !reflect.DeepEqual(fm.Cols, want) {
		t.Errorf("relative = %+v\nwant row c/g, cols %+v", fm, want)
	}

	// Never chained: the next relative format reads the full one.
	fm, _ = apply(t, &f, "")
	if fm.Row != on(Cyan, Blue) || fm.Cols[0].Align != 'L' || !fm.Cols[0].Clip {
		t.Errorf("after a relative, bare = %+v", fm)
	}

	// A new full format replaces the old.
	apply(t, &f, "10L 10R")
	fm, _ = apply(t, &f, "Rd C")
	if fm.Row != (Code{}) || len(fm.Cols) != 2 || fm.Cols[0].Code != fg(Default) {
		t.Errorf("against the new full = %+v", fm)
	}
}

func TestFormatErrors(t *testing.T) {
	for _, tc := range []struct {
		full string // a full format applied first, if any
		spec string
		kind error
		col  int
		msg  string
	}{
		{"", "L R", ErrNoFull, 1, "none came before"},
		{"", "", ErrNoFull, 1, "none came before"},
		{"", "5L R", ErrMixedWidths, 4, "every column a width"},
		{"", "L 5R", ErrMixedWidths, 3, "every column a width"},
		{"", "5L c", ErrToken, 4, "must be the first token"},
		{"", "5L 30", ErrToken, 4, "before the columns"},
		{"", "30 30 5L", ErrToken, 4, "once"},
		{"", "0L", ErrToken, 1, "not a positive width"},
		{"", "0 5L", ErrToken, 1, "not a positive width"},
		{"", "5", ErrNoFull, 1, ""}, // a narrowing alone is relative
		{"", "5X", ErrToken, 2, "'X' is not an alignment"},
		{"", "5", ErrNoFull, 1, ""},
		{"", "55", ErrNoFull, 1, ""},
		{"", "5L #", ErrToken, 4, "'#' does not begin a token"},
		{"", "*", ErrToken, 2, "no alignment"}, // where the letter is missing
		{"", "5L!r", ErrToken, 3, "! must end the token"},
		{"", "5Lx", ErrCode, 3, "'x' is not a colour letter"},
		{"", "5Lr/", ErrCode, 4, "no background"},
		{"", "5S 3L", ErrSpan, 1, "the first has none"},
		{"", "5L 3Sr", ErrSpan, 4, "carries no code"},
		{"", "5L 3S!", ErrSpan, 4, "carries no code"},
		{"", "*L 5L *R", ErrAuto, 7, "one column takes the rest"},
		{"5L 5L", "L", ErrColumnCount, 1, "1 columns where the full format has 2"},
		{"5L 5L", "L L L", ErrColumnCount, 1, "3 columns"},
		{"5L 5L", "20", ErrNarrowRelative, 1, "belongs to the full format"},
		{"5L 5L", "c 20 L L", ErrNarrowRelative, 3, ""},
		{"5L 5L", "S L", ErrSpan, 1, "the first has none"},
		{"5L 5L", "L Sg", ErrSpan, 3, "carries no code"},
	} {
		var f Formats
		if tc.full != "" {
			apply(t, &f, tc.full)
		}
		_, _, err := f.Apply(tc.spec, 1)
		var e *Error
		if !errors.As(err, &e) || !errors.Is(err, tc.kind) || e.Col != tc.col || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("Apply(%q) after %q = %v, want %v at column %d containing %q", tc.spec, tc.full, err, tc.kind, tc.col, tc.msg)
		}
	}
}

// Columns are absolute: a spec that starts at column 6 of its line
// reports from there, in code points.
func TestFormatErrorColumns(t *testing.T) {
	var f Formats
	_, _, err := f.Apply("5L λL", 6)
	var e *Error
	if !errors.As(err, &e) || e.Col != 9 {
		t.Errorf("error %v, want column 9", err)
	}
	_, _, err = f.Apply("5Lλ", 6)
	if !errors.As(err, &e) || e.Col != 8 {
		t.Errorf("error %v, want column 8", err)
	}
}
