package raster

import (
	"reflect"
	"strings"
	"testing"
)

func put(t *testing.T, d *Draft, col int, s string, fg, bg Color, target string) int {
	t.Helper()
	next, err := d.Put(col, s, fg, bg, target)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

// The worked example, as a producer writes it: the link, then the
// story in red with the spaces around it coloured too, and the age
// at the right edge; no column counted but the age's.
func TestDraftWorkedExample(t *testing.T) {
	d := NewDraft(Default)
	col := put(t, d, 0, "[1]", Default, Default, "888")
	put(t, d, col, " ● Fog closes Larnaca approach       ", Red, Default, "")
	put(t, d, Width-3, "12s", Default, Default, "")
	r, err := d.Row(3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r, liveRow) {
		t.Fatalf("Row\n%+v\nwant\n%+v", r, liveRow)
	}
}

func TestDraftBackgrounds(t *testing.T) {
	d := NewDraft(Default)
	put(t, d, 0, "abc", Red, Yellow, "")
	put(t, d, 6, "def", Green, Cyan, "")
	r, err := d.Row(5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r, bgRow) {
		t.Fatalf("Row\n%+v\nwant\n%+v", r, bgRow)
	}
}

func TestDraftRowBackground(t *testing.T) {
	d := NewDraft(Blue)
	put(t, d, 1, "REPANI LIVE", White, Blue, "")
	r, err := d.Row(0)
	if err != nil {
		t.Fatal(err)
	}
	if want := mk(0, Segment{Text: pad(" REPANI LIVE"), FG: White, BG: Blue}); !reflect.DeepEqual(r, want) {
		t.Fatalf("Row\n%+v\nwant\n%+v", r, want)
	}
}

func TestDraftLinkOneColour(t *testing.T) {
	d := NewDraft(Default)
	col := put(t, d, 0, "ab", Red, Default, "x")
	put(t, d, col, "cd", Green, Default, "x")
	if _, err := d.Row(0); err == nil || !strings.Contains(err.Error(), "a link is one colour") {
		t.Fatalf("Row = %v, want a link in two colours refused", err)
	}
}

func TestPut(t *testing.T) {
	d := NewDraft(Default)
	if col := put(t, d, 0, "Café", Default, Default, ""); col != 4 {
		t.Errorf("decomposed é took %d columns, want 4 after NFC", col)
	}
	before := *d
	for _, tc := range []struct {
		col  int
		s    string
		want string
	}{
		{38, "abc", "past the row"},
		{-1, "a", "past the row"},
		{0, "東京", "wide"},
		{0, "a\tb", "control"},
		{0, "a\xffb", "invalid UTF-8"},
	} {
		if _, err := d.Put(tc.col, tc.s, Red, Default, ""); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Put(%d, %q) = %v, want an error containing %q", tc.col, tc.s, err, tc.want)
		}
	}
	if *d != before {
		t.Error("a failed Put wrote columns")
	}
	if n, err := Columns("ΓΕΩΡΓΙΟΥ ─┼█"); n != 12 || err != nil {
		t.Errorf("Columns = %d, %v; want 12", n, err)
	}
}

// Every valid row, written into a Draft segment by segment, comes
// back unchanged.
func TestDraftRoundTrip(t *testing.T) {
	for _, r := range validRows {
		d := NewDraft(Default)
		for _, s := range r.Segments {
			put(t, d, s.Start, s.Text, s.FG, s.BG, s.Target)
		}
		got, err := d.Row(r.Index)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, r) {
			t.Errorf("round trip\n%+v\nwant\n%+v", got, r)
		}
	}
}
