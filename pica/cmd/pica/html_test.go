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
