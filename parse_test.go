package typeset

import (
	"errors"
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Doc {
	t.Helper()
	d, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

func TestParse_TitleAndDefaults(t *testing.T) {
	d := mustParse(t, "\nWeather Limassol\n\nSome prose here.\n")
	if d.Title != "Weather Limassol" {
		t.Errorf("Title = %q", d.Title)
	}
	if d.Layout != DefaultLayout() {
		t.Errorf("Layout = %+v", d.Layout)
	}
	if len(d.Blocks) != 1 || d.Blocks[0].Kind != Para {
		t.Fatalf("Blocks = %+v", d.Blocks)
	}
}

func TestParse_BlockKinds(t *testing.T) {
	src := `Title

# Forecast

Prose line one
continues here.

---

.pre
Temp   26.4 C
Wind   WNW 12
.end

.pre 1
LCRA 100550Z 29012KT
more raw
.end

.table 3L 4R
Day | Hi
Mon | 31
.end

.link https://example.com
`
	d := mustParse(t, src)
	kinds := []BlockKind{}
	for _, b := range d.Blocks {
		kinds = append(kinds, b.Kind)
	}
	want := []BlockKind{Heading, Para, RuleBlk, Pre, Pre, TableBlk, LinkBlk}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}

	if d.Blocks[0].Text != "Forecast" {
		t.Errorf("heading text = %q", d.Blocks[0].Text)
	}
	if d.Blocks[1].Text != "Prose line one continues here." {
		t.Errorf("para text = %q", d.Blocks[1].Text)
	}
	if got := d.Blocks[3].Lines; len(got) != 2 || !strings.Contains(got[0], "Temp") {
		t.Errorf("pre lines = %v", got)
	}
	if d.Blocks[4].Repeat != 1 || len(d.Blocks[4].Lines) != 2 {
		t.Errorf("pre block = %+v", d.Blocks[4])
	}
	if d.Blocks[6].Text != "https://example.com" {
		t.Errorf("link = %q", d.Blocks[6].Text)
	}
}

func TestParse_LayoutTrailer(t *testing.T) {
	src := "T\n\nbody\n\n.width 32\n\n.paper a5\n.cols 2\n"
	d := mustParse(t, src)
	if d.Layout != (Layout{Width: 32, Paper: "a5", Cols: 2}) {
		t.Errorf("Layout = %+v", d.Layout)
	}

	// Content (or wire commands) after a layout command is an error.
	for _, bad := range []string{
		"T\n\n.width 32\n\nprose\n",
		"T\n\n.width 32\n.link https://x\n",
	} {
		if _, err := Parse(bad); !errors.Is(err, ErrContentAfterTrail) {
			t.Errorf("Parse(%q) err = %v, want ErrContentAfterTrail", bad, err)
		}
	}

	if _, err := Parse("T\n\n.width 32\n.width 40\n"); !errors.Is(err, ErrDuplicateAttr) {
		t.Errorf("duplicate attr err = %v", err)
	}
	for _, bad := range []string{".width 5", ".width x", ".paper b5", ".cols 0", ".cols 9"} {
		if _, err := Parse("T\n\n" + bad + "\n"); !errors.Is(err, ErrBadAttr) {
			t.Errorf("Parse(%q) err = %v, want ErrBadAttr", bad, err)
		}
	}
}

func TestParse_ClosedVocabulary(t *testing.T) {
	if _, err := Parse("T\n\n.witdh 40\n"); !errors.Is(err, ErrUnknownCommand) {
		t.Errorf("typo err = %v, want ErrUnknownCommand", err)
	}
	// Wire text escapes are not commands.
	d := mustParse(t, "T\n\n. leading dot space is text with more words\n\n..dots\n")
	if d.Blocks[0].Kind != Para || d.Blocks[1].Kind != Para {
		t.Errorf("escaped dot lines misparsed: %+v", d.Blocks)
	}
}

func TestParse_BlockErrors(t *testing.T) {
	if _, err := Parse("T\n\n.end\n"); !errors.Is(err, ErrStrayEnd) {
		t.Errorf("stray end err = %v", err)
	}
	if _, err := Parse("T\n\n.pre\nline\n"); !errors.Is(err, ErrUnterminatedBlock) {
		t.Errorf("unterminated err = %v", err)
	}
	if _, err := Parse("T\n\n.table nope\nA\n.end\n"); err == nil {
		t.Error("bad table spec accepted")
	}
	if _, err := Parse("   \n\n  \n"); !errors.Is(err, ErrEmptyDoc) {
		t.Error("empty doc accepted")
	}
}

func TestParse_TightRuns(t *testing.T) {
	// A prose label directly above a .pre block: contiguous in
	// source, so contiguous in output.
	d := mustParse(t, "T\n\nLabel line:\n.pre\nKey   Value\n.end\n\nNext para.\n")
	if len(d.Blocks) != 3 {
		t.Fatalf("blocks = %+v", d.Blocks)
	}
	if d.Blocks[0].Tight || !d.Blocks[1].Tight || d.Blocks[2].Tight {
		t.Errorf("tight flags = %v %v %v", d.Blocks[0].Tight, d.Blocks[1].Tight, d.Blocks[2].Tight)
	}
}

func TestParse_NoStructureFromSpacing(t *testing.T) {
	// The parser never infers structure from spacing: an aligned-
	// looking line without .pre is prose and gets filled like any
	// other, including double spaces after sentences.
	d := mustParse(t, "T\n\nKey   Value\nend of sentence.  Next one.\n")
	if len(d.Blocks) != 1 || d.Blocks[0].Kind != Para {
		t.Fatalf("blocks = %+v, want one Para", d.Blocks)
	}
	if d.Blocks[0].Text != "Key   Value end of sentence.  Next one." {
		t.Errorf("para text = %q", d.Blocks[0].Text)
	}
}

func TestText_EndToEnd(t *testing.T) {
	src := `Weather Limassol

# Forecast

Clear skies expected throughout the morning with temperatures rising to twenty six degrees by midday across the district.

.table 3L *L 4R
Day | Conditions | Temp
Mon | Sunny with a fresh westerly breeze | 25
.end

.pre
Temp   26.4 C
LCRA 100550Z 29012KT 9999 FEW020 SCT250 27/19 Q1008 NOSIG
.end

---

.link https://example.com/a/very/long/url/that/would/not/fit

.width 32
`
	d := mustParse(t, src)
	out, err := d.Text()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	if lines[0] != "Weather Limassol" {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.Contains(out, "# Forecast") {
		t.Error("heading marker lost")
	}
	if !strings.Contains(out, "---") {
		t.Error("rule lost")
	}
	if !strings.Contains(out, ".link https://example.com") {
		t.Error("link line lost")
	}
	if strings.Contains(out, ".width") || strings.Contains(out, ".pre") || strings.Contains(out, ".end") {
		t.Errorf("consumed commands leaked into output:\n%s", out)
	}
	// The table wrapped its long cell rather than truncating.
	if !strings.Contains(out, "breeze") {
		t.Errorf("table cell content lost:\n%s", out)
	}
	// Every displayed line fits .width 32 -- including the truncated
	// METAR. Only .link metadata (hidden by clients) is exempt.
	for _, ln := range lines {
		if strings.HasPrefix(ln, ".link ") {
			continue
		}
		if len([]rune(ln)) > 32 {
			t.Errorf("line exceeds width 32: %q", ln)
		}
	}
}

func TestText_TightPreservesAdjacency(t *testing.T) {
	d := mustParse(t, "T\n\nLabel:\n.pre\nKey   Value\n.end\n")
	out, err := d.Text()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Label:\nKey   Value") {
		t.Errorf("tight blocks separated:\n%s", out)
	}
}



func TestParse_Link(t *testing.T) {
	d := mustParse(t, "T\n\n.link https://x.example\n.link https://x.example News\n")
	if len(d.Blocks) != 2 {
		t.Fatalf("blocks = %+v", d.Blocks)
	}
	if d.Blocks[0].Text != "https://x.example" {
		t.Errorf("untitled link = %q", d.Blocks[0].Text)
	}
	if d.Blocks[1].Text != "https://x.example News" {
		t.Errorf("titled link = %q", d.Blocks[1].Text)
	}

	// URL required; title is a single word.
	if _, err := Parse("T\n\n.link\n"); !errors.Is(err, ErrBadAttr) {
		t.Errorf("bare .link: err = %v, want ErrBadAttr", err)
	}
	if _, err := Parse("T\n\n.link https://x.example Two Words\n"); !errors.Is(err, ErrBadAttr) {
		t.Errorf("multi-word title: err = %v, want ErrBadAttr", err)
	}
}

func TestParse_TableHeaderless(t *testing.T) {
	d := mustParse(t, "T\n\n.table - 6L 3C *R\nAPOEL | 2-1 | AEL\nAEK | 0-0 | Omonoia\n.end\n")
	out, err := d.Text()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "---") {
		t.Errorf("headerless table grew a separator:\n%s", out)
	}
	if !strings.Contains(out, "APOEL") || !strings.Contains(out, "Omonoia") {
		t.Errorf("rows lost:\n%s", out)
	}
	// Width variant: ".table 30 - SPEC".
	d2 := mustParse(t, "T\n\n.table 30 - 6L *R\nA | 1\n.end\n")
	if d2.Blocks[0].Width != 30 {
		t.Errorf("fixed width lost with headerless marker")
	}
}
