package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	for _, args := range [][]string{{"fmt", "-h"}} {
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

// Input-free commands must not touch stdin: a reader that blocks
// (here: one that fails loudly) proves spec/help dispatch before
// the input read. Regression for the 2026-08-20 hang.
func TestInputFreeCommandsIgnoreStdin(t *testing.T) {
	for _, args := range [][]string{{"spec"}, {"-h"}, {"help"}, {"nonsense"}} {
		var out, errb bytes.Buffer
		rc := run(args, failingReader{}, &out, &errb)
		if args[0] == "spec" || args[0] == "-h" || args[0] == "help" {
			if rc != 0 {
				t.Errorf("%v: rc=%d, stderr=%s", args, rc, errb.String())
			}
		} else if rc != 2 {
			t.Errorf("%v: rc=%d, want 2 (usage)", args, rc)
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	panic("input-free command read stdin")
}
