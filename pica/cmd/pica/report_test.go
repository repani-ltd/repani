package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pavlos/typeset"
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

	doc, err := typeset.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, font := range []string{"mono", "sans"} {
		doc.Layout.Font = font
		b, err := report(doc)
		if err != nil {
			t.Fatalf("report(%s): %v", font, err)
		}
		if !bytes.HasPrefix(b, []byte("%PDF")) || len(b) < 1000 {
			t.Fatalf("report(%s): implausible PDF (%d bytes)", font, len(b))
		}
	}
}
