package press

import (
	"bytes"
	"regexp"
	"testing"

	"repani.com/pica"
)

const formDoc = `Cover Sheet
.rights Version 1

.fmt 10L 5L 5L *L
^Role | Entity
.fmt L L S S
Investor | Example Pension Fund
.fmt ^ L L L
RM# | N/A | 1953
.fmt 10L *P
Overview | A defined contribution scheme, compulsory for private sector employees.
.end

.fmt 10L 3L *P
Laws | REQ | The act that founded the scheme and its sponsor, with its amendments.
.fmt L L ^
Major | REQ
.end

.font sans
.width 60
.layout form
`

func TestFormRenders(t *testing.T) {
	doc, err := pica.Parse(formDoc)
	if err != nil {
		t.Fatal(err)
	}
	a, err := PDF(doc, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Report(doc, false)
	if err != nil {
		t.Fatal(err)
	}
	// Both commands render the form, and byte for byte the same.
	if !bytes.Equal(a, b) {
		t.Error("PDF and Report differ on a form")
	}
	again, _ := PDF(doc, false)
	if !bytes.Equal(a, again) {
		t.Error("two renders differ")
	}
	if n := len(regexp.MustCompile(`/Type\s*/Page\b`).FindAll(a, -1)); n != 1 {
		t.Errorf("%d pages, want 1", n)
	}
}

func TestFormTooNarrow(t *testing.T) {
	doc, err := pica.Parse("T\n\nBody.\n\n.width 200\n.paper a5\n.layout form\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PDF(doc, false); err == nil {
		t.Error("a 200-cell form on A5 rendered below the size floor")
	}
}
