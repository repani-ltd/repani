package raster

import (
	"os/exec"
	"strings"
	"testing"
)

// The raster package is what every renderer imports, so it stays
// free of the authoring side: the table language and the line
// breaker with its hyphenation patterns belong to the compilers
// that produce rasters (board's authoring layer, pica). This fails
// the tests the day an import drags them in.
func TestRasterStaysLight(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, banned := range []string{"repani.com/typeset/tbl", "repani.com/typeset/wrap", "repani.com/typeset/tab", "repani.com/board", "repani.com/pica"} {
			if dep == banned || strings.HasPrefix(dep, banned+"/") {
				t.Errorf("repani.com/typeset/raster depends on %s", dep)
			}
		}
	}
}
