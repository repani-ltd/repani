package fact

import (
	"bytes"
	"fmt"
	"testing"
)

// benchSrc is a data file of ~500 lines in the shape of a station's
// configuration: instances with str, int, float, bool and list(ref)
// values, the mix a real file has.
func benchSrc(b *testing.B) []byte {
	var buf bytes.Buffer
	buf.WriteString("stations: list(ref(station)) = [")
	for i := 0; i < 60; i++ {
		if i > 0 {
			buf.WriteString(", ")
		}
		fmt.Fprintf(&buf, "station:s%02d", i)
	}
	buf.WriteString("]\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&buf, "station:s%02d.name: str = \"Station %d\"\n", i, i)
		fmt.Fprintf(&buf, "station:s%02d.greek: str = \"Σταθμός %d\"\n", i, i)
		fmt.Fprintf(&buf, "station:s%02d.lat: float = %.3f\n", i, 35.0+float64(i)/10)
		fmt.Fprintf(&buf, "station:s%02d.lon: float = %.3f\n", i, 23.0+float64(i)/10)
		fmt.Fprintf(&buf, "station:s%02d.height: int = %d\n", i, i*7)
		fmt.Fprintf(&buf, "station:s%02d.active: bool = %v\n", i, i%3 == 0)
		fmt.Fprintf(&buf, "station:s%02d.tags: list(str) = [\"coast\", \"metar\", \"t%d\"]\n", i, i)
		fmt.Fprintf(&buf, "station:s%02d.kind: enum(civil|military|private) = civil\n", i)
	}
	return buf.Bytes()
}

func benchFacts(b *testing.B) []Fact {
	facts, errs := Load(benchSrc(b))
	if len(errs) > 0 {
		b.Fatal(errs[0])
	}
	return facts
}

func BenchmarkParse(b *testing.B) {
	src := benchSrc(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Parse(src)
	}
}

func BenchmarkValidate(b *testing.B) {
	facts := benchFacts(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Validate(facts)
	}
}

func BenchmarkBind(b *testing.B) {
	facts := benchFacts(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Bind(facts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonical(b *testing.B) {
	facts := benchFacts(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Canonical(facts)
	}
}
