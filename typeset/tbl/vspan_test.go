package tbl

import (
	"errors"
	"strings"
	"testing"
)

// A label and a key run down two rows while the middle of the grid
// changes shape beneath a joined name: the cover sheet's entity rows.
func TestVSpanLabelAndKey(t *testing.T) {
	rows := build(t, `
.fmt 9L 4L 5L *L 3C
.fmt L L S S C
Investor | Some Fund Name | Y
.fmt ^ L L L ^
RM# | N/A | Est 1953`, 40)
	check(t, rows, 40, `
Investor  Some Fund Name              Y |
          RM#  N/A   Est 1953           |
`)
	if got := rows[1].Cont; len(got) != 2 || got[0] != (Box{0, 0}) || got[1] != (Box{4, 4}) {
		t.Errorf("Cont = %v, want the label and key boxes", got)
	}
	if rows[0].Cont != nil {
		t.Errorf("the first row continues nothing: %v", rows[0].Cont)
	}
	// The continued boxes keep the placing of the cell that began them.
	if p := rows[1].Lines[0][4]; p.Start != 37 || p.End != 40 {
		t.Errorf("continued key box %+v", p)
	}
}

// A comment beside several short rows: its lines run down them, and
// the last row grows by what they leave short.
func TestVSpanTallComment(t *testing.T) {
	rows := build(t, `
.fmt 10L 3L *P
Laws | REQ | The laws and regulations that establish the scheme and its sponsor.
.fmt L L ^
Sponsors | REQ
Deal | YES
.fmt
Outlook | -- | Stable.`, 40)
	check(t, rows, 40, `
Laws       REQ The laws and regulations |
Sponsors   REQ that establish the scheme|
Deal       YES and its sponsor.         |
Outlook    --  Stable.                  |
`)
	rows = build(t, `
.fmt 10L 3L *P
Laws | REQ | The laws and regulations that establish the scheme and its sponsor, with the act of parliament that founded it.
.fmt L L ^
Sponsors | REQ`, 40)
	check(t, rows, 40, `
Laws       REQ The laws and regulations |
Sponsors   REQ that establish the scheme|
               and its sponsor, with    |
               the act of parliament    |
               that founded it.         |
`)
	if len(rows[0].Lines) != 1 || len(rows[1].Lines) != 4 {
		t.Errorf("heights %d and %d, want 1 and the 4 the comment leaves", len(rows[0].Lines), len(rows[1].Lines))
	}
}

// A span over three rows: the box continues through a middle row
// that continues it too.
func TestVSpanThreeRows(t *testing.T) {
	rows := build(t, `
.fmt 12L *L
Overview | one
.fmt ^ L
two
three`, 20)
	check(t, rows, 20, `
Overview     one    |
             two    |
             three  |
`)
	if len(rows[2].Cont) != 1 || rows[2].Cont[0] != (Box{0, 0}) {
		t.Errorf("third row Cont = %v", rows[2].Cont)
	}
}

func TestVSpanFormatErrors(t *testing.T) {
	for _, tc := range []struct {
		full, spec string
		kind       error
		msg        string
	}{
		{"", "5L 5^", ErrVSpan, "relative format"},
		{"", "*^ 5L", ErrVSpan, "relative format"},
		{"5L 5L", "L ^r", ErrVSpan, "no code"},
		{"5L 5L", "L ^!", ErrVSpan, "no code"},
		{"5L 5L 5L", "L ^ S", ErrSpan, "^ column"},
	} {
		var fs Formats
		if tc.full != "" {
			if _, _, err := fs.Apply(tc.full, 1, 1); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err := fs.Apply(tc.spec, 2, 1)
		if !errors.Is(err, tc.kind) || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%q: %v, want %v (%s)", tc.spec, err, tc.kind, tc.msg)
		}
	}
}

func TestVSpanRowErrors(t *testing.T) {
	for _, tc := range []struct {
		name, src, msg string
	}{
		{"no row above", "5L 5L\n.fmt ^ L\nb", "no data row above"},
		{"after a note", "5L 5L\na | b\n.. note\n.fmt ^ L\nc", "no data row above"},
		{"after a blank", "5L 5L\na | b\n\n.fmt ^ L\nc", "no data row above"},
		{"a note continuing", "5L 5L\na | b\n.fmt ^ L\n.. note", "note row continues no box"},
		{"part of a box", "5L 5L 5L\n.fmt L L S\na | b\n.fmt L ^ L\nc | d", "whole box"},
		{"columns in no box", "5L 5L 5L\na | b | c\n.fmt L ^ L\nd", "column 3 is in no box"},
	} {
		var (
			fs  Formats
			fm  Format
			tb  Table
			err error
		)
		lines := strings.Split(tc.src, "\n")
		if fm, _, err = fs.Apply(lines[0], 1, 1); err != nil {
			t.Fatal(err)
		}
		for n, line := range lines[1:] {
			if spec, ok := strings.CutPrefix(line, ".fmt"); ok {
				if fm, _, err = fs.Apply(spec, n+2, 5); err != nil {
					t.Fatal(err)
				}
				continue
			}
			r, perr := ParseRow(line, n+2, 1)
			if perr != nil {
				t.Fatal(perr)
			}
			if err = tb.Add(fm, r); err != nil {
				break
			}
		}
		if !errors.Is(err, ErrVSpan) || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%s: %v, want ErrVSpan (%s)", tc.name, err, tc.msg)
		}
	}
}
