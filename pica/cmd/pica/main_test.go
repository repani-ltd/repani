package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	m, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
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
	m, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
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
	m, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if m["body"] != "first para\n\nsecond para" {
		t.Errorf("body not trimmed: %q", m["body"])
	}
}

func TestParseTxtar_UnknownFilesIgnored(t *testing.T) {
	input := "-- data.fact --\nx: int = 1\n-- notes.md --\nignored\n"
	m, err := parseTxtar([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
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

// capture runs f with stdout and stderr redirected, returning what
// it wrote.
func capture(t *testing.T, f func()) (out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	stdout, stderr = &o, &e
	defer func() { stdout, stderr = os.Stdout, os.Stderr }()
	f()
	return o.String(), e.String()
}

func TestParseMixed(t *testing.T) {
	cases := []struct {
		args    []string
		wantPos []string
		wantO   string
		wantB   bool
		wantErr bool
	}{
		{[]string{"a", "b"}, []string{"a", "b"}, "", false, false},
		{[]string{"-o", "x", "a", "-b", "b"}, []string{"a", "b"}, "x", true, false},
		{[]string{"a", "-o=x", "b", "-b"}, []string{"a", "b"}, "x", true, false},
		{[]string{"a", "-b", "--", "-x", "-o", "y"}, []string{"a", "-x", "-o", "y"}, "", true, false},
		{[]string{"-o", "--", "a"}, []string{"a"}, "--", false, false},
		{[]string{"-", "-o", "x"}, []string{"-"}, "x", false, false},
		{[]string{"-z", "a"}, nil, "", false, true},
		{[]string{"a", "-o"}, nil, "", false, true},
		{[]string{"-h"}, nil, "", false, true},
	}
	for _, c := range cases {
		fs := newFlags("test")
		fs.SetOutput(io.Discard)
		o := fs.String("o", "", "")
		b := fs.Bool("b", false, "")
		pos, err := parseMixed(fs, c.args)
		if (err != nil) != c.wantErr {
			t.Errorf("%q: err = %v, wantErr %v", c.args, err, c.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if !slices.Equal(pos, c.wantPos) || *o != c.wantO || *b != c.wantB {
			t.Errorf("%q: pos=%q o=%q b=%v, want %q %q %v", c.args, pos, *o, *b, c.wantPos, c.wantO, c.wantB)
		}
	}
}

func TestWriteOutput(t *testing.T) {
	out, _ := capture(t, func() {
		if rc := writeOutput("x", "", []byte("hi\n")); rc != 0 {
			t.Errorf("stdout write rc = %d", rc)
		}
	})
	if out != "hi\n" {
		t.Errorf("stdout got %q", out)
	}
	path := filepath.Join(t.TempDir(), "o.txt")
	if rc := writeOutput("x", path, []byte("file\n")); rc != 0 {
		t.Errorf("file write rc = %d", rc)
	}
	if b, _ := os.ReadFile(path); string(b) != "file\n" {
		t.Errorf("file got %q", b)
	}
	_, errOut := capture(t, func() {
		if rc := writeOutput("x", filepath.Join(t.TempDir(), "no", "dir", "f"), nil); rc != 1 {
			t.Errorf("bad path rc = %d, want 1", rc)
		}
	})
	if !strings.HasPrefix(errOut, "pica x: write: ") {
		t.Errorf("bad path stderr %q, want the pica x: write: prefix", errOut)
	}
}

// writeFiles writes name->content into a temp dir and returns it.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRenderCmd(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"t.tmpl":    "{{.title}}",
		"d.json":    `{"title":"from json"}`,
		"d.fact":    "title: str = \"from fact\"\n",
		"-x":        `{"title":"dash file"}`,
		"arc.fact":  "-- data.fact --\ntitle: str = \"from txtar\"\n",
		"loose.txt": "title: str = \"from fact by flag\"\n",
	})
	in := func(name string) string { return filepath.Join(dir, name) }
	run := func(args ...string) (int, string, string) {
		var rc int
		out, errOut := capture(t, func() { rc = renderCmd(args) })
		return rc, out, errOut
	}

	// JSON by default; the document gains a final newline.
	if rc, out, _ := run(in("t.tmpl"), in("d.json")); rc != 0 || out != "from json\n" {
		t.Errorf("json: rc=%d out=%q", rc, out)
	}
	// -h is a served request (exit 0), not a usage error (exit 2).
	if rc, _, errOut := run("-h"); rc != 0 || !strings.Contains(errOut, "-txtar") {
		t.Errorf("-h: rc=%d stderr=%q, want 0 and the flag usage", rc, errOut)
	}
	// A .fact suffix implies FACT; -fact forces it for other names.
	if rc, out, _ := run(in("t.tmpl"), in("d.fact")); rc != 0 || out != "from fact\n" {
		t.Errorf("fact suffix: rc=%d out=%q", rc, out)
	}
	if rc, out, _ := run(in("t.tmpl"), "-fact", in("loose.txt")); rc != 0 || out != "from fact by flag\n" {
		t.Errorf("-fact flag: rc=%d out=%q", rc, out)
	}
	// -txtar wins over the .fact suffix.
	if rc, out, _ := run("-txtar", in("t.tmpl"), in("arc.fact")); rc != 0 || out != "from txtar\n" {
		t.Errorf("-txtar over suffix: rc=%d out=%q", rc, out)
	}
	// Flags after the positionals; -o writes a file, stdout stays quiet.
	o := filepath.Join(dir, "out.t")
	if rc, out, _ := run(in("t.tmpl"), in("d.json"), "-o", o); rc != 0 || out != "" {
		t.Errorf("-o: rc=%d stdout=%q", rc, out)
	} else if b, _ := os.ReadFile(o); string(b) != "from json\n" {
		t.Errorf("-o file got %q", b)
	}
	// "--" lets a data file named "-x" through.
	if rc, out, _ := run(in("t.tmpl"), "--", in("-x")); rc != 0 || out != "dash file\n" {
		t.Errorf("-- -x: rc=%d out=%q", rc, out)
	}
	// Usage errors exit 2 under the subcommand's prefix; input
	// errors exit 1.
	if rc, _, errOut := run("-bogus", in("t.tmpl"), in("d.json")); rc != 2 || !strings.Contains(errOut, "-bogus") {
		t.Errorf("bad flag: rc=%d stderr=%q, want 2 and the flag named", rc, errOut)
	}
	if rc, _, errOut := run(in("t.tmpl")); rc != 2 || !strings.HasPrefix(errOut, "pica render: ") {
		t.Errorf("missing data: rc=%d stderr=%q", rc, errOut)
	}
	if rc, _, errOut := run(in("t.tmpl"), in("missing.json")); rc != 1 || !strings.HasPrefix(errOut, "pica render: read data: ") {
		t.Errorf("missing file: rc=%d stderr=%q", rc, errOut)
	}
}

func TestTextCmd_StdinAndPrefix(t *testing.T) {
	stdin = strings.NewReader("T\n\nprose\n")
	defer func() { stdin = os.Stdin }()
	var rc int
	out, _ := capture(t, func() { rc = textCmd(nil) })
	if rc != 0 || !strings.HasPrefix(out, "T\n") {
		t.Errorf("text from stdin: rc=%d out=%q", rc, out)
	}
	var rc2 int
	_, errOut := capture(t, func() { rc2 = textCmd([]string{"a", "b"}) })
	if rc2 != 2 || !strings.HasPrefix(errOut, "pica text: ") {
		t.Errorf("two inputs: rc=%d stderr=%q", rc2, errOut)
	}
}
