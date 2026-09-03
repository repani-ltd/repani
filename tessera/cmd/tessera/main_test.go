package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"repani.com/tessera"
)

// tessera spec teaches the format and the tool: the reference's
// sections and the CLI usage must both be in it.
func TestSpecSections(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"spec"}, &out, &out); code != 0 {
		t.Fatalf("spec exit %d", code)
	}
	for _, want := range []string{"# The page", "# The tile", "# Cells", "# Ink", "# Authoring", "Frozen vector", "# The tessera CLI", "tessera check FILE"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("spec output missing %q", want)
		}
	}
}

// Every example compiles, and page emits exactly the raster.
func TestExamples(t *testing.T) {
	files, _ := filepath.Glob("../../examples/*.tessera")
	if len(files) == 0 {
		t.Fatal("no examples found")
	}
	for _, f := range files {
		var out, errb bytes.Buffer
		if code := run([]string{"check", f}, &out, &errb); code != 0 {
			t.Errorf("%s: %s", f, errb.String())
			continue
		}
		out.Reset()
		if code := run([]string{"page", f}, &out, &errb); code != 0 || out.Len() != tessera.PageLen {
			t.Errorf("%s: page exit %d, %d bytes", f, code, out.Len())
		}
	}
}

func TestCheckReportsLine(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bad.tessera")
	os.WriteFile(f, []byte(".panel 0\nok\n.bogus\n"), 0o644)
	var out, errb bytes.Buffer
	if code := run([]string{"check", f}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "line 3") {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
}
