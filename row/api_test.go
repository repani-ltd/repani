package row

import (
	"bytes"
	"math"
	"testing"
	"time"
)

// TestAccepted: Encode takes the variants of ROW.t 8 and decodes them
// back as the canonical Go values.
func TestAccepted(t *testing.T) {
	when := time.Date(2026, 11, 21, 11, 30, 0, 5e6, time.FixedZone("x", 2*3600))
	in := Row{int(7), uint8(8), int32(-9), uint64(10), float32(1.5), when, 90 * time.Minute, []any{"a", nil}}
	b, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	want := Row{int64(7), int64(8), int64(-9), int64(10), float64(1.5), when.UTC(), Duration(90 * 60000), Row{"a", nil}}
	again, _ := Encode(want)
	if !bytes.Equal(b, again) {
		t.Errorf("variants encode %x, canonical %x", b, again)
	}
	for i := range want {
		if typeName(out[i]) != typeName(want[i]) {
			t.Errorf("item %d decoded as %T, want %T", i, out[i], want[i])
		}
	}
	if !out[5].(time.Time).Equal(when) || out[5].(time.Time).Location() != time.UTC {
		t.Errorf("datetime %v, want %v in UTC", out[5], when)
	}
}

// TestRefused: a value outside its type fails Encode and Format alike,
// never rounded.
func TestRefused(t *testing.T) {
	cases := []struct {
		v    any
		code string
	}{
		{int64(1 << 53), "V001"},
		{uint64(1 << 60), "V001"},
		{Decimal{1, 16}, "V002"},
		{Decimal{1 << 53, 2}, "V002"},
		{math.NaN(), "V003"},
		{math.Copysign(0, -1), "V003"},
		{float32(math.Inf(1)), "V003"},
		{Day(maxDay + 1), "V004"},
		{time.Date(2026, 1, 1, 0, 0, 0, 1, time.UTC), "V005"},
		{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), "V005"},
		{TimeOfDay(maxTime + 1), "V006"},
		{Duration(1 << 53), "V007"},
		{time.Duration(1500), "V007"},
		{"\xff", "V008"},
		{map[string]int{}, "V009"},
		{struct{}{}, "V009"},
		{Row{Row{Row{Row{}}}}, "R005"},
	}
	for _, c := range cases {
		for name, f := range map[string]func(Row) error{
			"encode": func(r Row) error { _, err := Encode(r); return err },
			"format": func(r Row) error { _, err := Format(r); return err },
		} {
			err := f(Row{"x", c.v})
			e, ok := code(err)
			if !ok || e.Code != c.code {
				t.Errorf("%s %#v: %v, want %s", name, c.v, err, c.code)
				continue
			}
			if c.code != "R005" && pathString(e.Path) != "1" {
				t.Errorf("%s %#v: path %v, want 1", name, c.v, e.Path)
			}
		}
	}
}

// TestReader reads an iou-shaped expense: fixed positions, an optional
// one, and a sharers group whose share is a decimal or an int.
func TestReader(t *testing.T) {
	r := Row{"x", Day(20778), "EUR", Decimal{9000, 2}, nil, "pavlos",
		Row{Row{"ben", Decimal{4500, 2}}, Row{"pavlos", int64(1)}}, "taxi"}
	rd := Read(r)
	kind, day, cur, amt := rd.Str(), rd.Date(), rd.Str(), rd.Dec()
	place := "-"
	if !rd.None() {
		place = rd.Str()
	}
	payer := rd.Str()
	var fixed []Decimal
	var weights []int64
	sh := rd.Row()
	for sh.More() {
		s := sh.Row()
		s.Str()
		if s.Peek() == "dec" {
			fixed = append(fixed, s.Dec())
		} else {
			weights = append(weights, s.Int())
		}
		s.End()
	}
	sh.End()
	desc := rd.Str()
	if err := rd.End(); err != nil {
		t.Fatal(err)
	}
	if kind != "x" || day != 20778 || cur != "EUR" || amt != (Decimal{9000, 2}) || place != "-" ||
		payer != "pavlos" || len(fixed) != 1 || len(weights) != 1 || desc != "taxi" {
		t.Errorf("read %q %v %q %v %q %q %v %v %q", kind, day, cur, amt, place, payer, fixed, weights, desc)
	}
}

// TestReaderErrors: S001 to S003 with the item's path, and the first
// error sticks across nested readers.
func TestReaderErrors(t *testing.T) {
	check := func(name string, err error, want, path string) {
		t.Helper()
		e, ok := code(err)
		if !ok || e.Code != want || pathString(e.Path) != path {
			t.Errorf("%s: %v, want %s at %s", name, err, want, path)
		}
	}
	rd := Read(Row{"a", int64(1)})
	rd.Str()
	rd.Str() // an int: S001, and it sticks
	if rd.Int() != 0 || rd.More() || rd.Peek() != "" {
		t.Error("reads after an error did not return zero values")
	}
	check("wrong type", rd.End(), "S001", "1")

	rd = Read(Row{"a"})
	rd.Str()
	rd.Int()
	check("short", rd.End(), "S002", "1")

	rd = Read(Row{"a", "b"})
	rd.Str()
	check("long", rd.End(), "S003", "1")

	rd = Read(Row{Row{"a", int64(2)}})
	sub := rd.Row()
	sub.Str()
	sub.Str()
	check("nested sticks to the outer End", rd.End(), "S001", "0.1")
}
