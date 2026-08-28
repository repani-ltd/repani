// Package desk writes copy: a Go template plus bound data becomes
// a validated pica source document. The desk turns wire data into
// copy; the press (repani.com/pica/press) prints it; pica is the
// language between them.
//
// The package owns two things. Funcs is the formatting vocabulary
// shared by every bulletin template; its admission test is the
// standing rule "value formatting only -- layout belongs to the
// writers, never to templates". Render bundles the promise a
// bulletin generator must keep: the template's output is parsed
// before it is returned, so a template bug surfaces here as a pica
// error with a line number, never as an invalid page on air.
//
// Data arrives already bound (fact.Bind, encoding/json, or plain
// Go values); loading and scheduling belong to the caller.
package desk

import (
	"fmt"
	"strings"
	"text/template"

	"repani.com/pica"
)

// Render executes the template src over data with the desk
// function set and returns the generated pica source, newline
// terminated. The output is parsed before it is returned: an
// invalid document is an error, never a result. name labels the
// template in error messages; a parse failure of the generated
// document is labelled "name: rendered document:" and its line
// numbers index the output, not the template. Missing keys follow pica render's
// missingkey=zero: over map data (fact.Bind, JSON) the zero of an
// "any" element renders as Go's "<no value>" text -- a template
// that must not ship a missing fact checks with "if" or belongs
// on the html assembler's stricter missingkey=error path.
func Render(name, src string, data any) ([]byte, error) {
	tmpl, err := template.New(name).
		Option("missingkey=zero").
		Funcs(Funcs()).
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
	if _, err := pica.Parse(doc); err != nil {
		// The position in this error is a line of the GENERATED
		// document, not of the template; the label says so.
		return nil, fmt.Errorf("%s: rendered document: %w", name, err)
	}
	return []byte(doc), nil
}
