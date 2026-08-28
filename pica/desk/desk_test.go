package desk

import (
	"strings"
	"testing"
)

// TestRender_Valid exercises the happy path: helpers resolve, the
// result is the generated source, newline terminated.
func TestRender_Valid(t *testing.T) {
	src, err := Render("bulletin", "Weather\n\nTemp {{round .t}} degrees.", map[string]any{"t": 21.6})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "Weather\n\nTemp 22 degrees.\n"
	if string(src) != want {
		t.Fatalf("Render = %q, want %q", src, want)
	}
}

// TestRender_MissingKey pins the missingkey=zero behavior over
// map data, which is what fact.Bind and JSON both produce: the
// zero value of an "any" element is Go's "<no value>" text, not a
// blank. Pinned as-is because it is pica render's shipped
// behavior; whether desk should refuse it instead is an open
// question in the ledger.
func TestRender_MissingKey(t *testing.T) {
	src, err := Render("b", "T\n\nvalue {{.absent}} here.", map[string]any{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(src), "value <no value> here.") {
		t.Fatalf("missing map key rendered as %q", src)
	}
}

// TestRender_InvalidDoc is the package's promise: a template that
// generates an invalid document is an error carrying the pica
// parse position, never a result.
func TestRender_InvalidDoc(t *testing.T) {
	src, err := Render("b", "T\n\n.bogus {{.x}}", map[string]any{"x": 1})
	if src != nil || err == nil {
		t.Fatalf("Render = %q, %v; want nil, parse error", src, err)
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("parse error carries no line number: %v", err)
	}
	if !strings.Contains(err.Error(), "b: rendered document:") {
		t.Fatalf("parse error not labelled as the template's output: %v", err)
	}
}

// TestRender_BadTemplate: a template that does not parse fails
// before execution, named for its origin.
func TestRender_BadTemplate(t *testing.T) {
	if _, err := Render("broken.tmpl", "T {{if}}", nil); err == nil ||
		!strings.Contains(err.Error(), "broken.tmpl") {
		t.Fatalf("template parse error = %v; want one naming broken.tmpl", err)
	}
}

// TestRender_ExecError: an execution failure (a helper rejecting
// its value) is reported, not swallowed into output.
func TestRender_ExecError(t *testing.T) {
	if _, err := Render("b", "T\n\n{{round .s}}", map[string]any{"s": "not a number"}); err == nil {
		t.Fatal("Render accepted a helper type error")
	}
}
