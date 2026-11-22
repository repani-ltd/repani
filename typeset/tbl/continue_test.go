package tbl

import (
	"errors"
	"strings"
	"testing"
)

// shape is a box's structure: its columns, whether it continues the
// box above, its first row and how many rows it covers.
type shape struct {
	Box
	cont      bool
	top, rows int
}

func shapes(r LaidRow) []shape {
	out := make([]shape, len(r.Boxes))
	for i, b := range r.Boxes {
		out[i] = shape{b.Box, b.Cont, b.Top, b.Rows}
	}
	return out
}

// Boxes running down rows grow the rows they cover in the order of the
// row they end on, so a table and its mirror image are as tall, with
// no blank lines: the review's case of a five-line and a four-line
// box, the shorter ending first.
func TestContinueRunOrder(t *testing.T) {
	for _, src := range []string{`
.fmt 3L 3L 3L
x1 x2 x3 x4 x5 | y1 y2 y3 y4 | c
.fmt ^ ^ L
d
.fmt ^ L L
e | f`, `
.fmt 3L 3L 3L
y1 y2 y3 y4 | x1 x2 x3 x4 x5 | c
.fmt ^ ^ L
d
.fmt L ^ L
e | f`} {
		lines := 0
		for _, r := range build(t, src, 11) {
			lines += len(r.Lines)
		}
		if lines != 5 {
			t.Errorf("%d lines, want 5:%s", lines, src)
		}
	}
}

// A label and a key run down two rows while the middle of the grid
// changes shape beneath a joined name: the cover sheet's entity rows.
func TestContinueLabelAndKey(t *testing.T) {
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
	// The label and the key begin on row 0 and cover two rows; row 1
	// continues them, and its own three cells cover one.
	label, key := shape{Box{0, 0}, false, 0, 2}, shape{Box{4, 4}, false, 0, 2}
	if b := shapes(rows[0]); len(b) != 3 || b[0] != label || b[1] != (shape{Box{1, 3}, false, 0, 1}) || b[2] != key {
		t.Errorf("row 0 boxes %+v", b)
	}
	label.cont, key.cont = true, true
	if b := shapes(rows[1]); len(b) != 5 || b[0] != label || b[1] != (shape{Box{1, 1}, false, 1, 1}) || b[4] != key {
		t.Errorf("row 1 boxes %+v", b)
	}
	// A continued box keeps the settings of the cell that began it.
	if b := rows[1].Boxes[4]; b.Cell != -1 || b.Align != 'C' {
		t.Errorf("continued key box %+v", b)
	}
	// The continued boxes keep the placing of the cell that began them.
	if p := rows[1].Lines[0][4]; p.Start != 37 || p.End != 40 {
		t.Errorf("continued key box %+v", p)
	}
}

// A comment beside several short rows: its lines run down them, and
// the last row grows by what they leave short.
func TestContinueTallComment(t *testing.T) {
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

// A note row continues a box like any row, and its own cells lay out
// L whatever their columns: the cover sheet's identifier line, set
// small beside the label it belongs to.
func TestContinueNoteContinues(t *testing.T) {
	rows := build(t, `
.fmt 9L 5R *L
Investor | Fund | Pension
.fmt ^ R L
.. RM# | N/A
.fmt
Sponsor | State | Government`, 30)
	check(t, rows, 30, `
Investor   Fund Pension       |
          RM#   N/A           |
Sponsor   State Government    |
`)
	if b := shapes(rows[1]); rows[1].Role != Note || len(b) != 3 || b[0] != (shape{Box{0, 0}, true, 0, 2}) {
		t.Errorf("note row %+v: a note continuing the label", rows[1])
	}
}

// A box over three rows: it continues through a middle row that
// continues it too.
func TestContinueThreeRows(t *testing.T) {
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
	if b := shapes(rows[2]); len(b) != 2 || b[0] != (shape{Box{0, 0}, true, 0, 3}) {
		t.Errorf("third row boxes %+v", b)
	}
}

func TestContinueFormatErrors(t *testing.T) {
	for _, tc := range []struct {
		full, spec string
		kind       error
		msg        string
	}{
		{"", "5L 5^", ErrContinue, "relative format"},
		{"", "*^ 5L", ErrContinue, "relative format"},
		{"5L 5L", "L ^r", ErrContinue, "no code"},
		{"5L 5L", "L ^!", ErrContinue, "no code"},
		{"5L 5L 5L", "L ^ S", ErrSpan, "^ column"},
		{"5L", "^", ErrContinue, "no cell"},
		{"5L 5L", "^ ^", ErrContinue, "no cell"},
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

func TestContinueRowErrors(t *testing.T) {
	for _, tc := range []struct {
		name, src, msg string
	}{
		{"no row above", "5L 5L\n.fmt ^ L\nb", "no data row above"},
		{"after a blank", "5L 5L\na | b\n\n.fmt ^ L\nc", "no data row above"},
		{"after a rule", "5L 5L\na | b\n---\n.fmt ^ L\nc", "no data row above"},
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
		if !errors.Is(err, ErrContinue) || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%s: %v, want ErrContinue (%s)", tc.name, err, tc.msg)
		}
	}
}
