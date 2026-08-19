package main

import (
	"bytes"
	"strings"
	"testing"

	"repani.com/pica"
)

func TestReport_Smoke(t *testing.T) {
	src := strings.Join([]string{
		"CLIENT STATEMENT",
		".by Treasury Reporting",
		"",
		"Positions held at close of business.",
		"",
		".table *L 10N",
		"Client | Amount",
		".. | eur thousands",
		"Alpha | 1,234.56",
		"Beta | (2.00)",
		"= Total | 1,232.56",
		".end",
		"",
		".width 78",
	}, "\n") + "\n"

	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, font := range []string{"mono", "sans"} {
		doc.Layout.Font = font
		b, err := report(doc, false)
		if err != nil {
			t.Fatalf("report(%s): %v", font, err)
		}
		if !bytes.HasPrefix(b, []byte("%PDF")) || len(b) < 1000 {
			t.Fatalf("report(%s): implausible PDF (%d bytes)", font, len(b))
		}
	}
}

// The mark is a render option: off, the output carries no form
// object; on, the first page draws the embedded mark and the title
// block is measured against the narrower header.
func TestReport_Mark(t *testing.T) {
	src := "A TITLE LONG ENOUGH TO NEED SHRINKING WHEN THE MARK TAKES ITS CORNER OF THE PAGE\n\nBody.\n\n.width 60\n"
	doc, err := pica.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	off, err := report(doc, false)
	if err != nil {
		t.Fatal(err)
	}
	on, err := report(doc, true)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(off, []byte("/"+markName)) {
		t.Error("mark present without -mark")
	}
	if !bytes.Contains(on, []byte("/"+markName+" ")) || !bytes.Contains(on, []byte("/Subtype /Form")) {
		t.Error("mark form missing with -mark")
	}
	if len(on) <= len(off) {
		t.Errorf("mark added nothing: %d vs %d bytes", len(on), len(off))
	}
	if d := len(on) - len(off); d > 1200 {
		t.Errorf("mark costs %d bytes; expected under 1200", d)
	}
}
