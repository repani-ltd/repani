package raster

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite js/fixture.json from the Go implementation")

// The fixture is the Go implementation's answer for a set of rasters:
// bytes in, and the cell table, text rows, HTML rows and links out,
// plus one stream folded onto another. A second implementation of
// the spec (js/raster.js) must agree.
type fixture struct {
	Table   []string        `json:"table"` // CellRune of 0x00..0xFF
	Rasters []fixtureRaster `json:"rasters"`
	Stream  fixtureStream   `json:"stream"`
}

type fixtureRaster struct {
	Name  string   `json:"name"`
	Bytes string   `json:"bytes"` // hex, the canonical records
	Text  []string `json:"text"`  // per row
	HTML  []string `json:"html"`
	Links [][]Link `json:"links"` // per row
}

type fixtureStream struct {
	First  string   `json:"first"`  // hex
	Second string   `json:"second"` // hex, applied after
	Text   []string `json:"text"`   // the result
}

var fixtureSources = []struct{ name, src string }{
	{"plain", "plain text\n  indented\n"},
	{"ink", ".fg red\nALERT\n.fg default\n+ north quay closed\n.fg white\n.bg blue\nX\n.fg default\n.bg default\n.at 2\nAB\n.fg cyan\n+ CD\n.fg white\n.bg blue\n+ EF\n.fg red\n" + strings.Repeat("x", 40) + "\n"},
	{"fills", ".bg blue\n.fill 0\n.fg white\n.at 0\nTITLE\n.fg default\n.bg default\n.bg red\n.fill 2 10 2 8\n.bg green\n.fill 4 0 1 40\n.bg red\n.fg yellow\n.at 2 13\nQ\n"},
	{"links", "Tap [close] or [tide tables].\n[] [x\n.fg red\n[ALERT] now\n.fg default\nno]link[\n"},
	{"repertoire", "─│ ←↑→↓ ░▒▓█ °±×÷•·\n€£ ☀☁☂☾❄↯⚠ ‘’“”–— ☺☹♥★✓✗ ●○\nαβγδεζηθικλμνξοπρςστυφχψω\nάέήίόύώϊϋΐΰ\nΑΒΓΔΕΖΗΘΙΚΛΜΝΞΟΠΡΣΤΥΦΧΨΩ\n«…» ― <&>\"'\n"},
	{"sparse", ".at 3\nthree\n.at 100 35\nfar\n.at 1023\nlast\n"},
	{"aliases", ".def bar\n.fg white\n.bg blue\n.fill\n$bar\n.enddef\n.def t\n$t\n.enddef\n.bar TITLE\n.t .fg red\n.fg green\n.t + text\nstill green\n"},
	{"blank", ""},
}

func buildFixture(t *testing.T) fixture {
	t.Helper()
	var f fixture
	for b := range 256 {
		f.Table = append(f.Table, string(CellRune(byte(b))))
	}
	for _, s := range fixtureSources {
		r, err := Compile(s.src)
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		fr := fixtureRaster{Name: s.name, Bytes: hex.EncodeToString(r.Bytes()), Text: r.Text(), HTML: r.HTMLRows(), Links: [][]Link{}}
		for row := range r.Height() {
			l := r.Links(row)
			if l == nil {
				l = []Link{}
			}
			fr.Links = append(fr.Links, l)
		}
		f.Rasters = append(f.Rasters, fr)
	}
	first, _ := Compile("one\ntwo\nthree\n")
	second, _ := Compile(".at 1\nTWO\n")
	// A cleared row is a record of length 0, which Compile never emits
	// for a blank row: append one by hand for row 2.
	stream := append(second.Bytes(), 2<<6, 0)
	folded, err := Read(append(append([]byte{}, first.Bytes()...), stream...))
	if err != nil {
		t.Fatal(err)
	}
	f.Stream = fixtureStream{First: hex.EncodeToString(first.Bytes()), Second: hex.EncodeToString(stream), Text: folded.Text()}
	return f
}

const fixturePath = "js/fixture.json"

func TestFixture(t *testing.T) {
	f := buildFixture(t)
	want, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if *update {
		if err := os.WriteFile(fixturePath, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	have, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("%v (run: go test -run TestFixture -update)", err)
	}
	if string(have) != string(want) {
		t.Fatal("js/fixture.json is stale: go test -run TestFixture -update")
	}
}

// TestJS runs the JavaScript reader's test against the fixture when
// node is installed, and skips otherwise.
func TestJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	dir, _ := filepath.Abs("js")
	cmd := exec.Command(node, "--test", "raster_test.js")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "# fail 0") {
		t.Fatalf("node --test:\n%s", out)
	}
}
