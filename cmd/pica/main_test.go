package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseTxtar_FactsAndContent(t *testing.T) {
	input := `-- data.fact --
title: str = "Weather"
issue: int = 187
current.temp: float = 26.4
tags: list(str) = ["news", "cyprus"]
sources: list(str) = ["https://a", "https://b"]
daily: list(ref(day)) = [day:d0, day:d1]
day:d0.hi: float = 31.2
day:d1.hi: float = 30.4
-- body.txt --
first para

second para
`
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["title"] != "Weather" || m["issue"] != int64(187) {
		t.Errorf("scalar facts wrong: %+v", m)
	}
	if m["current"].(map[string]any)["temp"] != 26.4 {
		t.Errorf("nested fact wrong: %+v", m["current"])
	}
	if !reflect.DeepEqual(m["tags"], []any{"news", "cyprus"}) {
		t.Errorf("tags = %#v", m["tags"])
	}
	if !reflect.DeepEqual(m["sources"], []any{"https://a", "https://b"}) {
		t.Errorf("sources = %#v", m["sources"])
	}
	rows := m["daily"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["hi"] != 31.2 {
		t.Errorf("ordered rows wrong: %#v", m["daily"])
	}
	if m["body"] != "first para\n\nsecond para" {
		t.Errorf("body content wrong: %q", m["body"])
	}
}

func TestParseTxtar_EmptyDataFact(t *testing.T) {
	// data.fact is required but its content may be empty.
	input := "-- data.fact --\n-- body.txt --\nhello\n"
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"body": "hello"}
	if !reflect.DeepEqual(got, map[string]any(want)) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestParseTxtar_MissingDataFact(t *testing.T) {
	_, err := parseTxtar([]byte("-- body.txt --\nx\n"))
	if err == nil || !strings.Contains(err.Error(), "data.fact") {
		t.Fatalf("want missing-data.fact error, got %v", err)
	}
}

func TestParseTxtar_ContentKeyCollision(t *testing.T) {
	// A .txt member whose key data.fact also defines is rejected --
	// the FACT duplicate rule extended to the archive.
	input := "-- data.fact --\nbody: str = \"inline\"\n-- body.txt --\nprose\n"
	_, err := parseTxtar([]byte(input))
	if err == nil || !strings.Contains(err.Error(), `"body"`) {
		t.Fatalf("want body collision error, got %v", err)
	}
}

func TestParseTxtar_AnyTxtMemberInjected(t *testing.T) {
	input := "-- data.fact --\nx: int = 1\n-- synopsis.txt --\nshort\n-- outlook.txt --\nlong view\n"
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["synopsis"] != "short" || m["outlook"] != "long view" {
		t.Errorf("txt members not injected: %+v", m)
	}
}

func TestParseTxtar_InvalidFactsRejected(t *testing.T) {
	cases := []string{
		"-- data.fact --\nx: uint32 = 1\n",             // E004 illegal type
		"-- data.fact --\nx: int = 1\nx: int = 2\n",    // E007 duplicate
		"-- data.fact --\nx: ref(day) = day:missing\n", // E008 dangling ref
		"-- data.fact --\nnot a fact line at all\n",    // E001
	}
	for _, input := range cases {
		if _, err := parseTxtar([]byte(input)); err == nil {
			t.Errorf("want error for %q", input)
		}
	}
}

func TestParseTxtar_BodyTrailingWhitespaceTrimmed(t *testing.T) {
	input := "-- data.fact --\nx: int = 1\n-- body.txt --\nfirst para\n\nsecond para\n\n\n\n"
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["body"] != "first para\n\nsecond para" {
		t.Errorf("body not trimmed: %q", m["body"])
	}
}

func TestParseTxtar_UnknownFilesIgnored(t *testing.T) {
	input := "-- data.fact --\nx: int = 1\n-- notes.md --\nignored\n"
	got, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if _, exists := m["notes"]; exists {
		t.Errorf("non-.txt member should be ignored: %+v", m)
	}
}

func TestParseTxtar_EmptyArchive(t *testing.T) {
	if _, err := parseTxtar(nil); err == nil {
		t.Fatal("expected error for empty archive")
	}
}

func TestBindFacts_Plain(t *testing.T) {
	got, err := bindFacts([]byte("a.b: int = 1\nflag: bool = true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got["a"].(map[string]any)["b"] != int64(1) || got["flag"] != true {
		t.Errorf("bindFacts wrong: %+v", got)
	}
}

func TestParseArchive(t *testing.T) {
	files := parseArchive("comment ignored\n-- a.txt --\nline\n-- b.txt --\n")
	if len(files) != 2 || files[0].name != "a.txt" || files[0].data != "line\n" || files[1].data != "" {
		t.Errorf("parseArchive wrong: %#v", files)
	}
	if _, ok := markerName("-- x --"); !ok {
		t.Error("marker not recognized")
	}
	if _, ok := markerName("--x--"); ok {
		t.Error("non-marker recognized")
	}
}

func TestCheckCmd(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.t")
	bad := filepath.Join(dir, "bad.t")
	os.WriteFile(good, []byte("T\n\nprose\n"), 0o644)
	os.WriteFile(bad, []byte("T\n\n.bogus\n"), 0o644)
	if got := checkCmd([]string{good}); got != 0 {
		t.Errorf("check(good) = %d, want 0", got)
	}
	if got := checkCmd([]string{bad}); got != 1 {
		t.Errorf("check(bad) = %d, want 1", got)
	}
}
