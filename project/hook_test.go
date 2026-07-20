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
