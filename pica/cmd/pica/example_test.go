package main

import (
	"os"
	"strings"
	"testing"
	"text/template"

	"repani.com/pica"
	"repani.com/pica/desk"
	"repani.com/pica/press"
)

// TestOfficialExamples pins the committed example documents: the
// triptych (newspaper), the statement (report) and the harbour
// notice board (a 34-column sans page of terms, items and tables).
// The text output is golden; the PDF must render deterministically
// through the example's own presentation.
func TestOfficialExamples(t *testing.T) {
	cases := []struct {
		name, src, golden string
		render            func(*pica.Doc) ([]byte, error)
	}{
		{"triptych", "../../example/triptych.t", "../../example/triptych.txt", func(d *pica.Doc) ([]byte, error) { return press.PDF(d, false) }},
		{"statement", "../../example/statement.t", "../../example/statement.txt", func(d *pica.Doc) ([]byte, error) { return press.Report(d, false) }},
		{"harbour", "../../example/harbour.t", "../../example/harbour.txt", func(d *pica.Doc) ([]byte, error) { return press.PDF(d, false) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, err := os.ReadFile(c.src)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := pica.Parse(string(src))
			if err != nil {
				t.Fatal(err)
			}
			page, err := doc.Text()
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(c.golden)
			if err != nil {
				t.Fatal(err)
			}
			if page != string(want) {
				t.Errorf("text output differs from committed %s", c.golden)
			}
			a, err := c.render(doc)
			if err != nil {
				t.Fatal(err)
			}
			b, err := c.render(doc)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(a), "%PDF-1.3") {
				t.Fatal("not a PDF")
			}
			if string(a) != string(b) {
				t.Fatal("PDF bytes not deterministic")
			}
		})
	}
}

// renderExample runs the example through the same pipeline as
// `pica render -txtar | pica text` and returns the page text.
func renderExample(t *testing.T) string {
	t.Helper()
	tmplBytes, err := os.ReadFile("../../example/page.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	dataBytes, err := os.ReadFile("../../example/content.txtar")
	if err != nil {
		t.Fatal(err)
	}
	data, err := parseTxtar(dataBytes)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("page").
		Option("missingkey=zero").
		Funcs(desk.Funcs()).
		Parse(string(tmplBytes))
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatal(err)
	}
	doc, err := pica.Parse(buf.String())
	if err != nil {
		t.Fatal(err)
	}
	page, err := doc.Text()
	if err != nil {
		t.Fatal(err)
	}
	return page
}

// TestExampleGolden keeps the showcase honest: if the language,
// wrapping, or table rendering changes, this fails until
// example/expected.txt is regenerated (pica render -txtar
// example/page.tmpl example/content.txtar | pica text >
// example/expected.txt).
func TestExampleGolden(t *testing.T) {
	got := renderExample(t)
	want, err := os.ReadFile("../../example/expected.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("example output drifted from example/expected.txt:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	// Every displayed line respects the document's width; only
	// .link metadata (hidden by clients) is exempt.
	for _, ln := range strings.Split(got, "\n") {
		if strings.HasPrefix(ln, ".link ") {
			continue
		}
		if len([]rune(ln)) > 40 {
			t.Errorf("line exceeds width 40: %q", ln)
		}
	}
	// Layout commands never reach the page.
	for _, cmd := range []string{".width", ".cols", ".paper", ".pre", ".table", ".end"} {
		if strings.Contains(got, cmd+" ") || strings.Contains(got, cmd+"\n") {
			t.Errorf("consumed command %s leaked into output", cmd)
		}
	}
}

// TestExamplePDF renders the same source under the default presentation and checks
// determinism.
func TestExamplePDF(t *testing.T) {
	tmplBytes, err := os.ReadFile("../../example/page.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	dataBytes, err := os.ReadFile("../../example/content.txtar")
	if err != nil {
		t.Fatal(err)
	}
	data, err := parseTxtar(dataBytes)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("page").Option("missingkey=zero").Funcs(desk.Funcs()).Parse(string(tmplBytes))
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatal(err)
	}
	doc, err := pica.Parse(buf.String())
	if err != nil {
		t.Fatal(err)
	}
	a, err := press.PDF(doc, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := press.PDF(doc, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(a), "%PDF-1.3") {
		t.Fatal("not a PDF")
	}
	if string(a) != string(b) {
		t.Fatal("PDF bytes not deterministic")
	}
}
