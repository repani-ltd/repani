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

// TestLabelAndValue: FormatLabeled checks its labels, and FormatValue
// checks its value before refusing a row, as row.js does.
func TestLabelAndValue(t *testing.T) {
	if _, err := FormatLabeled([]Labeled{{"\xff", Row{}}}); err == nil || err.(*Error).Code != "V008" {
		t.Errorf("a label that is no UTF-8: %v, want V008", err)
	}
	if _, err := FormatValue(Row{math.NaN()}); err == nil || err.(*Error).Code != "V003" {
		t.Errorf("FormatValue(Row{NaN}): %v, want V003", err)
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
