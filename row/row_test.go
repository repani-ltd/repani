package row

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
)

// vectors are the known answers in testdata/vectors.json, shared with
// row.js (js_test.go). The valid and labelled ones were written by an
// independent encoder, from ROW.t alone; the lenient and bad ones by
// hand from ROW.t's tables. A changed vector is a changed format.
type vectors struct {
	Valid []struct {
		Text string `json:"text"`
		Hex  string `json:"hex"`
	} `json:"valid"`
	Labeled []struct {
		Text string `json:"text"`
		Rows []struct {
			Label string `json:"label"`
			Hex   string `json:"hex"`
		} `json:"rows"`
	} `json:"labeled"`
	Lenient []struct {
		Text      string `json:"text"`
		Canonical string `json:"canonical"`
	} `json:"lenient"`
	BadBinary []struct {
		Hex  string `json:"hex"`
		Code string `json:"code"`
		Path string `json:"path"`
	} `json:"badBinary"`
	BadText    []badText `json:"badText"`
	BadLabeled []badText `json:"badLabeled"`
}

type badText struct {
	Text string `json:"text"`
	Code string `json:"code"`
	Path string `json:"path"`
	Line int    `json:"line"`
}

func load(t *testing.T) vectors {
	t.Helper()
	b, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestValid: every valid row decodes, re-encodes, formats and parses
// to exactly its bytes and text.
func TestValid(t *testing.T) {
	for _, c := range load(t).Valid {
		b, _ := hex.DecodeString(c.Hex)
		r, err := Decode(b)
		if err != nil {
			t.Errorf("decode %s: %v", c.Hex, err)
			continue
		}
		if got, err := Encode(r); err != nil || !bytes.Equal(got, b) {
			t.Errorf("encode(decode(%s)) = %x, %v", c.Hex, got, err)
		}
		if got, err := Format(r); err != nil || got != c.Text {
			t.Errorf("format(decode(%s)) = %q, %v\nwant %q", c.Hex, got, err, c.Text)
		}
		p, err := Parse(c.Text)
		if err != nil {
			t.Errorf("parse %q: %v", c.Text, err)
			continue
		}
		if got, err := Encode(p); err != nil || !bytes.Equal(got, b) {
			t.Errorf("encode(parse(%q)) = %x, %v; want %s", c.Text, got, err, c.Hex)
		}
	}
}

func TestLabeled(t *testing.T) {
	for _, c := range load(t).Labeled {
		var rows []Labeled
		for _, r := range c.Rows {
			b, _ := hex.DecodeString(r.Hex)
			row, err := Decode(b)
			if err != nil {
				t.Fatalf("decode %s: %v", r.Hex, err)
			}
			rows = append(rows, Labeled{Label: r.Label, Row: row})
		}
		if got, err := FormatLabeled(rows); err != nil || got != c.Text {
			t.Errorf("formatLabeled = %q, %v\nwant %q", got, err, c.Text)
		}
		got, err := ParseLabeled(c.Text)
		if err != nil {
			t.Errorf("parseLabeled %q: %v", c.Text, err)
			continue
		}
		if len(got) != len(c.Rows) {
			t.Errorf("parseLabeled: %d rows, want %d", len(got), len(c.Rows))
			continue
		}
		for i, r := range c.Rows {
			b, _ := Encode(got[i].Row)
			if got[i].Label != r.Label || hex.EncodeToString(b) != r.Hex {
				t.Errorf("row %d = %q %x, want %q %s", i, got[i].Label, b, r.Label, r.Hex)
			}
		}
	}
}

// TestLenient: a text in another spelling reads as its canonical one.
func TestLenient(t *testing.T) {
	for _, c := range load(t).Lenient {
		r, err := Parse(c.Text)
		if err != nil {
			t.Errorf("parse %q: %v", c.Text, err)
			continue
		}
		if got, err := Format(r); err != nil || got != c.Canonical {
			t.Errorf("format(parse(%q)) = %q, %v; want %q", c.Text, got, err, c.Canonical)
		}
	}
}

func code(err error) (*Error, bool) {
	var e *Error
	return e, errors.As(err, &e)
}

func TestBad(t *testing.T) {
	v := load(t)
	for _, c := range v.BadBinary {
		b, _ := hex.DecodeString(c.Hex)
		_, err := Decode(b)
		e, ok := code(err)
		if !ok || e.Code != c.Code || pathString(e.Path) != c.Path {
			t.Errorf("decode %s: %v; want %s at %q", c.Hex, err, c.Code, c.Path)
		}
	}
	for _, c := range v.BadText {
		_, err := Parse(c.Text)
		e, ok := code(err)
		if !ok || e.Code != c.Code || pathString(e.Path) != c.Path || e.Line != c.Line {
			t.Errorf("parse %q: %v; want %s at %q line %d", c.Text, err, c.Code, c.Path, c.Line)
		}
	}
	for _, c := range v.BadLabeled {
		_, err := ParseLabeled(c.Text)
		e, ok := code(err)
		if !ok || e.Code != c.Code || pathString(e.Path) != c.Path || e.Line != c.Line {
			t.Errorf("parseLabeled %q: %v; want %s at %q line %d", c.Text, err, c.Code, c.Path, c.Line)
		}
	}
}

// valueLine is a line of an unlabelled text that holds a value: its
// path, its type name, and the value's text.
var valueLine = regexp.MustCompile(`^[ \t]*([0-9.]+)[ \t]*:[ \t]*([a-z0-9]+)[ \t]*=[ \t]*(.*?)[ \t]*$`)

func itemAt(r Row, p []int) any {
	var v any = r
	for _, i := range p {
		v = v.(Row)[i]
	}
	return v
}

// TestValue: ParseValue and FormatValue read and write each value as
// Parse and Format do, on every value line of the vectors, and refuse
// a bad one with the code Parse gives it.
func TestValue(t *testing.T) {
	v := load(t)
	texts := []string{}
	for _, c := range v.Valid {
		texts = append(texts, c.Text)
	}
	for _, c := range v.Lenient {
		texts = append(texts, c.Text)
	}
	n := 0
	for _, text := range texts {
		r, err := Parse(text)
		if err != nil {
			t.Fatalf("parse %q: %v", text, err)
		}
		for _, l := range strings.Split(text, "\n") {
			m := valueLine.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			p, err := parsePath(m[1])
			if err != nil {
				t.Fatal(err)
			}
			want := itemAt(r, p)
			got, err := ParseValue(m[2], m[3])
			if err != nil {
				t.Errorf("ParseValue(%q, %q): %v", m[2], m[3], err)
				continue
			}
			gb, _ := Encode(Row{got})
			wb, _ := Encode(Row{want})
			if !bytes.Equal(gb, wb) || TypeName(got) != m[2] {
				t.Errorf("ParseValue(%q, %q) = %#v, want %#v", m[2], m[3], got, want)
			}
			line, _ := Format(Row{want})
			_, canon, _ := strings.Cut(strings.TrimSuffix(line, "\n"), " = ")
			if s, err := FormatValue(want); err != nil || s != canon {
				t.Errorf("FormatValue(%#v) = %q, %v; want %q", want, s, err, canon)
			}
			n++
		}
	}
	bad := 0
	for _, c := range v.BadText {
		m := valueLine.FindStringSubmatch(strings.TrimSuffix(c.Text, "\n"))
		if m == nil || strings.Count(strings.TrimSuffix(c.Text, "\n"), "\n") > 0 || c.Path != "0" {
			continue
		}
		_, err := ParseValue(m[2], m[3])
		if e, ok := code(err); !ok || e.Code != c.Code {
			t.Errorf("ParseValue(%q, %q): %v; want %s", m[2], m[3], err, c.Code)
		}
		bad++
	}
	for _, c := range []struct {
		typ, text, code string
	}{
		{"none", "", "T004"}, {"row", "x", "T004"}, {"int", "", "T004"}, {"abc", "1", "T003"},
	} {
		if _, err := ParseValue(c.typ, c.text); err == nil {
			t.Errorf("ParseValue(%q, %q) accepted", c.typ, c.text)
		} else if e, _ := code(err); e.Code != c.code {
			t.Errorf("ParseValue(%q, %q): %v; want %s", c.typ, c.text, err, c.code)
		}
	}
	for _, c := range []struct {
		v    any
		code string
	}{
		{nil, "T004"}, {Row{}, "T004"}, {Decimal{1, 16}, "V002"}, {map[string]int{}, "V009"},
	} {
		if _, err := FormatValue(c.v); err == nil {
			t.Errorf("FormatValue(%#v) accepted", c.v)
		} else if e, _ := code(err); e.Code != c.code {
			t.Errorf("FormatValue(%#v): %v; want %s", c.v, err, c.code)
		}
	}
	if TypeName(int(3)) != "int" || TypeName([]any{}) != "row" || TypeName(map[string]int{}) != "" {
		t.Error("TypeName of the accepted variants")
	}
	if n < 100 || bad == 0 {
		t.Errorf("only %d value lines and %d bad ones checked", n, bad)
	}
	t.Logf("%d value lines, %d bad values", n, bad)
}
