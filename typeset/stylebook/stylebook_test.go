package stylebook

import (
	"errors"
	"strings"
	"testing"
	"text/template"
)

// A check standing in for a language's validator: the document
// must open with "T".
func checkT(doc string) error {
	if !strings.HasPrefix(doc, "T\n") {
		return errors.New("no title (line 1)")
	}
	return nil
}

// TestRender_Valid exercises the happy path: helpers resolve, the
// result is the generated source, newline terminated.
func TestRender_Valid(t *testing.T) {
	src, err := Render("bulletin", "T\n\nTemp {{round .t}} degrees.", map[string]any{"t": 21.6}, nil, checkT)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "T\n\nTemp 22 degrees.\n"
	if string(src) != want {
		t.Fatalf("Render = %q, want %q", src, want)
	}
}

// TestRender_MissingKey pins the missingkey=zero behavior over
// map data, which is what fact.Bind and JSON both produce: the
// zero value of an "any" element is Go's "<no value>" text, not a
// blank.
func TestRender_MissingKey(t *testing.T) {
	src, err := Render("b", "T\n\nvalue {{.absent}} here.", map[string]any{}, nil, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(src), "value <no value> here.") {
		t.Fatalf("missing map key rendered as %q", src)
	}
}

// TestRender_CheckFails is the package's promise: output the check
// rejects is an error labelled as the template's output, never a
// result.
func TestRender_CheckFails(t *testing.T) {
	src, err := Render("b", "no title {{.x}}", map[string]any{"x": 1}, nil, checkT)
	if src != nil || err == nil {
		t.Fatalf("Render = %q, %v; want nil, check error", src, err)
	}
	if !strings.Contains(err.Error(), "b: rendered document: no title (line 1)") {
		t.Fatalf("check error not labelled as the template's output: %v", err)
	}
}

// TestRender_Extra: a language's own helpers join the vocabulary,
// and may shadow it.
func TestRender_Extra(t *testing.T) {
	extra := template.FuncMap{"shout": strings.ToUpper, "round": func(any) (string, error) { return "R", nil }}
	src, err := Render("b", "T\n\n{{shout .s}} {{round 1}}", map[string]any{"s": "hi"}, extra, checkT)
	if err != nil {
		t.Fatal(err)
	}
	if string(src) != "T\n\nHI R\n" {
		t.Fatalf("Render = %q", src)
	}
}

// TestRender_BadTemplate: a template that does not parse fails
// before execution, named for its origin.
func TestRender_BadTemplate(t *testing.T) {
	if _, err := Render("broken.tmpl", "T {{if}}", nil, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "broken.tmpl") {
		t.Fatalf("template parse error = %v; want one naming broken.tmpl", err)
	}
}

// TestRender_ExecError: an execution failure (a helper rejecting
// its value) is reported, not swallowed into output.
func TestRender_ExecError(t *testing.T) {
	if _, err := Render("b", "T\n\n{{round .s}}", map[string]any{"s": "not a number"}, nil, nil); err == nil {
		t.Fatal("Render accepted a helper type error")
	}
}
