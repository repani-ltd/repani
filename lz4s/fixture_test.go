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

// The fixture is the Go implementation's streams for the corpus pages
// and their deltas, plus the known answers; js/lz4s.js must decode
// every one to the same bytes.
type fixture struct {
	Pages  []fixturePage  `json:"pages"`
	Deltas []fixtureDelta `json:"deltas"`
	Known  []fixtureKnown `json:"known"`
}

type fixturePage struct {
	Name string `json:"name"`
	Page string `json:"page"` // hex
	Comp string `json:"comp"` // hex
}

type fixtureDelta struct {
	Base  string `json:"base"`
	Src   string `json:"src"`
	Delta string `json:"delta"` // hex
}

type fixtureKnown struct {
	Src  string `json:"src"`
	Comp string `json:"comp"`
}

const fixturePath = "js/fixture.json"

func buildFixture(t *testing.T) fixture {
	t.Helper()
	var f fixture
	names, _ := filepath.Glob("testdata/*.bin")
	for _, n := range names {
		page, _ := os.ReadFile(n)
		f.Pages = append(f.Pages, fixturePage{strings.TrimSuffix(filepath.Base(n), ".bin"), hex.EncodeToString(page), hex.EncodeToString(Compress(page))})
	}
	for _, pair := range [][2]string{{"qam-report", "qam-report2"}, {"tess-harbour", "tess-harbour2"}} {
		base, _ := os.ReadFile("testdata/" + pair[0] + ".bin")
		src, _ := os.ReadFile("testdata/" + pair[1] + ".bin")
		f.Deltas = append(f.Deltas, fixtureDelta{pair[0], pair[1], hex.EncodeToString(Delta(base, src))})
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
	if s := JS(); !strings.Contains(s, "export function undelta(") {
		t.Fatal("JS() is not the decoder")
	}
}
