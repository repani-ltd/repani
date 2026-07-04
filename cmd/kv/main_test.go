package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncRunes(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"abc", 5, "abc"},       // fits
		{"abcdef", 6, "abcdef"}, // exact
		{"abcdef", 4, "abc…"},   // truncated with ellipsis
		{"héllo", 3, "hé…"},     // rune-aware (é is one rune)
		{"abc", 1, "a"},         // w<=1: no room for ellipsis
	}
	for _, c := range cases {
		got := truncRunes(c.in, c.w)
		if got != c.want {
			t.Errorf("truncRunes(%q,%d)=%q want %q", c.in, c.w, got, c.want)
		}
		if n := utf8.RuneCountInString(got); n > c.w {
			t.Errorf("truncRunes(%q,%d)=%q is %d runes, exceeds %d", c.in, c.w, got, n, c.w)
		}
	}
}

func TestChunkRunes(t *testing.T) {
	got := chunkRunes("abcdefg", 3)
	want := []string{"abc", "def", "g"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chunkRunes = %v, want %v", got, want)
	}
	if c := chunkRunes("", 4); len(c) != 1 || c[0] != "" {
		t.Fatalf("chunkRunes empty = %v, want [\"\"]", c)
	}
	// Reassembly is lossless.
	if strings.Join(chunkRunes("hello world", 4), "") != "hello world" {
		t.Fatal("chunkRunes not lossless")
	}
}

func TestPadRunes(t *testing.T) {
	if got := padRunes("ab", 5); got != "ab   " {
		t.Fatalf("padRunes short = %q", got)
	}
	if got := padRunes("abcdef", 3); got != "abcdef" {
		t.Fatalf("padRunes overflow must not shrink: %q", got)
	}
}

func TestCollapseCell(t *testing.T) {
	got := collapseCell("a\nb\tc\r\nd")
	if strings.ContainsAny(got, "\n\t\r") {
		t.Fatalf("collapseCell left a control char: %q", got)
	}
	if !strings.Contains(got, "⏎") {
		t.Fatalf("collapseCell dropped the newline marker: %q", got)
	}
}
