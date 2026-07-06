package typeset

import (
	"strings"
	"testing"
)

func TestExpandTables_Simple(t *testing.T) {
	input := `Title

.table 5L *L 4R
Day | Forecast | Temp
Mon | Sunny | 25
Tue | Cloudy | 22
.end

After table.`

	out, err := ExpandTables(input)
	if err != nil {
		t.Fatalf("ExpandTables: %v", err)
	}

	// Markers should be consumed (not in output).
	if strings.Contains(out, ".table") || strings.Contains(out, ".end") {
		t.Errorf("markers should be stripped:\n%s", out)
	}
	// The table should be rendered (header followed by separator).
	if !strings.Contains(out, "Day  ") || !strings.Contains(out, "---") {
		t.Errorf("expected rendered table content:\n%s", out)
	}
	// Outer text preserved.
	if !strings.Contains(out, "Title") || !strings.Contains(out, "After table.") {
		t.Errorf("outer text lost:\n%s", out)
	}
}

func TestExpandTables_Bare(t *testing.T) {
	input := `.table 3L 4R
A | B
1 | 2
.end`

	out, err := ExpandTables(input)
	if err != nil {
		t.Fatalf("ExpandTables: %v", err)
	}
	if strings.Contains(out, ".table") || strings.Contains(out, ".end") {
		t.Errorf("markers should be stripped:\n%s", out)
	}
	if !strings.Contains(out, "A") || !strings.Contains(out, "B") {
		t.Errorf("content missing:\n%s", out)
	}
}

func TestExpandTables_NoTable(t *testing.T) {
	input := "Just plain text\nwith no tables."
	out, err := ExpandTables(input)
	if err != nil {
		t.Fatalf("ExpandTables: %v", err)
	}
	if out != input {
		t.Errorf("got %q, want %q", out, input)
	}
}

func TestExpandTables_MultipleTables(t *testing.T) {
	input := `.table 3L 3R
A | B
1 | 2
.end

middle

.table 3L 3R
X | Y
9 | 8
.end`

	out, err := ExpandTables(input)
	if err != nil {
		t.Fatalf("ExpandTables: %v", err)
	}
	if strings.Contains(out, ".table") || strings.Contains(out, ".end") {
		t.Errorf("markers should be stripped:\n%s", out)
	}
	if !strings.Contains(out, "middle") {
		t.Errorf("middle text missing:\n%s", out)
	}
}

func TestExpandTables_BadSpec(t *testing.T) {
	input := `.table notavalidspec
Header
data
.end`

	// Bad spec must surface as an error.
	if _, err := ExpandTables(input); err == nil {
		t.Errorf("expected error for bad spec, got nil")
	}
}

func TestExpandTables_FullPipeline(t *testing.T) {
	// Simulate a template-rendered page going through stages 2 and 3.
	input := `Weather Limassol

.table 3L *L 4R
Day | Forecast | Temp
Mon | Sunny day with light wind | 25
Tue | Cloudy with chance of rain | 22
.end

#weather`

	stage2, err := ExpandTables(input)
	if err != nil {
		t.Fatalf("ExpandTables: %v", err)
	}

	// Title preserved.
	if !strings.HasPrefix(stage2, "Weather Limassol") {
		t.Errorf("title lost:\n%s", stage2)
	}
	// Table rows present after expansion.
	if !strings.Contains(stage2, "Mon") || !strings.Contains(stage2, "Tue") {
		t.Errorf("table rows missing:\n%s", stage2)
	}
}

func TestExpandTables_ExplicitWidth(t *testing.T) {
	input := ".table 20 3L *L\nA | B\n1 | two\n.end"
	out, err := ExpandTables(input)
	if err != nil {
		t.Fatalf("ExpandTables: %v", err)
	}
	for _, ln := range strings.Split(out, "\n") {
		if len([]rune(ln)) != 20 {
			t.Errorf("line not 20 chars: %q (len=%d)", ln, len([]rune(ln)))
		}
	}
}
