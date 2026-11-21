package lz4s

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

// The fixture is the Go implementation's streams for the benchmark
// inputs, plus the known answers; js/lz4s.js must decode every one to
// the same bytes.
type fixture struct {
	Pages []fixturePage  `json:"pages"`
	Known []fixtureKnown `json:"known"`
}

type fixturePage struct {
	Name string `json:"name"`
	Page string `json:"page"` // hex
	Comp string `json:"comp"` // hex
}

type fixtureKnown struct {
	Src  string `json:"src"`
	Comp string `json:"comp"`
}

const fixturePath = "js/fixture.json"

func buildFixture(t *testing.T) fixture {
	t.Helper()
	var f fixture
	in := benchInputs()
	for _, n := range []string{"random", "text"} {
		f.Pages = append(f.Pages, fixturePage{n, hex.EncodeToString(in[n]), hex.EncodeToString(Compress(in[n]))})
	}
	for _, src := range []string{"abc", "abcabc", "abcdefg", "abcdefgh", strings.Repeat("x", 300), "the quick brown fox jumps over the lazy dog the quick brown fox"} {
		f.Known = append(f.Known, fixtureKnown{hex.EncodeToString([]byte(src)), hex.EncodeToString(Compress([]byte(src)))})
	}
	return f
}

func TestFixture(t *testing.T) {
	want, err := json.MarshalIndent(buildFixture(t), "", " ")
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
	cmd := exec.Command(node, "--test", "lz4s_test.js")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "# fail 0") {
		t.Fatalf("node --test: %v\n%s", err, out)
	}
	if s := JS(); !strings.Contains(s, "export function decompress(") {
		t.Fatal("JS() is not the decoder")
	}
}
