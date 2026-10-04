package tbl

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCode(t *testing.T) {
	for i := range len(letters) {
		c := Color(i)
		l := string(letter(c))
		for s, want := range map[string]Code{
			l:           {FG: c, HasFG: true},
			"/" + l:     {BG: c, HasBG: true},
			l + "/" + l: {FG: c, BG: c, HasFG: true, HasBG: true},
			"w/" + l:    {FG: White, BG: c, HasFG: true, HasBG: true},
			l + "/d":    {FG: c, BG: Default, HasFG: true, HasBG: true},
		} {
			got, err := parseCode(s, 1)
			if err != nil || got != want {
				t.Errorf("parseCode(%q) = %+v, %v; want %+v", s, got, err, want)
			}
			if got.String() != s {
				t.Errorf("Code(%q).String() = %q", s, got.String())
			}
		}
	}
}

// Errors name the column of the code point at fault, counted from
// the column the code starts at.
func TestParseCodeErrors(t *testing.T) {
	for _, tc := range []struct {
		s    string
		col  int // where the code starts
		want int // where the error is
		msg  string
	}{
		{"", 7, 7, "empty"},
		{"/", 7, 7, "no background"},
		{"r/", 7, 8, "no background"},
		{"x", 7, 7, "'x' is not a colour letter"},
		{"R", 7, 7, "'R' is not a colour letter"},
		{"/x", 7, 8, "'x' is not a colour letter"},
		{"rr", 7, 8, `'r' after "r"`},
		{"r/bb", 7, 10, `'b' after "r/b"`},
		{"r//b", 7, 9, "'/' is not a colour letter"},
		{"λ", 7, 7, "'λ' is not a colour letter"},
		{"r/bλ", 3, 6, `'λ' after "r/b"`},
	} {
		_, err := parseCode(tc.s, tc.col)
		var e *Error
		if !errors.As(err, &e) || !errors.Is(err, ErrCode) {
			t.Errorf("parseCode(%q) = %v, want an ErrCode *Error", tc.s, err)
			continue
		}
		if e.Col != tc.want || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("parseCode(%q) at %d = column %d %q, want column %d containing %q", tc.s, tc.col, e.Col, err, tc.want, tc.msg)
		}
	}
}

func TestCodeOver(t *testing.T) {
	cell := Code{FG: Red, HasFG: true}
	row := Code{FG: Cyan, BG: Blue, HasFG: true, HasBG: true}
	col := Code{BG: Yellow, HasBG: true}
	if got, want := cell.Over(row.Over(col)), (Code{FG: Red, BG: Blue, HasFG: true, HasBG: true}); got != want {
		t.Errorf("cell over row over column = %+v, want %+v", got, want)
	}
	if got, want := cell.Over(col), (Code{FG: Red, BG: Yellow, HasFG: true, HasBG: true}); got != want {
		t.Errorf("cell over column = %+v, want %+v", got, want)
	}
	// A set default is a colour: it wins, where unset falls through.
	d := Code{FG: Default, HasFG: true}
	if got := d.Over(row); got.FG != Default || got.BG != Blue {
		t.Errorf("d over row = %+v, want default on blue", got)
	}
	if got := (Code{}).Over(Code{}); got.HasFG || got.HasBG {
		t.Errorf("unset over unset = %+v, want unset", got)
	}
}

func TestColorNames(t *testing.T) {
	if Cyan.String() != "cyan" || letter(Cyan) != 'c' || Color(9).String() != "Color(9)" || letter(Color(9)) != '?' {
		t.Error("colour names")
	}
}
