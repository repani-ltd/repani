// Package stylebook is the house style for templates that write
// copy from data: how a number, a date, a duration or a value cut
// to a measure is written, and the promise that a rendered page is
// checked before it is returned. It serves every Repani language
// that generates pages from templates -- pica documents, tessera
// panels -- and knows none of them: a function here formats a value
// into runes, never into a language's syntax (a helper that emits
// syntax belongs with its language; pica's "table" is the
// precedent). The admission rule is the standing one: value
// formatting only, layout belongs to the writers.
//
// Funcs is the vocabulary. Render executes a template over bound
// data with that vocabulary, plus whatever a language adds, and
// runs the language's check over the output, so a template bug
// surfaces as an error with a line number, never as an invalid
// page on air. Data arrives already bound (fact.Bind,
// encoding/json, plain Go values); loading and scheduling belong to
// the caller.
package stylebook

import (
	"fmt"
	"maps"
	"strings"
	"text/template"
)

// Render executes the template src over data with Funcs and extra
// (a language's own helpers; nil for none) and returns the
// generated source, newline terminated, after check has accepted
// it. check is the language's validator -- pica.Parse, a tessera
// compile -- and a failure is returned labelled "name: rendered
// document:" since its positions index the output, not the
// template. Missing keys render as Go's zero ("<no value>" over
// map data); a template that must not ship a missing fact tests
// with "if", or the caller drives text/template with
// missingkey=error itself.
func Render(name, src string, data any, extra template.FuncMap, check func(string) error) ([]byte, error) {
	funcs := Funcs()
	maps.Copy(funcs, extra)
	tmpl, err := template.New(name).
		Option("missingkey=zero").
		Funcs(funcs).
		Parse(src)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	doc := buf.String()
	if !strings.HasSuffix(doc, "\n") {
		doc += "\n"
	}
	if check != nil {
		if err := check(doc); err != nil {
			return nil, fmt.Errorf("%s: rendered document: %w", name, err)
		}
	}
	return []byte(doc), nil
}
