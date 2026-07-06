package main

import (
	"os"
	"strings"
	"testing"
	"text/template"

	"github.com/pavlos/typeset"
)

// TestExampleGolden renders example/page.tmpl with
// example/content.txtar through the same pipeline as `pica render
// -txtar` and compares against the committed expected output. This
// keeps the showcase example honest: if helper behavior or table
// rendering changes, this fails until example/expected.txt is
// regenerated (pica render -txtar example/page.tmpl
// example/content.txtar > example/expected.txt).
func TestExampleGolden(t *testing.T) {
	tmplBytes, err := os.ReadFile("../../example/page.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	dataBytes, err := os.ReadFile("../../example/content.txtar")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../example/expected.txt")
	if err != nil {
		t.Fatal(err)
	}

	data, err := parseTxtar(dataBytes)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("page").
		Option("missingkey=zero").
		Funcs(funcMap()).
		Parse(string(tmplBytes))
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatal(err)
	}
	got, err := typeset.ExpandTables(buf.String())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "\n") {
		got += "\n"
	}

	if got != string(want) {
		t.Errorf("example output drifted from example/expected.txt:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	// The page must respect its own widths: nothing beyond 40 runes.
	if !typeset.FitsWidth(got, 40) {
		t.Errorf("example output exceeds 40 columns (max %d)", typeset.MaxLineWidth(got))
	}
}
