package raster

import (
	"reflect"
	"testing"
)

func heights(t *testing.T, p *Page, recs []Record, want int) {
	t.Helper()
	if err := p.Apply(recs...); err != nil {
		t.Fatal(err)
	}
	if p.Height() != want {
		t.Fatalf("height %d, want %d", p.Height(), want)
	}
}

// The page of DESIGN.t's table of intents, 24 rows.
func TestRowCount(t *testing.T) {
	var p Page
	heights(t, &p, []Record{Count(24)}, 24)
	if !p.Row(23).IsBlank() || p.Row(23).Index != 23 {
		t.Fatal("grown rows are not blank")
	}

	row10 := mk(10, Segment{Text: pad("rewritten")})
	heights(t, &p, []Record{row10}, 24) // an update keeps the height
	if !reflect.DeepEqual(p.Row(10), row10) {
		t.Fatal("row 10 not written")
	}

	heights(t, &p, []Record{Blank(22), Blank(23)}, 24) // blanking keeps it
	heights(t, &p, []Record{Count(22)}, 22)            // shrinking says so
	heights(t, &p, []Record{mk(30, Segment{Text: pad("x")})}, 31)
	if !p.Row(25).IsBlank() {
		t.Fatal("rows between are not blank")
	}
	heights(t, &p, []Record{Count(0)}, 0)
}

func TestWholePage(t *testing.T) {
	var p Page
	bar := mk(0, Segment{Text: pad(""), BG: Blue})
	if err := p.Apply(Count(5), liveRow, bar); err != nil {
		t.Fatal(err)
	}
	recs := p.Records()
	want := []Record{Count(0), bar, liveRow, Count(5)}
	if !reflect.DeepEqual(recs, want) {
		t.Fatalf("Records\n%v\nwant\n%v", recs, want)
	}

	// Correct against whatever the renderer held.
	var q Page
	if err := q.Apply(Count(40), mk(1, Segment{Text: pad("stale")}), mk(4, Segment{Text: pad(""), BG: Red})); err != nil {
		t.Fatal(err)
	}
	if err := q.Apply(recs...); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(q, p) {
		t.Fatal("whole page left the held page different")
	}
}

func TestApplyIsAllOrNothing(t *testing.T) {
	var p Page
	if err := p.Apply(Count(3), mk(1, Segment{Text: "short"})); err == nil {
		t.Fatal("invalid record applied")
	}
	if p.Height() != 0 {
		t.Fatal("records before the invalid one were applied")
	}
}
