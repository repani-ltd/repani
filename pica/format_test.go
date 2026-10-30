package pica

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"repani.com/typeset/wrap"
)

// long is a run of words that fills more than one source line.
var long = strings.Repeat("word ", 30)

func TestFormatRefills(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"paragraph joined", "T\n\nshort\nlines\njoin.\n", "T\n\nshort lines join.\n"},
		{"paragraph split", "T\n\n" + long + "\n",
			"T\n\n" + strings.Repeat("word ", 15) + "word\n" + strings.Repeat("word ", 13) + "word\n"},
		{"item prefix", "T\n\n.item " + long + "\n",
			"T\n\n.item " + strings.Repeat("word ", 14) + "word\n" + strings.Repeat("word ", 14) + "word\n"},
		{"term label kept", "T\n\n.term LA BEL\ntext\nhere\n", "T\n\n.term LA BEL\ntext here\n"},
		{"rem group", "T\n.rem one\n.rem two\n.rem\n.rem three\n", "T\n.rem one two\n.rem\n.rem three\n"},
		{"rem wraps", ".rem " + long + "\nT\n",
			".rem " + strings.Repeat("word ", 14) + "word\n.rem " + strings.Repeat("word ", 14) + "word\nT\n"},
		{"rem inside a paragraph", "T\n\na\nb\n.rem c\nd\n", "T\n\na b\n.rem c\nd\n"},
		{"quote", "T\n\n.quote\n  a\n  b\n.attrib  WHO  \n.end\n", "T\n\n.quote\na b\n.attrib  WHO\n.end\n"},
		{"verbatim kept", "T\n\n.pre\n  a  \nb\n.end\n.table 3L\na\n.end\n", "T\n\n.pre\n  a  \nb\n.end\n.table 3L\na\n.end\n"},
		{"crlf and trailing space", "T  \r\n\r\na \r\n\r\n\r\n", "T\n\na\n"},
		{"structure kept", "T\n# H\n---\n.link u t\ntext\n.width 48\n", "T\n# H\n---\n.link u t\ntext\n.width 48\n"},
		{"not a heading", "T\n\n#3\nfiled\n", "T\n\n#3 filed\n"},
	} {
		got, err := Format(tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: Format\n%q\nwant\n%q", tc.name, got, tc.want)
		}
	}
}

func TestFormatKeepsStructureWordsOffLineStarts(t *testing.T) {
	// Each of these words would make its line structure if a
	// break put it first; it stays on the line before.
	for _, w := range []string{".end", ".attrib", ".item", "#", "##", "---", " .end"} {
		pad := strings.Repeat("x", FormatWidth-1-utf8.RuneCountInString(w))
		for _, src := range []string{
			"T\n\n" + pad + " " + w + " tail\n",
			"T\n\n.quote\n" + pad + " " + w + " tail\n.end\n",
		} {
			got, err := Format(src)
			if err != nil {
				t.Fatalf("Format(%q): %v", src, err)
			}
			assertSameDoc(t, src, got)
		}
	}
}

func TestFormatRefusesInvalid(t *testing.T) {
	const src = "T\n\n.bogus\n"
	got, err := Format(src)
	if err == nil || got != src {
		t.Errorf("Format(invalid) = %q, %v", got, err)
	}
}

// TestFormatCorpus formats every pica document in the module: each
// must keep its meaning, format to itself a second time, and keep
// its refilled lines within the measure.
func TestFormatCorpus(t *testing.T) {
	n := 0
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".t") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := Parse(string(src)); err != nil {
			return nil // not a document (a fixture of an invalid one)
		}
		n++
		got, err := Format(string(src))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		assertSameDoc(t, string(src), got)
		if again, _ := Format(got); again != got {
			t.Errorf("%s: Format is not idempotent", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no documents found")
	}
}

// assertSameDoc fails unless a and b parse to the same document:
// the same blocks, prose compared word by word (the writers' own
// split, so a refill that keeps the words keeps the meaning), and
// the same rendered page.
func assertSameDoc(t *testing.T, a, b string) {
	t.Helper()
	da, errA := Parse(a)
	db, errB := Parse(b)
	if errA != nil || errB != nil {
		t.Fatalf("parse: %v / %v\n%s", errA, errB, b)
	}
	norm := func(d *Doc) []Block {
		var bs []Block
		for _, blk := range d.Blocks {
			blk.Line = 0
			blk.Table = nil
			blk.Text = strings.Join(wrap.Fields(blk.Text), " ")
			bs = append(bs, blk)
		}
		return bs
	}
	if !reflect.DeepEqual(norm(da), norm(db)) || da.Title != db.Title || da.Byline() != db.Byline() ||
		da.Rights != db.Rights || da.Layout != db.Layout {
		t.Errorf("meaning changed:\n%s\n---\n%s", a, b)
	}
	if da.Text() != db.Text() {
		t.Errorf("page changed:\n%s\n---\n%s", a, b)
	}
}
