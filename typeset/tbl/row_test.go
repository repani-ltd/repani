package tbl

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func parse(t *testing.T, line string) Row {
	t.Helper()
	r, err := ParseRow(line, 1)
	if err != nil {
		t.Fatalf("ParseRow(%q): %v", line, err)
	}
	return r
}

func TestRowKinds(t *testing.T) {
	for line, want := range map[string]Row{
		"":               {Kind: Blank},
		"   \t ":         {Kind: Blank},
		"---":            {Kind: Rule},
		"  ---  ":        {Kind: Rule},
		"----":           {Kind: Data, Cells: []Cell{{Text: "----"}}},
		"a":              {Kind: Data, Cells: []Cell{{Text: "a"}}},
		"|":              {Kind: Data, Cells: []Cell{{}, {}}},
		"a | |":          {Kind: Data, Cells: []Cell{{Text: "a"}, {}, {}}},
		"a |":            {Kind: Data, Cells: []Cell{{Text: "a"}, {}}},
		" a  b |c ":      {Kind: Data, Cells: []Cell{{Text: "a  b"}, {Text: "c"}}},
		"^TIME | TO":     {Kind: Data, Role: Header, Cells: []Cell{{Text: "TIME"}, {Text: "TO"}}},
		"= total | 1.50": {Kind: Data, Role: Total, Cells: []Cell{{Text: "total"}, {Text: "1.50"}}},
		".. a note":      {Kind: Data, Role: Note, Cells: []Cell{{Text: "a note"}}},
		"...":            {Kind: Data, Role: Note, Cells: []Cell{{Text: "."}}},
		"^":              {Kind: Data, Role: Header, Cells: []Cell{{}}},
	} {
		if got := parse(t, line); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseRow(%q) = %+v, want %+v", line, got, want)
		}
	}
}

func TestCellMarks(t *testing.T) {
	r := parse(t, "^~c TIME | ~r Gate closed | @888 [1] | ~/y @tel:+35725101189 25101189 | @x ~d plain |~y|")
	want := []Cell{
		{Text: "TIME", Code: fg(Cyan)},
		{Text: "Gate closed", Code: fg(Red)},
		{Text: "[1]", Target: "888"},
		{Text: "25101189", Code: bg(Yellow), Target: "tel:+35725101189"},
		{Text: "plain", Code: fg(Default), Target: "x"},
		{Code: fg(Yellow)},
		{},
	}
	if r.Role != Header || !reflect.DeepEqual(r.Cells, want) {
		t.Errorf("cells %+v\nwant %+v", r.Cells, want)
	}
	if got := parse(t, "@news/1?q=a%20b#top x"); got.Cells[0].Target != "news/1?q=a%20b#top" {
		t.Errorf("target %q", got.Cells[0].Target)
	}
	if got := parse(t, "@"+strings.Repeat("a", MaxTarget)); len(got.Cells[0].Target) != MaxTarget {
		t.Error("a target of MaxTarget bytes refused")
	}
}

func TestRowErrors(t *testing.T) {
	for _, tc := range []struct {
		line string
		col  int // where the line starts
		kind error
		at   int
		msg  string
	}{
		{"a | ~x b", 1, ErrCode, 6, "'x' is not a colour letter"},
		{"~ b", 1, ErrCode, 2, "empty"},
		{"~rGate", 1, ErrCode, 3, `'G' after "r"`},
		{"~r ~g b", 1, ErrMark, 4, "a second ~"},
		{"@a @b c", 1, ErrMark, 4, "a second @"},
		{"@ x", 1, ErrTarget, 2, "no target"},
		{"@a b|c", 1, nil, 0, ""}, // fine: | ends the cell
		{"x | @a%2 y", 1, ErrTarget, 7, "two hex digits"},
		{"x | @a%zz y", 1, ErrTarget, 7, "two hex digits"},
		{"@café x", 1, ErrTarget, 5, "'é' is not allowed"},
		{"@a\"b x", 1, ErrTarget, 3, `'"' is not allowed`},
		{"@" + strings.Repeat("a", MaxTarget+1), 1, ErrTarget, 2, "256 bytes"},
		{"  ^ λ | ~q", 5, ErrCode, 14, "'q' is not a colour letter"}, // columns 5..14, λ one
	} {
		_, err := ParseRow(tc.line, tc.col)
		if tc.at == 0 {
			if err != nil {
				t.Errorf("ParseRow(%q) = %v, want no error", tc.line, err)
			}
			continue
		}
		var e *Error
		if !errors.As(err, &e) || !errors.Is(err, tc.kind) || e.Col != tc.at || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("ParseRow(%q) = %v, want %v at column %d containing %q", tc.line, err, tc.kind, tc.at, tc.msg)
		}
	}
}
