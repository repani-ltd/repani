package fact

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

type crew struct {
	Name   string
	Rating int
	Role   string `fact:"role"`
}

type vessel struct {
	Name       string
	Draft      float64
	Registered time.Time
	Flags      []string
	Crew       []crew `fact:"manifest,kind=crew"`
	Escort     []crew `fact:"escort,kind=crew"`
	Berth      *int
	Tug        *string `fact:"tug"`
	Meta       meta
	skipped    string
	Ignored    string `fact:"-"`
}

type meta struct {
	SourceID string
	Checked  bool
}

func sample() vessel {
	berth := 12
	return vessel{
		Name:       "MV Ledger",
		Draft:      7.5,
		Registered: time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC),
		Flags:      []string{"CY", "handysize"},
		Crew:       []crew{{"Case", 4, "bosun"}, {"Souness", 7, "master"}},
		Escort:     []crew{{"Clemence", 6, "pilot"}},
		Berth:      &berth,
		Tug:        nil,
		Meta:       meta{SourceID: "lloyds", Checked: true},
		skipped:    "x",
		Ignored:    "y",
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	in := sample()
	data, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out vessel
	if err := Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}
	in.skipped, in.Ignored = "", "" // not marshalled
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip:\n in: %+v\nout: %+v\ndoc:\n%s", in, out, data)
	}
}

func TestMarshalCanonicalGolden(t *testing.T) {
	data, err := Marshal(sample())
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		`berth: int? = 12`,
		`crew:c01.name: str = "Case"`,
		`crew:c01.rating: int = 4`,
		`crew:c01.role: str = "bosun"`,
		`crew:c02.name: str = "Souness"`,
		`crew:c02.rating: int = 7`,
		`crew:c02.role: str = "master"`,
		`crew:c03.name: str = "Clemence"`,
		`crew:c03.rating: int = 6`,
		`crew:c03.role: str = "pilot"`,
		`draft: float = 7.5`,
		`escort: list(ref(crew)) = [crew:c03]`,
		`flags: list(str) = ["CY", "handysize"]`,
		`manifest: list(ref(crew)) = [crew:c01, crew:c02]`,
		`meta.checked: bool = true`,
		`meta.source_id: str = "lloyds"`,
		`name: str = "MV Ledger"`,
		`registered: datetime = 2026-07-20T09:30:00Z`,
		`tug: str? = none`,
	}, "\n") + "\n"
	if string(data) != want {
		t.Errorf("canonical output:\ngot:\n%s\nwant:\n%s", data, want)
	}
	// Marshal output must itself parse and validate cleanly.
	facts, errs := Parse(data)
	errs = append(errs, Validate(facts)...)
	if len(errs) > 0 {
		t.Errorf("marshal output does not validate: %v", errs)
	}
}

func TestMarshalDeterministic(t *testing.T) {
	a, _ := Marshal(sample())
	b, _ := Marshal(sample())
	if string(a) != string(b) {
		t.Error("equal values produced different documents")
	}
}

func TestMarshalStringEscaping(t *testing.T) {
	type doc struct{ Text string }
	for _, s := range []string{
		`plain`,
		`with "quotes" inside`,
		`back\slash`,
		"line\nbreak\tand tab",
		`unicode: mudbath—porridge £5`,
		`trailing backslash \`,
	} {
		data, err := Marshal(doc{Text: s})
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		var out doc
		if err := Unmarshal(data, &out); err != nil {
			t.Fatalf("%q: unmarshal: %v\n%s", s, err, data)
		}
		if out.Text != s {
			t.Errorf("string round trip: %q -> %q", s, out.Text)
		}
	}
}

func TestMarshalMarker(t *testing.T) {
	type pick struct {
		Squad   []crew `fact:"squad,kind=crew"`
		Captain Marker `fact:"captain,kind=crew"`
	}
	in := pick{Squad: []crew{{"Souness", 7, "master"}}, Captain: "crew:c01"}
	data, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "captain: ref(crew) = crew:c01\n") {
		t.Errorf("marker fact missing:\n%s", data)
	}
	// A dangling marker must fail Marshal's validation pass (E008).
	in.Captain = "crew:c99"
	if _, err := Marshal(in); err == nil {
		t.Error("dangling ref survived Marshal")
	}
}

func TestMarshalErrors(t *testing.T) {
	type noKind struct {
		Crew []crew `fact:"crew"`
	}
	if _, err := Marshal(noKind{Crew: []crew{{}}}); err == nil || !strings.Contains(err.Error(), "kind=") {
		t.Errorf("want kind= error, got %v", err)
	}
	type mapped struct{ M map[string]int }
	if _, err := Marshal(mapped{}); err == nil {
		t.Error("map field should be unsupported")
	}
	type collide struct {
		Crew []crew `fact:"crew,kind=crew"`
	}
	if _, err := Marshal(collide{Crew: []crew{{}}}); err == nil || !strings.Contains(err.Error(), "collides") {
		t.Errorf("want kind-collision error, got %v", err)
	}
	type dup struct {
		A string `fact:"x"`
		B string `fact:"x"`
	}
	if _, err := Marshal(dup{}); err == nil {
		t.Error("duplicate key from tags should fail validation")
	}
	if _, err := Marshal(42); err == nil {
		t.Error("non-struct should fail")
	}
}

func TestUnmarshalDatetimeForms(t *testing.T) {
	type doc struct {
		Day     time.Time
		Instant time.Time
	}
	src := "day: datetime = 2026-07-20\ninstant: datetime = 2026-09-01T09:30:00Z\n"
	var out doc
	if err := Unmarshal([]byte(src), &out); err != nil {
		t.Fatal(err)
	}
	if out.Day != time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC) {
		t.Errorf("day = %v", out.Day)
	}
	if out.Instant != time.Date(2026, 9, 1, 9, 30, 0, 0, time.UTC) {
		t.Errorf("instant = %v", out.Instant)
	}
}

func TestUnmarshalZeroAndNone(t *testing.T) {
	type doc struct {
		Present *int
		Absent  *int
		None    *int `fact:"gone"`
		Missing string
	}
	src := "gone: int? = none\npresent: int? = 5\n"
	var out doc
	if err := Unmarshal([]byte(src), &out); err != nil {
		t.Fatal(err)
	}
	if out.Present == nil || *out.Present != 5 {
		t.Errorf("present = %v", out.Present)
	}
	if out.None != nil || out.Absent != nil || out.Missing != "" {
		t.Errorf("absent/none handling: %+v", out)
	}
}
