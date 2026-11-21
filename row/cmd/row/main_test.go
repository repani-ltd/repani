package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runText(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errs strings.Builder
	code := run(args, strings.NewReader(stdin), &out, &errs)
	return code, out.String(), errs.String()
}

func TestEncodeDecode(t *testing.T) {
	code, out, errs := runText(t, "0: str = \"EUR\"\n1: dec = 90.00\n", "encode")
	// 90.00 is mantissa 9000, zigzag 18000: past 16511, so three varint
	// bytes, 80 8b 50 (((0+1)*128 + 11 + 1)*128 + 0x50 = 18000).
	if code != 0 || out != "0203455552d2808b50\n" {
		t.Fatalf("encode = %d %q %q", code, out, errs)
	}
	code, out, errs = runText(t, out, "decode")
	if code != 0 || out != "0: str = \"EUR\"\n1: dec = 90.00\n" {
		t.Errorf("decode = %d %q %q", code, out, errs)
	}
	if code, _, errs := runText(t, "0: int = abc\n", "encode"); code != 1 || !strings.HasPrefix(errs, "stdin:1: T004 at 0") {
		t.Errorf("bad encode = %d %q", code, errs)
	}
}

func TestCheckAndFmt(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.row")
	os.WriteFile(f, []byte("1: int = +7\n0: str = \"a\"\n"), 0o644)
	if code, out, _ := runText(t, "", "check", f); code != 0 || !strings.Contains(out, "not canonical") {
		t.Errorf("check = %d %q", code, out)
	}
	if code, out, _ := runText(t, "", "fmt", f); code != 0 || out != "0: str = \"a\"\n1: int = 7\n" {
		t.Errorf("fmt = %d %q", code, out)
	}
	if code, _, errs := runText(t, "", "fmt", "-w", f); code != 0 || errs != "" {
		t.Fatalf("fmt -w = %d %q", code, errs)
	}
	if code, out, _ := runText(t, "", "check", f); code != 0 || out != "" {
		t.Errorf("check after fmt -w = %d %q", code, out)
	}
	os.WriteFile(f, []byte("\"k\" 0: int = 1\n0: int = 2\n"), 0o644)
	if code, out, _ := runText(t, "", "check", f); code != 1 || !strings.Contains(out, ":2: T007") {
		t.Errorf("check mixed = %d %q", code, out)
	}
}
