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

// The fixture is the Go implementation's answer for a set of pages:
// bytes in, and the cell table, text rows, HTML rows and links out.
// A second implementation of the spec (js/raster.js) must agree.
type fixture struct {
	Table []string      `json:"table"` // CellRune of 0x00..0xFF
	Pages []fixturePage `json:"pages"`
}

type fixturePage struct {
	Name   string     `json:"name"`
	Cols   int        `json:"cols"`
	Rows   int        `json:"rows"`
	Panels int        `json:"panels"`
	Bytes  string     `json:"bytes"` // hex
	Text   [][]string `json:"text"`  // per panel, per row
	HTML   [][]string `json:"html"`
	Links  [][][]Link `json:"links"` // per panel, per row
}

var fixtureSources = []struct {
	name string
	g    Geometry
	src  string
}{
	{"plain", Geometry{40, 3, 1}, "plain text\n  indented\n"},
	{"tail and gaps", Geometry{40, 4, 1}, ".fg red\nALERT\n.fg default\n+ north quay closed\n.fg white\n.bg blue\nX\n.fg default\n.bg default\n.at 2\nAB\n.fg cyan\n+ CD\n.fg white\n.bg blue\n+  EF\n"},
	{"fills", Geometry{40, 6, 1}, ".bg blue\n.fill 0\n.fg white\n.at 0 2\nTITLE\n.fg default\n.bg default\n.bg red\n.fill 2 10 2 8\n.bg green\n.fill 4 0 1 39\n.bg red\n.fg yellow\n.at 2 13\nQ\n"},
	{"links", Geometry{40, 4, 1}, "Tap [close] or [tide tables].\n[] [x\n.fg red\n[ALERT] now\n.fg default\nno]link[\n"},
	{"repertoire", Geometry{40, 8, 1}, "─│ ←↑→↓ ░▒▓█ °±×÷•·\n€£ ☀☁☂☾❄↯⚠ ‘’“”–— ☺☹♥★✓✗ ●○\nαβγδεζηθικλμνξοπρςστυφχψω\nάέήίόύώϊϋΐΰ\nΑΒΓΔΕΖΗΘΙΚΛΜΝΞΟΠΡΣΤΥΦΧΨΩ\n«…» ― <&>\"'\n"},
	{"panels and margin", Geometry{20, 3, 2}, ".margin 2\n.fg green\nGO\n.panel 1\nstill green\n.at 2 5\nfar\n"},
}

func buildFixture(t *testing.T) fixture {
	t.Helper()
	var f fixture
	for b := range 256 {
		f.Table = append(f.Table, string(CellRune(byte(b))))
	}
	for _, s := range fixtureSources {
		p, err := Compile(s.g, s.src)
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		c := Decode(p)
		page := fixturePage{Name: s.name, Cols: s.g.Cols, Rows: s.g.Rows, Panels: s.g.Panels, Bytes: hex.EncodeToString(p.Cells)}
		for panel := range s.g.Panels {
			page.Text = append(page.Text, c.Text(panel))
			page.HTML = append(page.HTML, c.HTMLRows(panel))
			var rows [][]Link
			for row := range s.g.Rows {
				l := c.Links(panel, row)
				if l == nil {
					l = []Link{}
				}
				rows = append(rows, l)
			}
			page.Links = append(page.Links, rows)
		}
		f.Pages = append(f.Pages, page)
	}
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

// TestJS runs the JavaScript decoder's test against the fixture when
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
