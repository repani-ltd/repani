package main

import (
	"strings"
	"testing"
)

func TestHTMLPage(t *testing.T) {
	archive := strings.Join([]string{
		"-- data.fact --",
		`site.name: str = "Repani <Ltd>"`,
		"-- mark.svg --",
		`<svg><circle r="1"/></svg>`,
		"-- index.t --",
		"Hello & welcome",
		".rights (c) R",
		"",
		"Body.",
		"",
		".width 60",
		"-- page.tmpl --",
		"<!doctype html><title>{{.Title}} · {{.Facts.site.name}}</title>",
		"<header>{{.Raw.mark}} {{.Page}} w{{.Layout.Width}}</header>",
		"{{.Article}}",
	}, "\n") + "\n"
	out, err := htmlPage(archive, "index")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"<title>Hello &amp; welcome · Repani &lt;Ltd&gt;</title>", // escaped in context
		`<header><svg><circle r="1"/></svg> index w60</header>`,   // raw member trusted
		"<article>\n<h1>Hello &amp; welcome</h1>",                 // article trusted, its own escaping kept
		"<footer>(c) R</footer>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if _, err := htmlPage(archive, "missing"); err == nil || !strings.Contains(err.Error(), "missing.t") {
		t.Errorf("want missing-page error, got %v", err)
	}
	if _, err := htmlPage("-- index.t --\nT\n", "index"); err == nil || !strings.Contains(err.Error(), "page.tmpl") {
		t.Errorf("want missing-template error, got %v", err)
	}
}

func TestHTMLPageTemplatedDoc(t *testing.T) {
	archive := strings.Join([]string{
		"-- data.fact --",
		`company.name: str = "Repani"`,
		`items: list(ref(p)) = [p:a, p:b]`,
		`p:a.path: str = "x/a"`,
		`p:a.summary: str = "first"`,
		`p:b.path: str = "x/b"`,
		`p:b.summary: str = "second"`,
		"-- index.t.tmpl --",
		"{{.company.name}}",
		"",
		"Made by {{.company.name}}.",
		"",
		`{{table "6L *L" "path | what" .items "path" "summary"}}`,
		"-- page.tmpl --",
		"{{.Article}}",
	}, "\n") + "\n"
	out, err := htmlPage(archive, "index")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"<h1>Repani</h1>", "<p>Made by Repani.</p>", "<tr><td>x/a</td><td>first</td></tr>", "<tr><td>x/b</td><td>second</td></tr>"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	// A missing key is an error, never a blank.
	bad := strings.Replace(archive, "{{.company.name}}\n\nMade", "{{.company.nope}}\n\nMade", 1)
	if _, err := htmlPage(bad, "index"); err == nil {
		t.Error("want error for missing fact key")
	}
	// Both forms present is an error.
	both := archive + "-- index.t --\nT\n"
	if _, err := htmlPage(both, "index"); err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("want both-present error, got %v", err)
	}
}
