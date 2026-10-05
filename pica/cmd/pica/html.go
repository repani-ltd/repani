// pica html: the HTML writer at the command line. Plain form renders
// one document to its <article> fragment. The -txtar form assembles
// a whole page from one archive, by member name:
//
//	NAME.t      the document selected by -page NAME; rendered by the
//	            writer and handed to the template as .Article
//	NAME.t.tmpl the same, but first expanded over data.fact by
//	            desk.Render, as pica render expands a template, but a
//	            missing key an error (desk.Refuse), so prose states
//	            each fact once and never a fact it does not have; a
//	            page has one of NAME.t and NAME.t.tmpl, never both
//	page.tmpl   the Go html/template executed for the page
//	data.fact   typed values under their keys (optional)
//	*.html      raw trusted fragments under their stem (.mark for
//	*.svg       mark.svg): the shell's own pieces, not documents
//
// The template also sees .Title, .Byline, .Rights and .Layout from
// the document, and .Page (the selected name). html/template
// escapes every fact value in context; the article and the raw
// members are the only trusted HTML, and pica rendered or was
// handed them. The archive is the page's single source: the same
// file a visitor can fetch reproduces the page.
package main

import (
	"errors"
	"fmt"
	"html/template"
	"strings"

	"repani.com/pica"
	"repani.com/pica/desk"
)

func htmlCmd(args []string) int {
	fs := newFlags("html")
	out := fs.String("o", "", "output file (default stdout)")
	archive := fs.Bool("txtar", false, "input is a txtar archive; assemble the page named by -page")
	page := fs.String("page", "", "with -txtar: the member NAME.t to render (required)")
	src, rc := loadSource("html", fs, args)
	if rc != 0 {
		return rc
	}
	var result []byte
	var err error
	if *archive {
		if *page == "" {
			fmt.Fprintln(stderr, "pica html: -txtar needs -page NAME")
			return 2
		}
		result, err = htmlPage(string(src), *page)
	} else {
		if *page != "" {
			fmt.Fprintln(stderr, "pica html: -page only applies with -txtar")
			return 2
		}
		var doc *pica.Doc
		doc, err = pica.Parse(string(src))
		if err == nil {
			result = []byte(doc.HTML())
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "pica html: %v\n", err)
		return 1
	}
	return writeOutput("html", *out, result)
}

// pageData is what page.tmpl executes against: the document's
// rendering and metadata, the selected page name, the facts, and
// the raw members.
type pageData struct {
	Page    string
	Title   string
	Byline  string
	Rights  string
	Layout  pica.Layout
	Article template.HTML
	Facts   map[string]any
	Raw     map[string]template.HTML
}

// htmlPage assembles the page named page from a txtar archive.
func htmlPage(archive, page string) ([]byte, error) {
	files := parseArchive(archive)
	if len(files) == 0 {
		return nil, errors.New("txtar: empty archive")
	}
	var docSrc, tmplSrc string
	haveDoc, haveTmpl, docIsTmpl := false, false, false
	var factSrc []byte
	raw := map[string]template.HTML{}
	for _, f := range files {
		switch {
		case f.name == page+".t" || f.name == page+".t.tmpl":
			if haveDoc {
				return nil, fmt.Errorf("txtar: both %s.t and %s.t.tmpl present", page, page)
			}
			docSrc, haveDoc = f.data, true
			docIsTmpl = f.name == page+".t.tmpl"
		case f.name == "page.tmpl":
			tmplSrc, haveTmpl = f.data, true
		case f.name == "data.fact":
			factSrc = []byte(f.data)
		case strings.HasSuffix(f.name, ".html") || strings.HasSuffix(f.name, ".svg"):
			stem := f.name[:strings.LastIndexByte(f.name, '.')]
			if _, dup := raw[stem]; dup {
				return nil, fmt.Errorf("txtar: duplicate raw member stem %q", stem)
			}
			raw[stem] = template.HTML(strings.TrimRight(f.data, "\n"))
		}
	}
	if !haveDoc {
		return nil, fmt.Errorf("txtar: no member %s.t or %s.t.tmpl", page, page)
	}
	if !haveTmpl {
		return nil, errors.New("txtar: no member page.tmpl")
	}
	facts := map[string]any{}
	var err error
	if factSrc != nil {
		facts, err = bindFacts(factSrc)
		if err != nil {
			return nil, err
		}
	}
	if docIsTmpl {
		// The page's copy is expanded as pica render expands any
		// template, but a missing key is an error: a page that states
		// a fact the data does not hold must not ship.
		src, err := desk.Render(page+".t.tmpl", docSrc, facts, desk.Refuse)
		if err != nil {
			return nil, err
		}
		docSrc = string(src)
	}
	doc, err := pica.Parse(docSrc)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("page.tmpl").Option("missingkey=zero").Funcs(desk.Funcs()).Parse(tmplSrc)
	if err != nil {
		return nil, fmt.Errorf("page.tmpl: %w", err)
	}
	var buf strings.Builder
	err = tmpl.Execute(&buf, pageData{
		Page: page, Title: doc.Title, Byline: doc.Byline(), Rights: doc.Rights, Layout: doc.Layout,
		Article: template.HTML(doc.HTML()), Facts: facts, Raw: raw,
	})
	if err != nil {
		return nil, fmt.Errorf("page.tmpl: %w", err)
	}
	s := buf.String()
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return []byte(s), nil
}
