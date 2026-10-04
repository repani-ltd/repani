package tbl

import (
	"errors"
	"strings"
	"testing"
)

// build applies each line of src -- ".fmt SPEC" lines, starting a new
// table at each full format, and row lines -- and lays the tables out
// on measure.
func build(t *testing.T, src string, measure int) []LaidRow {
	t.Helper()
	var (
		fs  Formats
		fm  Format
		tb  *Table
		out []LaidRow
	)
	flush := func() {
		if tb != nil {
			laid, err := tb.Layout(measure)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, laid...)
		}
	}
	for n, line := range strings.Split(strings.TrimPrefix(src, "\n"), "\n") {
		if spec, ok := strings.CutPrefix(line, ".fmt"); ok {
			f, full, err := fs.Apply(spec, 5)
			if err != nil {
				t.Fatalf("line %d: %v", n+1, err)
			}
			if full {
				flush()
				tb = new(Table)
			}
			fm = f
			continue
		}
		r, err := ParseRow(line, 1)
		if err != nil {
			t.Fatalf("line %d: %v", n+1, err)
		}
		if err := tb.Add(fm, r); err != nil {
			t.Fatalf("line %d: %v", n+1, err)
		}
	}
	flush()
	return out
}

// text draws the laid rows as plain lines: boxes at their columns,
// gaps blank, a rule as dashes, trailing spaces kept to the measure.
func text(rows []LaidRow, measure int) string {
	var b strings.Builder
	for _, r := range rows {
		switch r.Kind {
		case Blank:
			b.WriteString(strings.Repeat(" ", measure) + "|\n")
		case Rule:
			b.WriteString(strings.Repeat("-", measure) + "|\n")
		}
		for _, line := range r.Lines {
			cells := []rune(strings.Repeat(" ", measure))
			for _, p := range line {
				copy(cells[p.Start:p.End], []rune(p.Text))
			}
			b.WriteString(string(cells) + "|\n")
		}
	}
	return b.String()
}

func check(t *testing.T, rows []LaidRow, measure int, want string) {
	t.Helper()
	if got := text(rows, measure); got != strings.TrimPrefix(want, "\n") {
		t.Errorf("laid out\n%s\nwant\n%s", got, strings.TrimPrefix(want, "\n"))
	}
}

func TestLayoutDepartures(t *testing.T) {
	rows := build(t, `
.fmt w/b 31L 8R
REPANI LIVE | 22:41
.fmt 5L 13L 7L *L
.fmt c
^TIME | DESTINATION | FLIGHT | STATUS
.fmt
22:50 | Athens | A3 903 | :r Gate closed
.fmt L L S Lg
23:40 | Paphos (crew change) | Boarding
.fmt
---

01:05 | Rhodes | A3 912 | On time`, 40)
	check(t, rows, 40, `
REPANI LIVE                        22:41|
TIME  DESTINATION   FLIGHT  STATUS      |
22:50 Athens        A3 903  Gate closed |
23:40 Paphos (crew change)  Boarding    |
----------------------------------------|
                                        |
01:05 Rhodes        A3 912  On time     |
`)
	if r := rows[0]; r.FG != White || r.BG != Blue || r.Lines[0][0].BG != Blue {
		t.Errorf("title bar colours %+v", r)
	}
	if r := rows[1]; r.Role != Header || r.FG != Cyan || r.Lines[0][3].FG != Cyan {
		t.Errorf("header %+v", r)
	}
	if p := rows[2].Lines[0][3]; p.FG != Red || rows[2].FG != Default {
		t.Errorf("status cell %+v, row %v", p, rows[2].FG)
	}
	// S joins columns 1 and 2 (6..19, 20..27): one box 6 to 27, the
	// gap inside.
	if p := rows[3].Lines[0][1]; p.Start != 6 || p.End != 27 || rows[3].Lines[0][2].FG != Green {
		t.Errorf("joined box %+v, last %+v", p, rows[3].Lines[0][2])
	}
}

func TestLayoutShortRowAndLinks(t *testing.T) {
	rows := build(t, `
.fmt *L 8R
ΓΕΩΡΓΙΟΥ ΑΝΔΡΕΑΣ | @tel:+35725101189 25101189
.fmt /b
Makariou & Kosti Palama 187
.fmt
a |`, 40)
	check(t, rows, 40, `
ΓΕΩΡΓΙΟΥ ΑΝΔΡΕΑΣ                25101189|
Makariou & Kosti Palama 187             |
a                                       |
`)
	if p := rows[0].Lines[0][1]; p.Target != "tel:+35725101189" || p.Start != 32 || p.End != 40 {
		t.Errorf("phone box %+v", p)
	}
	// The short row's one cell spans both columns, on blue.
	if p := rows[1].Lines[0][0]; p.Start != 0 || p.End != 40 || p.BG != Blue {
		t.Errorf("address box %+v", p)
	}
	if n := len(rows[2].Lines[0]); n != 2 {
		t.Errorf("a | has %d boxes, want 2: the empty cell keeps its column", n)
	}
}

