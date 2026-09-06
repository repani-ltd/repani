package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProjection generates and stores dir's pkg.fact as fact project -w does.
func writeProjection(t *testing.T, dir string) {
	t.Helper()
	out, err := File(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg.fact"), out, 0o444); err != nil {
		t.Fatal(err)
	}
}

// hookPayload is a PostToolUse payload for an edit of path in a session
// whose state lives in a per-test directory.
func hookPayload(path string) []byte {
	return fmt.Appendf(nil, `{"session_id":"test","hook_event_name":"PostToolUse","tool_input":{"file_path":%q}}`, path)
}

func stopPayload(active bool) []byte {
	return fmt.Appendf(nil, `{"session_id":"test","hook_event_name":"Stop","stop_hook_active":%v}`, active)
}

// isolateState points the hook's session state at a fresh directory.
func isolateState(t *testing.T) {
	t.Helper()
	t.Setenv("FACT_HOOK_STATE", t.TempDir())
}

func TestHookSummarisesAdditions(t *testing.T) {
	isolateState(t)
	dir := writeBankModule(t)
	writeProjection(t, dir)

	src := bankSrc + "\n// Drain empties the ledger.\nfunc Drain(l *MemLedger) { l.Balance = 0 }\n"
	if err := os.WriteFile(filepath.Join(dir, "bank.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, err := Hook(hookPayload(filepath.Join(dir, "bank.go")))
	if err != nil {
		t.Fatal(err)
	}
	// An addition breaks no caller: one line naming it, no diff lines.
	if !strings.Contains(ctx, "added func:Drain") {
		t.Errorf("hook context missing the additions line\ngot:\n%s", ctx)
	}
	if strings.Contains(ctx, "+ ") || strings.Contains(ctx, "- ") || strings.Contains(ctx, "impact report") {
		t.Errorf("additive edit reported a line diff:\n%s", ctx)
	}
	if strings.Count(ctx, "\n") > 1 {
		t.Errorf("additive edit reported more than one line:\n%s", ctx)
	}
	// The stored projection was rewritten and is now fresh.
	stored, err := os.ReadFile(filepath.Join(dir, "pkg.fact"))
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := File(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(fresh) {
		t.Error("hook left pkg.fact stale")
	}
}

func TestHookReportsRemovalsAsImpact(t *testing.T) {
	isolateState(t)
	dir := writeBankModule(t)
	writeProjection(t, dir)

	// Renaming an exported method removes a declaration: every caller
	// breaks, so the diff is listed line by line.
	src := strings.Replace(bankSrc, "func (m *MemLedger) Reset() error {", "func (m *MemLedger) Clear() error {", 1)
	if src == bankSrc {
		t.Fatal("fixture has no Reset method")
	}
	if err := os.WriteFile(filepath.Join(dir, "bank.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, err := Hook(hookPayload(filepath.Join(dir, "bank.go")))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"impact report", "- method:MemLedger_Reset", "+ method:MemLedger_Clear"} {
		if !strings.Contains(ctx, w) {
			t.Errorf("hook context missing %q\ngot:\n%s", w, ctx)
		}
	}
}

func TestHookCompileDeltaAndStop(t *testing.T) {
	isolateState(t)
	dir := writeBankModule(t)
	writeProjection(t, dir)
	path := filepath.Join(dir, "bank.go")

	// First broken save: the error is new and listed.
	src := bankSrc + "\nfunc Broken() { undefinedSymbol() }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, err := Hook(hookPayload(path))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx, "undefinedSymbol") {
		t.Errorf("first save did not list the new error:\n%s", ctx)
	}
	// Second save with the same error: counted, not repeated.
	ctx, err = Hook(hookPayload(path))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ctx, "undefinedSymbol") || !strings.Contains(ctx, "1 errors persist") {
		t.Errorf("repeated error was not collapsed to a count:\n%s", ctx)
	}
	// A syntax error in the edited file is always listed.
	if err := os.WriteFile(path, []byte(src+"func {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, err = Hook(hookPayload(path))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx, "expected") && !strings.Contains(ctx, "syntax") {
		t.Errorf("syntax error not listed:\n%s", ctx)
	}
	// The turn ends with the package broken: the stop is refused, once.
	reason, err := Stop(stopPayload(false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "does not compile") {
		t.Errorf("stop with a broken package was not refused: %q", reason)
	}
	// State is cleared by the stop; a second, active stop passes.
	if reason, _ := Stop(stopPayload(true)); reason != "" {
		t.Errorf("stop after a refusal refused again: %q", reason)
	}
	// Fixed: the save says so, and the stop is quiet.
	if err := os.WriteFile(path, []byte(bankSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Hook(hookPayload(path + "x")); err != nil { // not .go: ignored
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	Hook(hookPayload(path)) // broken again, state records it
	if err := os.WriteFile(path, []byte(bankSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, err = Hook(hookPayload(path))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx, "compiles again") {
		t.Errorf("recovery not reported:\n%s", ctx)
	}
	if reason, _ := Stop(stopPayload(false)); reason != "" {
		t.Errorf("stop with everything building was refused: %q", reason)
	}
}

func TestHookSilentOnDeclarationNeutralEdit(t *testing.T) {
	isolateState(t)
	dir := writeBankModule(t)
	writeProjection(t, dir)

	src := strings.Replace(bankSrc, "\tm.Balance += amount",
		"\t// comment inside a body\n\tm.Balance += amount", 1)
	if err := os.WriteFile(filepath.Join(dir, "bank.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, err := Hook(hookPayload(filepath.Join(dir, "bank.go")))
	if err != nil {
		t.Fatal(err)
	}
	if ctx != "" {
		t.Errorf("declaration-neutral edit produced context:\n%s", ctx)
	}
}

func TestHookReportsCompileErrors(t *testing.T) {
	isolateState(t)
	dir := writeBankModule(t)
	writeProjection(t, dir)
	before, err := os.ReadFile(filepath.Join(dir, "pkg.fact"))
	if err != nil {
		t.Fatal(err)
	}

	src := bankSrc + "\nfunc Broken() { undefinedSymbol() }\n"
	if err := os.WriteFile(filepath.Join(dir, "bank.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, err := Hook(hookPayload(filepath.Join(dir, "bank.go")))
	if err != nil {
		t.Fatalf("compile errors must be context, not a hook error: %v", err)
	}
	for _, w := range []string{"does not compile", "undefinedSymbol"} {
		if !strings.Contains(ctx, w) {
			t.Errorf("hook context missing %q\ngot:\n%s", w, ctx)
		}
	}
	after, err := os.ReadFile(filepath.Join(dir, "pkg.fact"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("broken package rewrote pkg.fact")
	}
}

func TestHookRunsGoimports(t *testing.T) {
	isolateState(t)
	dir := writeBankModule(t)
	writeProjection(t, dir)

	// Uses fmt without importing it: goimports adds the import, after
	// which the package compiles and the projection picks up the decl —
	// formatting must run before regeneration.
	src := "package bank\n\nfunc Greet() string { return fmt.Sprintf(\"hi\") }\n"
	path := filepath.Join(dir, "greet.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, err := Hook(hookPayload(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		"goimports rewrote",
		"added func:Greet",
	} {
		if !strings.Contains(ctx, w) {
			t.Errorf("hook context missing %q\ngot:\n%s", w, ctx)
		}
	}
	fixed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fixed), `import "fmt"`) {
		t.Errorf("goimports did not add the fmt import:\n%s", fixed)
	}
}

func TestHookFormatsTestFilesWithoutProjecting(t *testing.T) {
	isolateState(t)
	dir := writeBankModule(t)
	writeProjection(t, dir)
	before, err := os.ReadFile(filepath.Join(dir, "pkg.fact"))
	if err != nil {
		t.Fatal(err)
	}

	src := "package bank\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {   t.Log(\"x\")   }\n"
	path := filepath.Join(dir, "bank_test.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, err := Hook(hookPayload(path))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx, "goimports rewrote") {
		t.Errorf("misformatted test file not reformatted:\n%s", ctx)
	}
	if strings.Contains(ctx, "impact report") || strings.Contains(ctx, "added") {
		t.Errorf("test file produced an impact report:\n%s", ctx)
	}
	after, err := os.ReadFile(filepath.Join(dir, "pkg.fact"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("test-file edit rewrote pkg.fact")
	}
}

func TestHookSkipsNonTargets(t *testing.T) {
	isolateState(t)
	dir := writeBankModule(t) // no pkg.fact stored: projection not opted in
	for _, path := range []string{
		filepath.Join(dir, "bank.go"),      // .go, but package carries no pkg.fact
		filepath.Join(dir, "bank_test.go"), // test files are outside the projection
		filepath.Join(dir, "go.mod"),       // not a .go file
	} {
		ctx, err := Hook(hookPayload(path))
		if err != nil {
			t.Errorf("%s: %v", path, err)
		}
		if ctx != "" {
			t.Errorf("%s: expected silence, got:\n%s", path, ctx)
		}
	}
}
