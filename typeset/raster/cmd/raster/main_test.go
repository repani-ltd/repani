package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"repani.com/typeset/raster"
)

// raster spec teaches the format and the tool: the reference's
// sections and the CLI usage must both be in it.
func TestSpecSections(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"spec"}, &out, &out); code != 0 {
		t.Fatalf("spec exit %d", code)
	}
	for _, want := range []string{"# Rows", "# Cells", "# Ink", "# Authoring", "# The raster CLI", "raster check FILE"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("spec output missing %q", want)
		}
	}
}

// Every example compiles, and bytes emits exactly its records, which
// read back to the same raster.
func TestExamples(t *testing.T) {
	files, _ := filepath.Glob("../../examples/*.rt")
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
		if code := run([]string{"bytes", f}, &out, &errb); code != 0 {
			t.Errorf("%s: bytes exit %d", f, code)
			continue
		}
		r, err := raster.Read(out.Bytes())
		if err != nil || !bytes.Equal(r.Bytes(), out.Bytes()) {
			t.Errorf("%s: bytes do not read back: %v", f, err)
		}
	}
}

func TestCheckReportsLine(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bad.rt")
	os.WriteFile(f, []byte(".at 0\nok\n.bogus\n"), 0o644)
	var out, errb bytes.Buffer
	if code := run([]string{"check", f}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "line 3") {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
}
