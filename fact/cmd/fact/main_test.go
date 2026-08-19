package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const doc = "b.y: str = \"two\"\na.x: int = 1\n"
const canonical = "a.x: int = 1\nb.y: str = \"two\"\n"

// exec runs the command in-process and returns exit code, stdout, stderr.
func exec(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"bogus"}, {"validate", "a", "b"}, {"fmt", "-nope"}} {
		if code, _, stderr := exec(t, "", args...); code != 2 || stderr == "" {
			t.Errorf("%v: exit %d, stderr %q; want 2 and a message", args, code, stderr)
		}
	}
	// -h is a served request, not a usage error.
	for _, args := range [][]string{{"fmt", "-h"}, {"project", "-h"}} {
		if code, _, stderr := exec(t, "", args...); code != 0 || stderr == "" {
			t.Errorf("%v: exit %d, stderr %q; want 0 and the flag usage", args, code, stderr)
		}
	}
}

func TestValidateFmtEncodeDecode(t *testing.T) {
	code, stdout, _ := exec(t, doc, "validate")
	if code != 0 || stdout != "ok: 2 facts\n" {
		t.Errorf("validate: exit %d, %q", code, stdout)
	}
	code, _, stderr := exec(t, "a.x: int = 1\na.x: int = 2\nnope\n", "validate")
	if code != 1 || !strings.Contains(stderr, "line 3: E001") || !strings.Contains(stderr, "line 2: E007") {
		t.Errorf("validate invalid: exit %d, stderr %q", code, stderr)
	}
	if code, stdout, _ := exec(t, doc, "fmt"); code != 0 || stdout != canonical {
		t.Errorf("fmt: exit %d, %q", code, stdout)
	}
	code, enc, _ := exec(t, doc, "encode")
	if code != 0 || !strings.Contains(enc, `"key": "a.x"`) {
		t.Errorf("encode: exit %d, %q", code, enc)
	}
	if code, stdout, _ := exec(t, enc, "decode"); code != 0 || stdout != canonical {
		t.Errorf("decode: exit %d, %q", code, stdout)
	}
	if code, _, stderr := exec(t, `[{"key":"a","type":"ref(p)","value":"p:q"}]`, "decode"); code != 1 || !strings.Contains(stderr, "E008") {
		t.Errorf("decode invalid: exit %d, stderr %q", code, stderr)
	}
}

func TestFmtWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.fact")
	os.WriteFile(path, []byte(doc), 0o644)
	if code, stdout, stderr := exec(t, "", "fmt", "-w", path); code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("fmt -w: exit %d, out %q, err %q", code, stdout, stderr)
	}
	if got, _ := os.ReadFile(path); string(got) != canonical {
		t.Errorf("file after fmt -w:\n%s", got)
	}
	if _, _, stderr := exec(t, "", "fmt", "-w", filepath.Join(t.TempDir(), "missing.fact")); !strings.HasPrefix(stderr, "fact: ") {
		t.Errorf("missing file: stderr %q", stderr)
	}
}

// writeModule lays out a one-file Go module and returns its directory.
func writeModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":  "module tiny\n\ngo 1.25\n",
		"tiny.go": "package tiny\n\n// Answer is it.\nfunc Answer() int { return 42 }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestProject(t *testing.T) {
	dir := writeModule(t)
	target := filepath.Join(dir, "pkg.fact")

	for _, args := range [][]string{
		{"project", "-w", "-o", target, dir},
		{"project", "-w", "-check", dir},
		{"project", dir, "extra"},
	} {
		if code, _, _ := exec(t, "", args...); code != 2 {
			t.Errorf("%v: exit %d, want 2 (usage)", args, code)
		}
	}

	code, stdout, _ := exec(t, "", "project", dir)
	if code != 0 || !strings.Contains(stdout, "func:Answer.sig: str = \"func() int\"\n") {
		t.Fatalf("project stdout: exit %d\n%s", code, stdout)
	}

	// -check before any file: stale, with the -w hint.
	code, _, stderr := exec(t, "", "project", "-check", dir)
	if code != 1 || !strings.Contains(stderr, "regenerate with: fact project -w "+dir) {
		t.Errorf("check stale: exit %d, stderr %q", code, stderr)
	}

	// -w writes read-only; a second -w leaves the file untouched.
	if code, _, stderr := exec(t, "", "project", "-w", dir); code != 0 {
		t.Fatalf("project -w: exit %d, %s", code, stderr)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("pkg.fact mode = %v, err %v", info, err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != stdout {
		t.Errorf("-w wrote something else than stdout:\n%s", got)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	os.Chtimes(target, old, old)
	if code, _, _ := exec(t, "", "project", "-w", dir); code != 0 {
		t.Fatal("second -w failed")
	}
	if again, _ := os.Stat(target); !again.ModTime().Equal(old) {
		t.Error("unchanged projection was rewritten (mtime churn)")
	}
	if code, stdout, _ := exec(t, "", "project", "-check", dir); code != 0 || stdout != "ok: "+target+" is fresh\n" {
		t.Errorf("check fresh: exit %d, %q", code, stdout)
	}

	// -o: a separate target (parent created), its own stale hint.
	alt := filepath.Join(dir, "facts", "tiny", "pkg.fact")
	code, _, stderr = exec(t, "", "project", "-o", alt, "-check", dir)
	if code != 1 || !strings.Contains(stderr, "regenerate with: fact project -o "+alt+" "+dir) {
		t.Errorf("check -o stale: exit %d, stderr %q", code, stderr)
	}
	if code, _, stderr := exec(t, "", "project", "-o", alt, dir); code != 0 {
		t.Fatalf("project -o: exit %d, %s", code, stderr)
	}
	if code, _, _ := exec(t, "", "project", "-o", alt, "-check", dir); code != 0 {
		t.Error("check -o fresh: want exit 0")
	}

	// A declaration edit makes the stored file stale.
	os.WriteFile(filepath.Join(dir, "tiny.go"), []byte("package tiny\n\nfunc Answer() int64 { return 42 }\n"), 0o644)
	if code, _, _ := exec(t, "", "project", "-check", dir); code != 1 {
		t.Error("check after declaration edit: want stale")
	}
	if code, _, _ := exec(t, "", "project", "-w", dir); code != 0 {
		t.Fatal("rewrite after edit failed")
	}
	if code, _, _ := exec(t, "", "project", "-check", dir); code != 0 {
		t.Error("check after rewrite: want fresh")
	}

	// A package that does not compile is exit 1, not a projection.
	os.WriteFile(filepath.Join(dir, "tiny.go"), []byte("package tiny\n\nfunc Answer() int { return }\n"), 0o644)
	if code, _, stderr := exec(t, "", "project", dir); code != 1 || !strings.Contains(stderr, "fact: load") {
		t.Errorf("broken package: exit %d, stderr %q", code, stderr)
	}
}
