package wrap

import (
	"os/exec"
	"strings"
	"testing"
)

// The breaker is the light half: a program that wraps without
// hyphenating imports it alone, and links neither the patterns nor
// the code that compiles them. This fails the tests the day an
// import drags typeset/wrap/hyphen in.
func TestWrapStaysLight(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "repani.com/") && dep != "repani.com/typeset/wrap" {
			t.Errorf("repani.com/typeset/wrap depends on %s", dep)
		}
	}
}
