package main

import (
	"strings"
	"testing"
)

// TestCheck: every bad row is reported as NAME:LINE:COL: CODE, blank
// lines are skipped, and the exit code says whether any row was bad.
func TestCheck(t *testing.T) {
	in := "\"x\" 2026-10-08 90.00\n\n-0\n1  2\nnope\n"
	var out strings.Builder
	if code := check(strings.NewReader(in), "f", &out); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	// 1  2 first differs from 1 2 at its second space, column 3.
	want := []string{"f:3:1: T002 ", "f:4:3: T002 ", "f:5:1: T001 "}
	got := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d reports:\n%s", len(got), out.String())
	}
	for i, w := range want {
		if !strings.HasPrefix(got[i], w) {
			t.Errorf("report %d = %q, want prefix %q", i, got[i], w)
		}
	}
	out.Reset()
	if code := check(strings.NewReader("1 2\n\"a\"\n"), "f", &out); code != 0 || out.Len() != 0 {
		t.Errorf("valid rows: exit %d, %q", code, out.String())
	}
}

func TestEncodeDecodeLine(t *testing.T) {
	h, err := encodeLine(`"EUR" 90.00`)
	if err != nil || h != "03455552f702d08c01" {
		t.Fatalf("encode = %q, %v", h, err)
	}
	if s, err := decodeLine(h); err != nil || s != `"EUR" 90.00` {
		t.Errorf("decode = %q, %v", s, err)
	}
	if _, err := decodeLine("zz"); err == nil {
		t.Error("decoded non-hex")
	}
}
