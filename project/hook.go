// Agent-harness integration: regenerate a package's stored projection
// after a source edit and surface the projection diff — the impact report
// of SPEC §11.1 — back to the editing agent.

package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"flat/fact"
)

// File projects target (a package directory or import path, as Lines) and
// renders the stored file form: the generated header plus the canonical
// fact set (SPEC §11.1).
func File(target string) ([]byte, error) {
	lines, err := Lines(target)
	if err != nil {
		return nil, err
	}
	facts, errs := fact.Parse([]byte(strings.Join(lines, "\n") + "\n"))
	errs = append(errs, fact.Validate(facts)...)
	if len(errs) > 0 { // a generator bug, not a user error
		return nil, fmt.Errorf("generator produced invalid FACT: %s", errs[0].Error())
	}
	return append([]byte(Header+"\n"), fact.Canonical(facts)...), nil
}

// Refresh regenerates dir's stored pkg.fact, rewriting it only when the
// projection changed, and returns the canonical line diff. Empty diff with
// nil error means the edit was declaration-neutral (the churn invariant,
// SPEC §11.2).
func Refresh(dir string) (removed, added []string, err error) {
	target := filepath.Join(dir, "pkg.fact")
	existing, err := os.ReadFile(target)
	if err != nil {
		return nil, nil, err
	}
	out, err := File(dir)
	if err != nil {
		return nil, nil, err
	}
	if bytes.Equal(existing, out) {
		return nil, nil, nil
	}
	os.Remove(target) // stored read-only per SPEC §11.1
	if err := os.WriteFile(target, out, 0o444); err != nil {
		return nil, nil, err
	}
	removed, added = diffLines(existing, out)
	return removed, added, nil
}

// diffLines set-diffs two canonical projections line-wise. Canonical files
// are bytewise-sorted, so membership is the whole story: there are no
// move or reorder cases to report.
func diffLines(before, after []byte) (removed, added []string) {
	beforeLines := splitLines(before)
	afterLines := splitLines(after)
	beforeSet := map[string]bool{}
	for _, l := range beforeLines {
		beforeSet[l] = true
	}
	afterSet := map[string]bool{}
	for _, l := range afterLines {
		afterSet[l] = true
	}
	for _, l := range beforeLines {
		if !afterSet[l] {
			removed = append(removed, l)
		}
	}
	for _, l := range afterLines {
		if !beforeSet[l] {
			added = append(added, l)
		}
	}
	return removed, added
}

func splitLines(b []byte) []string {
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// hookInput is the subset of Claude Code's PostToolUse payload the fact
// hook consumes.
type hookInput struct {
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

// maxHookDiffLines caps the impact report surfaced to the agent; the full
// diff is always available as the pkg.fact working-tree diff.
const maxHookDiffLines = 80

// Hook implements a Claude Code PostToolUse hook over Refresh: when the
// edited file is a non-test .go file in a package carrying a stored
// pkg.fact, regenerate it and return the projection diff as context for
// the agent. An empty return means nothing to report: not a projected
// package, or a declaration-neutral edit (the churn invariant, SPEC §11.2).
func Hook(payload []byte) (string, error) {
	var in hookInput
	if err := json.Unmarshal(payload, &in); err != nil {
		return "", err
	}
	fp := in.ToolInput.FilePath
	if !strings.HasSuffix(fp, ".go") || strings.HasSuffix(fp, "_test.go") {
		return "", nil
	}
	dir := filepath.Dir(fp)
	if _, err := os.Stat(filepath.Join(dir, "pkg.fact")); err != nil {
		return "", nil // projection is opt-in per package
	}
	removed, added, err := Refresh(dir)
	if err != nil {
		return "", err
	}
	if len(removed)+len(added) == 0 {
		return "", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "pkg.fact regenerated for %s — projection diff (impact report, SPEC §11.1):\n", dir)
	n := 0
	for _, l := range removed {
		if n++; n > maxHookDiffLines {
			break
		}
		fmt.Fprintf(&b, "- %s\n", l)
	}
	for _, l := range added {
		if n++; n > maxHookDiffLines {
			break
		}
		fmt.Fprintf(&b, "+ %s\n", l)
	}
	if total := len(removed) + len(added); total > maxHookDiffLines {
		fmt.Fprintf(&b, "… (%d more lines; see the pkg.fact diff)\n", total-maxHookDiffLines)
	}
	return b.String(), nil
}
