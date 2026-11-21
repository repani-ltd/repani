package row

import (
	"strings"
	"testing"
)

// TestSpec: the reference row spec prints has every section an author
// needs, and the codes it lists are the ones ROW.t tables.
func TestSpec(t *testing.T) {
	s := Spec()
	for _, h := range []string{"# Rows", "# Tags", "# Text", "# Errors", "# Go and JavaScript", "# CLI"} {
		if !strings.Contains(s, "\n"+h+"\n") {
			t.Errorf("spec has no %q section", h)
		}
	}
	for _, c := range []string{"R001", "R005", "T001", "T007", "V001", "V009", "S001", "S003"} {
		if !strings.Contains(s, c) {
			t.Errorf("spec lists no %s", c)
		}
	}
	if strings.Contains(s, "lz4s") || strings.Contains(s, "compressed") {
		t.Error("spec still speaks of compression")
	}
}
