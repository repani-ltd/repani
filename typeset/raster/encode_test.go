package raster

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// pad fills s with spaces to Width code points.
func pad(s string) string {
	return s + strings.Repeat(" ", Width-utf8.RuneCountInString(s))
}

// mk returns row index from segments given by text and style alone,
// filling in their columns.
func mk(index int, segs ...Segment) Row {
	col := 0
	for i := range segs {
		segs[i].Start = col
		col += utf8.RuneCountInString(segs[i].Text)
		segs[i].End = col
	}
	return Row{Index: index, Segments: segs}
}

// liveRow is RASTER.t's worked example: row 3 of the mock Live
// board, its [1] a link to 888, red from the mark to the age.
var liveRow = mk(3,
	Segment{Text: "[1]", Target: "888"},
	Segment{Text: " ● Fog closes Larnaca approach    ", FG: Red},
	Segment{Text: "12s"},
)

func TestWorkedExample(t *testing.T) {
	b, err := Append(nil, liveRow)
	if err != nil {
		t.Fatal(err)
	}
	head := []byte{
		0x03, 0x00, 0x03, // row, role, k
		0x03, 0x00, 0x00, 0x03, 0x38, 0x38, 0x38, // [1]: end, fg, bg, tlen, "888"
		0x25, 0x01, 0x00, 0x00, // the story in red to column 37
		0x28, 0x00, 0x00, 0x00, // 12s to column 40
	}
	if !bytes.HasPrefix(b, head) || string(b[len(head):]) != liveRow.Text() {
		t.Fatalf("encoding\n% x\nwant\n% x <text>", b, head)
	}
	if len(b) != 60 {
		t.Errorf("%d bytes, want 60", len(b))
	}
}

// bgRow is abc in red on yellow, three spaces in the defaults, def in
// green on cyan: every boundary a change of background.
var bgRow = mk(5,
	Segment{Text: "abc", FG: Red, BG: Yellow},
	Segment{Text: "   "},
	Segment{Text: "def", FG: Green, BG: Cyan},
	Segment{Text: strings.Repeat(" ", 31)},
)

var validRows = []Row{
	liveRow,
	bgRow,
	Blank(9),
	mk(0, Segment{Text: pad(" REPANI LIVE"), FG: White, BG: Blue}),
	mk(7,
		Segment{Text: "ΚΑΛΗΜΕΡΑ", Target: "tel:+35725101189"},
		Segment{Text: " ─┼█▀▄░▒▓ ", Target: "geo:34.68,33.04"},
		Segment{Text: "°µ€", FG: Cyan},
		Segment{Text: "     ", BG: Red, Target: "news/1?q=a%20b#top"}, // a link of spaces
		Segment{Text: pad("")[:14], BG: Red},                          // same colours, after a link
	),
}

func TestRoundTrip(t *testing.T) {
	recs := []Record{Count(0)}
	for _, r := range validRows {
		recs = append(recs, r)
	}
	recs = append(recs, Count(24), Count(255))
	b, err := Append(nil, recs...)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, recs) {
		t.Fatalf("decoded\n%v\nwant\n%v", got, recs)
	}
	again, _ := Append(nil, got...)
	if !bytes.Equal(again, b) {
		t.Fatal("re-encoding differs")
	}
}