func TestLayoutWrapAndClip(t *testing.T) {
	rows := build(t, `
.fmt 6L 6L! *R
Isolated thunderstorms | clipped text | ok
.fmt C C! C
mid | x | y`, 20)
	check(t, rows, 20, `
Iso-   clippe     ok|
lated               |
thun-               |
der-                |
storms              |
 mid     x      y   |
`)
	// Every line carries every box, its colours and link included.
	if n := len(rows[0].Lines[4]); n != 3 {
		t.Errorf("last line has %d boxes, want 3", n)
	}
}

func TestLayoutNumberTooWide(t *testing.T) {
	// A number wider than its N box is an error, never a shorter
	// number; a label that is not a number clips as before.
	var fs Formats
	fm, _, _ := fs.Apply("5N", 1)
	for _, s := range []string{"1234567", "(12.5)"} {
		var tb Table
		r, _ := ParseRow(s, 1)
		tb.Add(fm, r)
		if _, err := tb.Layout(5); !errors.Is(err, ErrNumber) {
			t.Errorf("%q in 5N: %v", s, err)
		}
	}
	var tb Table
	r, _ := ParseRow("Amount", 1)
	tb.Add(fm, r)
	if _, err := tb.Layout(5); err != nil {
		t.Errorf("label: %v", err)
	}
}

func TestLayoutNumbers(t *testing.T) {
	rows := build(t, `
.fmt 6L 8N
^Day | Temp
Mon | 25.5
Tue | (3.25)
= Avg | 11.13`, 15)
	// The point sits one place after the widest integer part, the
	// paren slot is reserved, and the header ends at the units.
	check(t, rows, 15, `
Day    Temp    |
Mon      25.5  |
Tue      (3.25)|
Avg      11.13 |
`)
	// An N cell whose box spans has no column metrics: it aligns right.
	rows = build(t, `
.fmt 8N 4L
1.5 | x
12.25`, 13)
	check(t, rows, 13, `
     1.5 x   |
        12.25|
`)
}

func TestLayoutNarrowing(t *testing.T) {
	rows := build(t, `
.fmt g/b 20 *L 5R
narrow | 1`, 40)
	if p := rows[0].Lines[0][1]; p.End != 20 || rows[0].BG != Blue {
		t.Errorf("narrowed to %d, row bg %v", p.End, rows[0].BG)
	}
	// A narrowing wider than the measure is ignored.
	rows = build(t, `
.fmt 60 *L 5R
wide | 1`, 40)
	if p := rows[0].Lines[0][1]; p.End != 40 {
		t.Errorf("box ends at %d, want 40", p.End)
	}
}

func TestLayoutPrecedence(t *testing.T) {
	rows := build(t, `
.fmt 5Lr/y 5L/m *L
a | b | c
.fmt c/b
:d d | e | :/g f`, 20)
	l := rows[0].Lines[0]
	if l[0].FG != Red || l[0].BG != Yellow || l[1].FG != Default || l[1].BG != Magenta {
		t.Errorf("columns: %+v %+v", l[0], l[1])
	}
	l = rows[1].Lines[0]
	switch {
	case l[0].FG != Default || l[0].BG != Blue: // cell d over row c, row b over column y
		t.Errorf("cell 0 %+v", l[0])
	case l[1].FG != Cyan || l[1].BG != Blue: // row over column m
		t.Errorf("cell 1 %+v", l[1])
	case l[2].FG != Cyan || l[2].BG != Green:
		t.Errorf("cell 2 %+v", l[2])
	}
}

func TestTableErrors(t *testing.T) {
	var fs Formats
	fm, _, _ := fs.Apply("5L 5L", 1)
	var tb Table
	r, _ := ParseRow("a | b | c", 1)
	if err := tb.Add(fm, r); !errors.Is(err, ErrCells) || err.(*Error).Col != 9 {
		t.Errorf("three cells in two columns: %v", err)
	}
	r, _ = ParseRow(".. note", 1)
	if err := tb.Add(fm, r); !errors.Is(err, ErrNote) {
		t.Errorf("note first: %v", err)
	}
	r, _ = ParseRow("", 1)
	if err := tb.Add(fm, r); err != nil {
		t.Fatal(err)
	}
	r, _ = ParseRow(".. note", 1)
	if err := tb.Add(fm, r); !errors.Is(err, ErrNote) {
		t.Errorf("note after a blank row only: %v", err)
	}
	other, _, _ := fs.Apply("6L 5L", 1)
	if err := tb.Add(other, r); err == nil {
		t.Error("a row of another grid accepted")
	}
	if err := fm.Fit(10); err == nil || !errors.Is(err, ErrFit) {
		t.Errorf("5+1+5 in 10: %v", err)
	}
	if err := fm.Fit(11); err != nil {
		t.Errorf("5+1+5 in 11: %v", err)
	}
	if rows, err := new(Table).Layout(40); rows != nil || err != nil {
		t.Errorf("empty table: %v, %v", rows, err)
	}
	if _, err := (&Table{entries: []entry{{fm, Row{}}}}).Layout(9); !errors.Is(err, ErrFit) {
		t.Errorf("layout in 9: %v", err)
	}
}
