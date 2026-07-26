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

func hookPayload(path string) []byte {
	return fmt.Appendf(nil, `{"tool_input":{"file_path":%q}}`, path)
}

func TestHookReportsDeclarationImpact(t *testing.T) {
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
	for _, w := range []string{
		"impact report",
		`+ func:Drain.sig: str = "func(l *MemLedger)"`,
		`+ func:Drain.file: str = "bank.go"`,
	} {
		if !strings.Contains(ctx, w) {
			t.Errorf("hook context missing %q\ngot:\n%s", w, ctx)
		}
	}
	if strings.Contains(ctx, "- ") {
		t.Errorf("additive edit reported removed facts:\n%s", ctx)
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

func TestHookSilentOnDeclarationNeutralEdit(t *testing.T) {
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
		"impact report",
		`+ func:Greet.sig: str = "func() string"`,
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
	if strings.Contains(ctx, "impact report") {
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
