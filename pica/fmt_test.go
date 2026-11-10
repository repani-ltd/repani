package pica

import (
	"errors"
	"strings"
	"testing"

	"repani.com/typeset/tbl"
)

// A .fmt block with a relative format, a ^ span and a second grid:
// the cover sheet's entity box and its overview under it.
const coverBlock = `T

.fmt 10L 5L 5L *L
^Role | Entity
.fmt L L S S
Investor | Example Pension Fund
.fmt ^ L L L
RM# | N/A | 1953
.fmt 10L *L
Overview | A defined contribution scheme.
.end

.width 40
`

func TestFmtBlockGrids(t *testing.T) {
	d := mustParse(t, coverBlock)
	if len(d.Blocks) != 2 || d.Blocks[0].Kind != TableBlk || d.Blocks[1].Kind != TableBlk {
		t.Fatalf("blocks %+v, want two table grids", d.Blocks)
	}
	if d.Blocks[0].Tight || !d.Blocks[1].Tight {
		t.Errorf("tight %v %v: the second grid stacks under the first", d.Blocks[0].Tight, d.Blocks[1].Tight)
	}
	want := `T

Role       Entity
---------- ----- ----- -----------------
Investor   Example Pension Fund
           RM#   N/A   1953
Overview   A defined contribution
           scheme.
`
	if got := d.Text(); got != want {
		t.Errorf("text\n%s\nwant\n%s", got, want)
	}
	tl, err := d.Blocks[0].Table.Layout(40)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Rows) != 2 || tl.Rows[0].Cont || !tl.Rows[1].Cont {
		t.Errorf("rows %+v: the identifier row continues the label", tl.Rows)
	}
}

func TestFmtBlockHTMLRowspan(t *testing.T) {
	h := mustParse(t, coverBlock).HTML()
	for _, want := range []string{
		`<td rowspan="2">Investor</td><td colspan="3">Example Pension Fund</td>`,
		`<tr><td>RM#</td><td>N/A</td><td>1953</td></tr>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("HTML lacks %s:\n%s", want, h)
		}
	}
}

// A comment beside short rows runs down them; when it needs more
// lines, the last row it covers grows.
func TestFmtSpanGrowsLastRow(t *testing.T) {
	d := mustParse(t, `T

.fmt 8L 3L *L
Laws | REQ | The act that founded the scheme and its sponsor.
.fmt L L ^
Major | REQ
.end

.width 30
`)
	want := `T

Laws     REQ The act that
Major    REQ founded the
             scheme and its
             sponsor.
`
	if got := d.Text(); got != want {
		t.Errorf("text\n%s\nwant\n%s", got, want)
	}
}

func TestFmtBlockErrors(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      error
	}{
		{"relative first", "T\n\n.fmt L L\na | b\n.end\n", ErrTableRelative},
		{"empty grid mid-block", "T\n\n.fmt 4L\na\n.fmt 4L 4L\n.fmt 4L\nb\n.end\n", ErrTableEmpty},
		{"empty grid at the end", "T\n\n.fmt 4L\na\n.fmt 4L 4L\n.end\n", ErrTableEmpty},
		{"caret with no row above", "T\n\n.fmt 4L 4L\n.fmt ^ L\na\n.end\n", tbl.ErrVSpan},
		{"caret across grids", "T\n\n.fmt 4L 4L\na | b\n.fmt 4L 4L\n.fmt ^ L\nc\n.end\n", tbl.ErrVSpan},
		{"the old word", "T\n\n.table 4L\na\n.end\n", ErrUnknownCommand},
	} {
		if _, err := Parse(tc.src); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}
