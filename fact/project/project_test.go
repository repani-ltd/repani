package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"repani.com/fact"
)

const bankSrc = `package bank

import "errors"

// MaxAmount caps a single posting.
const MaxAmount = 1 << 20

// ErrClosed reports posting to a closed ledger.
var ErrClosed = errors.New("ledger closed")

// Poster posts amounts to a ledger.
type Poster interface {
	Post(amount int) error
}

// MemLedger is an in-memory ledger.
type MemLedger struct {
	Balance int
}

func (m *MemLedger) Post(amount int) error {
	m.Balance += amount
	return nil
}

// Reset empties the ledger via Post.
func (m *MemLedger) Reset() error {
	return m.Post(-m.Balance)
}

// Submit validates and posts an amount.
func Submit(l Poster, amount int) error {
	if amount <= 0 {
		return errors.New("bad amount")
	}
	return l.Post(amount)
}
`

// writeBankModule lays out bankSrc as a tiny standalone module.
func writeBankModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":  "module bank\n\ngo 1.25\n",
		"bank.go": bankSrc,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestProjectGoPackage(t *testing.T) {
	dir := writeBankModule(t)
	lines, err := Lines(dir)
	if err != nil {
		t.Fatal(err)
	}

	// The projection must itself be valid FACT.
	facts, errs := fact.Load([]byte(strings.Join(lines, "\n") + "\n"))
	for _, e := range errs {
		t.Errorf("projection is invalid FACT: %s", e.Error())
	}
	canonical := string(fact.Canonical(facts))

	want := []string{
		`pkg.path: str = "bank"`,
		`imports: list(str) = ["errors"]`,
		`type:Poster.kind: enum(struct|iface|basic) = iface`,
		`type:Poster.method_Post_sig: str = "func(amount int) error"`,
		`type:MemLedger.kind: enum(struct|iface|basic) = struct`,
		`type:MemLedger.fields: list(str) = ["Balance"]`,
		`type:MemLedger.field_Balance_type: str = "int"`,
		`type:MemLedger.methods: list(str) = ["Post", "Reset"]`,
		// The killer query (SPEC §12.1): computed interface satisfaction.
		`type:MemLedger.implements: list(ref(type)) = [type:Poster]`,
		`method:MemLedger_Post.receiver: str = "MemLedger"`,
		// Method bodies carry call edges too — without them the reverse
		// call query sees only the free-function half of the call graph.
		`method:MemLedger_Post.calls: list(str) = []`,
		`method:MemLedger_Reset.calls: list(str) = ["MemLedger.Post"]`,
		// Package-level consts and vars are API surface (error sentinels
		// above all) and appear in dependency-upgrade diffs.
		`const:MaxAmount.type: str = "untyped int"`,
		`const:MaxAmount.file: str = "bank.go"`,
		`var:ErrClosed.type: str = "error"`,
		`var:ErrClosed.file: str = "bank.go"`,
		`func:Submit.sig: str = "func(l Poster, amount int) error"`,
		`func:Submit.exported: bool = true`,
		// All-str call edges (SPEC §6.4): static callee through the interface,
		// external symbol as source-qualified string.
		`func:Submit.calls: list(str) = ["Poster.Post", "errors.New"]`,
		`func:Submit.file: str = "bank.go"`,
	}
	for _, w := range want {
		if !strings.Contains(canonical, w+"\n") {
			t.Errorf("projection missing fact:\n  %s", w)
		}
	}
	if t.Failed() {
		t.Logf("full projection:\n%s", canonical)
	}
}

// Regression: packages importing other module-local packages must project
// (the stdlib source importer could not resolve them; go/packages can).
func TestProjectPackageWithLocalImports(t *testing.T) {
	lines, err := Lines("../cmd/fact") // imports repani.com/fact and repani.com/fact/project
	if err != nil {
		t.Fatal(err)
	}
	facts, errs := fact.Load([]byte(strings.Join(lines, "\n") + "\n"))
	for _, e := range errs {
		t.Errorf("projection is invalid FACT: %s", e.Error())
	}
	canonical := string(fact.Canonical(facts))
	for _, w := range []string{
		"func:main.exported: bool = false\n",
		`pkg.path: str = "repani.com/fact/cmd/fact"` + "\n",
		`"repani.com/fact", "repani.com/fact/project"`,
	} {
		if !strings.Contains(canonical, w) {
			t.Errorf("projection missing %q\nfull projection:\n%s", w, canonical)
		}
	}
}

// canonProjection regenerates dir's projection in canonical form.
func canonProjection(t *testing.T, dir string) string {
	t.Helper()
	lines, err := Lines(dir)
	if err != nil {
		t.Fatal(err)
	}
	facts, _ := fact.Parse([]byte(strings.Join(lines, "\n") + "\n"))
	return string(fact.Canonical(facts))
}

func TestProjectionIsDeterministic(t *testing.T) {
	dir := writeBankModule(t)
	first, second := canonProjection(t, dir), canonProjection(t, dir)
	if first != second {
		t.Error("regenerating an unchanged package is not byte-identical")
	}
}

// The churn invariant (SPEC §11.2): edits that do not change the declaration
// layer regenerate byte-identically. The edit below shifts every line after
// it, so this fails for any projection carrying line numbers.
func TestProjectionIgnoresNonDeclarationEdits(t *testing.T) {
	dir := writeBankModule(t)
	before := canonProjection(t, dir)
	edited := strings.Replace(bankSrc, "\tm.Balance += amount",
		"\t// comment inside a body\n\n\tm.Balance += amount", 1)
	if edited == bankSrc {
		t.Fatal("edit did not apply")
	}
	if err := os.WriteFile(filepath.Join(dir, "bank.go"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := canonProjection(t, dir); after != before {
		t.Errorf("body-only edit changed the projection:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestProjectMethodIDCollision: receiver A_B with method C and
// receiver A with method B_C flatten to the same compound id; the
// generator must report that, not emit duplicate keys.
func TestProjectMethodIDCollision(t *testing.T) {
	dir := t.TempDir()
	src := `package amb

type A struct{}

func (A) B_C() {}

type A_B struct{}

func (A_B) C() {}
`
	for name, body := range map[string]string{
		"go.mod": "module amb\n\ngo 1.25\n",
		"amb.go": src,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Lines(dir)
	if err == nil || !strings.Contains(err.Error(), "A_B_C") || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("Lines: err=%v, want method id collision diagnostic", err)
	}
}
