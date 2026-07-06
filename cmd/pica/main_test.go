package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseTxtar_AllThreeFiles(t *testing.T) {
	input := `-- data.yaml --
title: Cyprus News
date: 2026-04-11T17:30:00+03:00
tags:
  - news
  - cyprus
-- body.txt --
First paragraph of news prose.

Second paragraph with more detail.
-- sources.txt --
https://cyprus-mail.com/article/one
https://philenews.com/article/two
https://kathimerini.com.cy/article/three
`
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatalf("parseTxtar: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("want map[string]any, got %T", got)
	}

	if m["title"] != "Cyprus News" {
		t.Errorf("title = %v, want %q", m["title"], "Cyprus News")
	}
	wantTags := []any{"news", "cyprus"}
	if !reflect.DeepEqual(m["tags"], wantTags) {
		t.Errorf("tags = %v, want %v", m["tags"], wantTags)
	}
	wantBody := "First paragraph of news prose.\n\nSecond paragraph with more detail."
	if m["body"] != wantBody {
		t.Errorf("body = %q, want %q", m["body"], wantBody)
	}
	wantSources := []string{
		"https://cyprus-mail.com/article/one",
		"https://philenews.com/article/two",
		"https://kathimerini.com.cy/article/three",
	}
	if !reflect.DeepEqual(m["sources"], wantSources) {
		t.Errorf("sources = %v, want %v", m["sources"], wantSources)
	}
}

func TestParseTxtar_OnlyDataYAML(t *testing.T) {
	// Static editors that have all their data in YAML and no
	// separate body / sources still work.
	input := `-- data.yaml --
title: Limassol Weather
temp: 22
`
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatalf("parseTxtar: %v", err)
	}
	m := got.(map[string]any)
	if m["title"] != "Limassol Weather" {
		t.Errorf("title = %v", m["title"])
	}
	if _, ok := m["body"]; ok {
		t.Errorf("body should not be set when body.txt is absent")
	}
	if _, ok := m["sources"]; ok {
		t.Errorf("sources should not be set when sources.txt is absent")
	}
}

func TestParseTxtar_EmptyDataYAML(t *testing.T) {
	// data.yaml is required but its content may be empty.
	input := `-- data.yaml --
-- body.txt --
just a body, no metadata
`
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatalf("parseTxtar: %v", err)
	}
	m := got.(map[string]any)
	if m["body"] != "just a body, no metadata" {
		t.Errorf("body = %v", m["body"])
	}
}

func TestParseTxtar_MissingDataYAML(t *testing.T) {
	// data.yaml is required.
	input := `-- body.txt --
hello
`
	_, err := parseTxtar([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing data.yaml")
	}
	if !strings.Contains(err.Error(), "data.yaml") {
		t.Errorf("error should mention data.yaml: %v", err)
	}
}

func TestParseTxtar_BodyKeyCollision(t *testing.T) {
	// data.yaml owning "body" while body.txt is also present
	// is rejected (ambiguous which wins).
	input := `-- data.yaml --
title: Test
body: this is wrong
-- body.txt --
this is also wrong
`
	_, err := parseTxtar([]byte(input))
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), "body") {
		t.Errorf("error should mention body: %v", err)
	}
}

func TestParseTxtar_SourcesKeyCollision(t *testing.T) {
	input := `-- data.yaml --
title: Test
sources:
  - https://wrong-place.example/
-- sources.txt --
https://right-place.example/
`
	_, err := parseTxtar([]byte(input))
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), "sources") {
		t.Errorf("error should mention sources: %v", err)
	}
}

func TestParseTxtar_SourcesEmptyAndBlankLinesDropped(t *testing.T) {
	input := "-- data.yaml --\ntitle: Test\n-- sources.txt --\n\nhttps://example.com/one\n   \nhttps://example.com/two\n\n"
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatalf("parseTxtar: %v", err)
	}
	m := got.(map[string]any)
	want := []string{"https://example.com/one", "https://example.com/two"}
	if !reflect.DeepEqual(m["sources"], want) {
		t.Errorf("sources = %v, want %v", m["sources"], want)
	}
}

func TestParseTxtar_SourcesPerLineTrimmed(t *testing.T) {
	input := "-- data.yaml --\nx: 1\n-- sources.txt --\n  https://example.com/spaces  \n"
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatalf("parseTxtar: %v", err)
	}
	m := got.(map[string]any)
	want := []string{"https://example.com/spaces"}
	if !reflect.DeepEqual(m["sources"], want) {
		t.Errorf("sources = %v, want %v", m["sources"], want)
	}
}

func TestParseTxtar_BodyTrailingWhitespaceTrimmed(t *testing.T) {
	// We trim trailing whitespace (including newlines) but preserve
	// internal blank lines (paragraph separators).
	input := "-- data.yaml --\nx: 1\n-- body.txt --\nfirst para\n\nsecond para\n\n\n\n"
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatalf("parseTxtar: %v", err)
	}
	m := got.(map[string]any)
	want := "first para\n\nsecond para"
	if m["body"] != want {
		t.Errorf("body = %q, want %q", m["body"], want)
	}
}

func TestParseTxtar_UnknownFilesIgnored(t *testing.T) {
	// Forward compatibility: extra files in the archive must be
	// silently ignored, not rejected.
	input := `-- data.yaml --
title: Test
-- body.txt --
hello
-- future.txt --
some future thing we don't know about yet
`
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatalf("parseTxtar: %v", err)
	}
	m := got.(map[string]any)
	if m["title"] != "Test" {
		t.Errorf("title = %v", m["title"])
	}
	if _, ok := m["future"]; ok {
		t.Errorf("unknown file should not appear in template data")
	}
}

func TestParseTxtar_MalformedYAML(t *testing.T) {
	input := "-- data.yaml --\nthis: is: not: valid: yaml\n  - oops\n"
	_, err := parseTxtar([]byte(input))
	if err == nil {
		t.Fatal("expected YAML parse error")
	}
}

// Sanity check that errors.Is patterns still work for callers that
// might want to distinguish parseTxtar failures.
func TestParseTxtar_ErrorsAreErrors(t *testing.T) {
	_, err := parseTxtar([]byte("-- body.txt --\nx\n"))
	if err == nil {
		t.Fatal("expected error")
	}
	// Just smoke-test that the returned value is an error.
	var e error = err
	if !errors.Is(e, e) {
		t.Errorf("err should satisfy errors.Is(err, err)")
	}
}
