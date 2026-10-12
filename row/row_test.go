package row

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// vectors are ROW v1's frozen test vectors, written by an independent
// encoder and shared with row.js (js_test.go). A changed vector is a
// changed format.
type vectors struct {
	Valid []struct {
		Text string `json:"text"`
		Hex  string `json:"hex"`
	} `json:"valid"`
	BadBinary []struct {
		Hex    string `json:"hex"`
		Code   string `json:"code"`
		Offset int    `json:"offset"`
	} `json:"badBinary"`
	BadText []struct {
		Text string `json:"text"`
		Code string `json:"code"`
	} `json:"badText"`
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

// TestVectors: every valid row decodes, re-encodes, formats and parses
// to exactly its bytes and text; every bad one fails with its code
// (and, in bytes, its offset).
func TestVectors(t *testing.T) {
	v := load(t)
	for _, c := range v.Valid {
		b, _ := hex.DecodeString(c.Hex)
		items, err := Decode(b)
		if err != nil {
			t.Errorf("decode %s: %v", c.Hex, err)
			continue
		}
		if got, err := Encode(items); err != nil || !bytes.Equal(got, b) {
			t.Errorf("encode(decode(%s)) = %x, %v", c.Hex, got, err)
		}
		if got, err := Format(items); err != nil || got != c.Text {
			t.Errorf("format(decode(%s)) = %q, %v; want %q", c.Hex, got, err, c.Text)
		}
		parsed, err := Parse(c.Text)
		if err != nil {
			t.Errorf("parse %q: %v", c.Text, err)
			continue
		}
		if got, err := Encode(parsed); err != nil || !bytes.Equal(got, b) {
			t.Errorf("encode(parse(%q)) = %x, %v; want %s", c.Text, got, err, c.Hex)
		}
	}
	for _, c := range v.BadBinary {
		b, _ := hex.DecodeString(c.Hex)
		_, err := Decode(b)
		var re *Error
		if !errors.As(err, &re) || re.Code != c.Code || re.Offset != c.Offset {
			t.Errorf("decode %s: %v; want %s at %d", c.Hex, err, c.Code, c.Offset)
		}
	}
	for _, c := range v.BadText {
		_, err := Parse(c.Text)
		var re *Error
		if !errors.As(err, &re) || re.Code != c.Code {
			t.Errorf("parse %q: %v; want %s", c.Text, err, c.Code)
		}
	}
	t.Logf("%d valid, %d bad binary, %d bad text", len(v.Valid), len(v.BadBinary), len(v.BadText))
}

// TestEncodeRules: Go values outside a type's rules are refused with
// the code Decode gives their bytes; a value a type cannot hold is
// refused rather than rounded.
func TestEncodeRules(t *testing.T) {
	for _, c := range []struct {
		item any
		code string
	}{
		{int64(maxInt + 1), "R003"},
		{-maxInt - 1, "R003"},
		{Decimal{1, 16}, "R004"},
		{Decimal{1, -1}, "R004"},
		{Decimal{maxInt + 1, 0}, "R003"},
		{math.NaN(), "R005"},
		{math.Inf(-1), "R005"},
		{math.Copysign(0, -1), "R005"},
		{Day(maxDay + 1), "R003"},
		{TimeOfDay(maxTime + 1), "R003"},
		{TimeOfDay(-1), "R003"},
		{time.Date(2026, 1, 1, 0, 0, 0, 1, time.UTC), "R003"},
		{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), "R003"},
		{time.Microsecond, "R003"},
		{Duration(maxInt + 1), "R003"},
		{"\xff", "R006"},
		{[]any{[]any{}}, "R008"},
	} {
		_, err := Encode([]any{"pad", c.item})
		var re *Error
		if !errors.As(err, &re) || re.Code != c.code || re.Offset != 4 {
			t.Errorf("encode %#v: %v; want %s at 4", c.item, err, c.code)
		}
	}
	if _, err := Encode([]any{struct{}{}}); err == nil {
		t.Error("encoded a struct")
	}
}

// TestGoValues pins what Decode returns for each type, and that a
// datetime in any location is the same instant.
func TestGoValues(t *testing.T) {
	when := time.Date(2026, 10, 8, 11, 30, 0, 0, time.FixedZone("CEST", 2*3600))
	b, err := Encode([]any{"s", nil, true, 7, Decimal{9000, 2}, 1.5, Day(20734), when, TimeOfDay(1), 90 * time.Minute, []byte{1}, []any{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"s", nil, true, int64(7), Decimal{9000, 2}, 1.5, Day(20734), time.Date(2026, 10, 8, 9, 30, 0, 0, time.UTC), TimeOfDay(1), Duration(5400000), []byte{1}, []any{"a"}}
	for i := range want {
		if g, w := strings.TrimSpace(fmtAny(got[i])), fmtAny(want[i]); g != w {
			t.Errorf("item %d = %s, want %s", i, g, w)
		}
	}
}

func fmtAny(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestSpec: the "tools explain themselves" rule -- Spec is the package
// comment, whole, with every section and no source furniture.
func TestSpec(t *testing.T) {
	s := Spec()
	for _, want := range []string{"# Items", "# Text notation", "# Errors", "# Go and JavaScript", "# CLI", "R008", "R009", "T002", "ROW.t"} {
		if !strings.Contains(s, want) {
			t.Errorf("Spec() missing %q", want)
		}
	}
	if strings.Contains(s, "package row") || strings.Contains(s, "/*") {
		t.Error("Spec() leaks source furniture")
	}
}

// FuzzDecode: bytes that decode are the one encoding of their row.
func FuzzDecode(f *testing.F) {
	for _, s := range []string{"", "0378", "f3f4f5", "f7029c01", "ff0203626f6ef602", "f9fcc302", "fe8c02", "f2f201041f6100e0", "f2ac02051f6100ff1b"} {
		b, _ := hex.DecodeString(s)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		items, err := Decode(b)
		if err != nil {
			var re *Error
			if !errors.As(err, &re) || re.Offset < 0 || re.Offset >= len(b) {
				t.Fatalf("decode %x: bad error %v", b, err)
			}
			return
		}
		got, err := Encode(items)
		if err != nil || !bytes.Equal(got, b) {
			t.Fatalf("decode %x then encode = %x, %v", b, got, err)
		}
		text, err := Format(items)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Parse(text)
		if err != nil {
			t.Fatalf("parse(format(%x)) %q: %v", b, text, err)
		}
		if again, _ := Encode(back); !bytes.Equal(again, b) {
			t.Fatalf("%x -> %q -> %x", b, text, again)
		}
	})
}

// FuzzParse: text that parses is the one spelling of its row.
func FuzzParse(f *testing.F) {
	for _, s := range []string{`"x" 2026-10-08 "EUR" 90.00 none`, `[1, "a"]`, `9.05e1 -0.5s 0x00ff`, `1h30m15.25s`, `2026-10-08T09:30:00.000Z 09:30:00.000`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		items, err := Parse(s)
		if err != nil {
			return
		}
		if got, err := Format(items); err != nil || got != s {
			t.Fatalf("format(parse(%q)) = %q, %v", s, got, err)
		}
	})
}