func TestRowCheck(t *testing.T) {
	rest := func(n int) Segment { return Segment{Text: strings.Repeat(" ", n)} }
	for _, tc := range []struct {
		name string
		row  Row
		want string
	}{
		{"row 255", mk(255, Segment{Text: pad("")}), "row 255"},
		{"no segments", Row{}, "no segments"},
		{"short", mk(0, Segment{Text: "abc"}), "end at column 3"},
		{"gap", Row{Segments: []Segment{{Start: 1, End: 40, Text: pad("")[1:]}}}, "do not continue"},
		{"text for other columns", Row{Segments: []Segment{{End: 40, Text: "abc"}}}, "3 code points of text for 40"},
		{"control", mk(0, Segment{Text: pad("a\tb")}), "control"},
		{"combining", mk(0, Segment{Text: pad("é")}), "combining"},
		{"variation selector", mk(0, Segment{Text: pad("☀️")}), "combining"},
		{"zero-width joiner", mk(0, Segment{Text: pad("a‍b")}), "zero-width"},
		{"wide", mk(0, Segment{Text: pad("東京")}), "wide"},
		{"emoji", mk(0, Segment{Text: pad("🚌")}), "wide"},
		{"not NFC", mk(0, Segment{Text: pad("Å")}), "NFC"}, // ANGSTROM SIGN, NFC Å
		{"invalid UTF-8", mk(0, Segment{Text: pad("a\xffb")}), "invalid UTF-8"},
		{"reserved fg", mk(0, Segment{Text: pad("a"), FG: 8}), "reserved"},
		{"reserved bg", mk(0, Segment{Text: pad("a"), BG: 9}), "reserved"},
		{"spaces in a colour", mk(0, Segment{Text: pad(""), FG: Red}), "foreground must be default"},
		{"same colours twice", mk(0, Segment{Text: "abc"}, Segment{Text: "def"}, rest(34)), "same colours"},
		{"foreground change on a space", mk(0, Segment{Text: "abc ", FG: Red}, Segment{Text: "  def", FG: Green}, rest(31)), "starts on a space"},
		{"spaces before a colour", mk(0, Segment{Text: "   "}, Segment{Text: "abc" + strings.Repeat(" ", 34), FG: Red}), "follows a segment of spaces"},
		{"adjoining one target", mk(0, Segment{Text: "ab", Target: "a"}, Segment{Text: "cd", FG: Red, Target: "a"}, rest(36)), "same target"},
		{"empty target in a link", mk(0, Segment{Text: pad(""), Target: strings.Repeat("a", 256)}), "256 bytes"},
		{"space in target", mk(0, Segment{Text: pad(""), Target: "a b"}), "not allowed"},
		{"non-ASCII target", mk(0, Segment{Text: pad(""), Target: "λ"}), "not allowed"},
		{"bad escape", mk(0, Segment{Text: pad(""), Target: "a%2"}), "two hex"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.row.Check()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Check() = %v, want an error containing %q", err, tc.want)
			}
		})
	}
	for _, r := range validRows {
		if err := r.Check(); err != nil {
			t.Errorf("valid row: %v", err)
		}
	}
	if err := Count(256).Check(); err == nil {
		t.Error("Count(256) passed")
	}
}

func TestDecodeErrors(t *testing.T) {
	good, _ := Append(nil, liveRow)
	for _, tc := range []struct {
		name string
		b    []byte
		want string
	}{
		{"count truncated", []byte{0xFF}, "truncated"},
		{"no segments", []byte{0, 0, 0}, "0 segments"},
		{"segment ends before it starts", []byte{0, 0, 2, 5, 0, 0, 0, 5, 0, 0, 0}, "ends at column 5 after starting at 5"},
		{"segments short of the row", append([]byte{0, 0, 1, 39, 0, 0, 0}, pad("")[:39]...), "end at column 39"},
		{"header truncated", good[:3], "truncated"},
		{"target truncated", good[:6], "truncated"},
		{"text truncated", good[:len(good)-1], "truncated"},
		{"text cut inside a code point", good[:23], "truncated"},
		{"non-canonical", append([]byte{0, 0, 1, 40, 1, 0, 0}, pad("")...), "foreground must be default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(tc.b)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Decode = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

// Roles travel in the row and survive a round trip, a value not in
// the registry included: a renderer reads it as None, but the bytes
// keep it.
func TestRoles(t *testing.T) {
	header := mk(0, Segment{Text: pad("TIME  DESTINATION"), FG: Cyan})
	header.Role = Header
	unknown := mk(1, Segment{Text: pad("")})
	unknown.Role = 9
	b, err := Append(nil, header, unknown)
	if err != nil {
		t.Fatal(err)
	}
	if b[1] != 1 {
		t.Errorf("role byte %d, want 1", b[1])
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Record{header, unknown}) {
		t.Fatalf("decoded %v", got)
	}
	if unknown.IsBlank() {
		t.Error("a row with a role is blank")
	}
	if Header.String() != "header" || Role(9).String() != "Role(9)" {
		t.Error("role names")
	}
}

// Every position counts from zero, and from one origin: byte offsets
// from the start of the input, columns from the start of the row.
func TestPositions(t *testing.T) {
	good, _ := Append(nil, liveRow)
	_, err := Decode(append(good, good[:5]...))
	if want := "record at byte 60: truncated at byte 65"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Decode = %v, want %q", err, want)
	}
	r := mk(0, Segment{Text: "ab", FG: Red}, Segment{Text: "c東" + strings.Repeat(" ", 36), FG: Green})
	if want := "segment 1: text: column 3: wide"; r.Check() == nil || !strings.Contains(r.Check().Error(), want) {
		t.Errorf("Check = %v, want %q", r.Check(), want)
	}
}

func TestAppendKeepsBufferOnError(t *testing.T) {
	b, err := Append([]byte{1, 2}, Count(3), mk(0, Segment{Text: "short"}))
	if err == nil || !bytes.Equal(b, []byte{1, 2}) {
		t.Fatalf("Append = % x, %v", b, err)
	}
}
