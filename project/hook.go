// Agent-harness integration: regenerate a package's stored projection
// after a source edit and surface the projection diff — the impact report
// of SPEC §11.1 — back to the editing agent. The hook also runs goimports
// on the edited file and, when the package no longer compiles, surfaces
// the compiler diagnostics instead: the edit→build→read-errors loop
// collapses into the edit itself.

package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/imports"

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

// maxHookDiagnostics caps the compile errors surfaced to the agent.
const maxHookDiagnostics = 20

// Hook implements a Claude Code PostToolUse hook: when the edited file is
// a .go file in a package carrying a stored pkg.fact, it runs goimports
// on the file, regenerates the projection, and returns the projection
// diff — or, if the package no longer compiles, the compiler
// diagnostics — as context for the agent. Test files are formatted but
// sit outside the projection (generator scope). An empty return means
// nothing to report: not a projected package, or a declaration-neutral
// edit that goimports left untouched (the churn invariant, SPEC §11.2).
func Hook(payload []byte) (string, error) {
	var in hookInput
	if err := json.Unmarshal(payload, &in); err != nil {
		return "", err
	}
	fp := in.ToolInput.FilePath
	if !strings.HasSuffix(fp, ".go") {
		return "", nil
	}
	dir := filepath.Dir(fp)
	if _, err := os.Stat(filepath.Join(dir, "pkg.fact")); err != nil {
		return "", nil // projection is opt-in per package
	}
	var report []string
	// A goimports failure (typically a syntax error) leaves the file
	// untouched; Refresh below reports the diagnostics.
	if changed, err := goimports(fp); err == nil && changed {
		report = append(report, fmt.Sprintf("goimports rewrote %s (formatting/imports) — re-read it before editing it again", fp))
	}
	if strings.HasSuffix(fp, "_test.go") {
		return strings.Join(report, "\n"), nil
	}
	removed, added, err := Refresh(dir)
	var ce *CompileError
	if errors.As(err, &ce) {
		report = append(report, compileReport(dir, ce))
		return strings.Join(report, "\n"), nil
	}
	if err != nil {
		return strings.Join(report, "\n"), err
	}
	if len(removed)+len(added) > 0 {
		report = append(report, diffReport(dir, removed, added))
	}
	return strings.Join(report, "\n"), nil
}

// diffReport formats the projection diff as the impact report.
func diffReport(dir string, removed, added []string) string {
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
	return b.String()
}

// compileReport formats a CompileError as agent context: the edit left
// the package broken, so the projection is stale by necessity, and the
// compiler diagnostics are the actionable payload.
func compileReport(dir string, ce *CompileError) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s does not compile — pkg.fact not regenerated (stale until the package builds):\n", dir)
	for i, d := range ce.Diagnostics {
		if i == maxHookDiagnostics {
			fmt.Fprintf(&b, "… (%d more errors)\n", len(ce.Diagnostics)-maxHookDiagnostics)
			break
		}
		fmt.Fprintf(&b, "%s\n", relPos(d))
	}
	return b.String()
}

// relPos shortens a diagnostic's absolute file position
// ("/abs/pkg/file.go:3:1: msg") relative to the working directory,
// purely for report brevity.
func relPos(d string) string {
	i := strings.Index(d, ".go:")
	if i < 0 || !filepath.IsAbs(d) {
		return d
	}
	path := d[:i+3]
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, path); err == nil && len(rel) < len(path) {
			return rel + d[i+3:]
		}
	}
	return d
}

// goimports formats fp in place with import fixing (the goimports
// algorithm) and reports whether the file changed. On error (typically a
// syntax error) the file is left untouched.
func goimports(fp string) (bool, error) {
	src, err := os.ReadFile(fp)
	if err != nil {
		return false, err
	}
	out, err := imports.Process(fp, src, nil)
	if err != nil || bytes.Equal(src, out) {
		return false, err
	}
	info, err := os.Stat(fp)
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(fp, out, info.Mode())
}
