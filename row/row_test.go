package row

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
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
