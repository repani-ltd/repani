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
	if d.Layout != (Layout{Width: 32, Paper: "a5", Cols: 2, Font: "mono"}) {
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
	for _, bad := range []string{".width 5", ".width x", ".paper b5", ".cols 0", ".cols 9", ".font serif"} {
		if _, err := Parse("T\n\n" + bad + "\n"); !errors.Is(err, ErrBadAttr) {
			t.Errorf("Parse(%q) err = %v, want ErrBadAttr", bad, err)
		}
	}

	if d := mustParse(t, "T\n\nbody\n\n.font sans\n"); d.Layout.Font != "sans" {
		t.Errorf("Layout.Font = %q, want sans", d.Layout.Font)
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
	d := mustParse(t, "T\n\n.link https://x.example\n"+
		".link https://x.example News\n"+
		".link https://x.example The morning news, archived\n"+
		".link https://a.example https://b.example\n")
	if len(d.Blocks) != 4 {
		t.Fatalf("blocks = %+v", d.Blocks)
	}
	if d.Blocks[0].Text != "https://x.example" {
		t.Errorf("untitled link = %q", d.Blocks[0].Text)
	}
	if d.Blocks[1].Text != "https://x.example News" {
		t.Errorf("titled link = %q", d.Blocks[1].Text)
	}
	if d.Blocks[2].Text != "https://x.example The morning news, archived" {
		t.Errorf("phrase-titled link = %q", d.Blocks[2].Text)
	}
	// First field wins: a URL-shaped title is still just a title.
	if d.Blocks[3].Text != "https://a.example https://b.example" {
		t.Errorf("url-shaped title = %q", d.Blocks[3].Text)
	}

	// The URL is required.
	if _, err := Parse("T\n\n.link\n"); !errors.Is(err, ErrBadAttr) {
		t.Errorf("bare .link: err = %v, want ErrBadAttr", err)
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
	// Subsections: "## " is Heading level 2; a third level is a
	// loud error, never silent structure.
	dh := mustParse(t, "T\n\n# Section\n\n## Subsection\n\nprose\n")
	if dh.Blocks[0].Level != 1 || dh.Blocks[1].Level != 2 {
		t.Errorf("heading levels = %d, %d; want 1, 2", dh.Blocks[0].Level, dh.Blocks[1].Level)
	}
	if dh.Blocks[1].Text != "Subsection" {
		t.Errorf("subsection text = %q", dh.Blocks[1].Text)
	}
	if _, err := Parse("T\n\n### Too deep\n"); err == nil {
		t.Error("expected error for ### heading")
	}

	// Note rows: ".." annotates the row above; before any data row
	// it annotates the header, and it never becomes the header.
	dn := mustParse(t, "T\n\n.table 6L 5N\nClient | Amt\n.. | eur\nAlpha | 12.50\n.. broker |\n.end\n")
	tln, err := dn.Blocks[0].Table.Layout(12)
	if err != nil {
		t.Fatal(err)
	}
	if len(tln.HeaderNotes) != 1 || len(tln.Rows) != 1 || len(tln.RowNotes[0]) != 1 {
		t.Errorf("note layout = %+v", tln)
	}

	// Width variant: ".table 30 - SPEC".
	d2 := mustParse(t, "T\n\n.table 30 - 6L *R\nA | 1\n.end\n")
	if d2.Blocks[0].Width != 30 {
		t.Errorf("fixed width lost with headerless marker")
	}
}

func TestTableFixedWidthValidated(t *testing.T) {
	for _, bad := range []string{".table 0 3L 4R", ".table -5 3L 4R"} {
		src := "T\n\n" + bad + "\nh | h\n.end\n"
		if _, err := Parse(src); !errors.Is(err, ErrBadAttr) {
			t.Errorf("Parse(%q) err = %v, want ErrBadAttr", bad, err)
		}
	}
}

func TestQuoteBlock(t *testing.T) {
	d := mustParse(t, "T\n\n.quote\nWisdom of the\nancients endures.\n.attrib Aesop\n.end\n")
	b := d.Blocks[0]
	if b.Kind != Quote || b.Text != "Wisdom of the ancients endures." || b.Attrib != "Aesop" {
		t.Fatalf("quote block = %+v", b)
	}

	for _, bad := range []string{
		".quote\n.end",                        // empty
		".quote\ntext\n.attrib A\nmore\n.end", // content after .attrib
		".quote\ntext\n.attrib\n.end",         // .attrib without text
		".quote\n.attrib A\n.end",             // attribution only
		".attrib Aesop",                       // outside .quote
	} {
		if _, err := Parse("T\n\n" + bad + "\n"); !errors.Is(err, ErrBadAttr) {
			t.Errorf("Parse(%q) err = %v, want ErrBadAttr", bad, err)
		}
	}
	if _, err := Parse("T\n\n.quote\ntext\n"); !errors.Is(err, ErrUnterminatedBlock) {
		t.Errorf("unterminated .quote err = %v, want ErrUnterminatedBlock", err)
	}
}

func TestItemBlock(t *testing.T) {
	d := mustParse(t, "T\n\n.item first thing\n.item second thing\n")
	if len(d.Blocks) != 2 || d.Blocks[0].Kind != Item || d.Blocks[1].Kind != Item {
		t.Fatalf("blocks = %+v", d.Blocks)
	}
	if d.Blocks[0].Tight || !d.Blocks[1].Tight {
		t.Error("consecutive items should be tight after the first")
	}
	if _, err := Parse("T\n\n.item\n"); !errors.Is(err, ErrBadAttr) {
		t.Errorf("bare .item err = %v, want ErrBadAttr", err)
	}
}

func TestRemInvisible(t *testing.T) {
	// A comment neither ends a paragraph nor appears in output, and
	// is valid after the layout trailer.
	d := mustParse(t, "T\n\nalpha beta\n.rem hidden note\ngamma delta\n\n.width 20\n.rem trailing comment\n")
	if len(d.Blocks) != 1 || d.Blocks[0].Text != "alpha beta gamma delta" {
		t.Fatalf("comment split the paragraph: %+v", d.Blocks)
	}
	out, err := d.Text()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "hidden") || strings.Contains(out, "trailing") {
		t.Errorf("comment leaked into output:\n%s", out)
	}
}

func TestByDate(t *testing.T) {
	d := mustParse(t, "T\n\n.by Pavlos\n.date 24 July 2026\n\nprose here\n")
	if got := d.Byline(); got != "by Pavlos -- 24 July 2026" {
		t.Errorf("Byline() = %q", got)
	}
	if d := mustParse(t, "T\n\n.date 24 July 2026\n"); d.Byline() != "24 July 2026" {
		t.Errorf("date-only Byline() = %q", d.Byline())
	}

	if _, err := Parse("T\n\n.by A\n.by B\n"); !errors.Is(err, ErrDuplicateAttr) {
		t.Errorf("duplicate .by err = %v, want ErrDuplicateAttr", err)
	}
	if _, err := Parse("T\n\nprose\n\n.by A\n"); !errors.Is(err, ErrMetaAfterContent) {
		t.Errorf(".by after content err = %v, want ErrMetaAfterContent", err)
	}
	if _, err := Parse("T\n\n.by\n"); !errors.Is(err, ErrBadAttr) {
		t.Errorf("bare .by err = %v, want ErrBadAttr", err)
	}
	if _, err := Parse("T\n\n.width 20\n.by A\n"); !errors.Is(err, ErrContentAfterTrail) {
		t.Errorf(".by in trailer err = %v, want ErrContentAfterTrail", err)
	}
}

func TestLangRemoved(t *testing.T) {
	// .lang was removed (DESIGN.md: admitted for a hypothetical
	// future, failing the vocabulary gate's demand test). The
	// closed vocabulary makes the removal loud, never silent.
	if _, err := Parse("T\n\n.lang el\n"); err == nil {
		t.Error("expected .lang to be an unknown-command error")
	}
}

func TestTextQuoteItemByline(t *testing.T) {
	// Explicit .width 40: the content is sized to wrap at that
	// measure, independent of the language default.
	src := "T\n\n.by A. Writer\n.date Today\n\n" +
		".quote\nThe quick brown fox jumps over the lazy dog again and again and again.\n.attrib Aesop\n.end\n\n" +
		".item first item that runs long enough to wrap onto a second line for sure\n.item second\n\n.width 40\n"
	d := mustParse(t, src)
	out, err := d.Text()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if lines[1] != "by A. Writer -- Today" {
		t.Errorf("byline = %q", lines[1])
	}

	var quote, attrib, items []string
	for _, ln := range lines {
		switch {
		case strings.HasSuffix(ln, "-- Aesop"):
			attrib = append(attrib, ln)
		case strings.HasPrefix(ln, "  The") || strings.HasPrefix(ln, "  again"):
			quote = append(quote, ln)
		case strings.HasPrefix(ln, "• "):
			items = append(items, ln)
		}
	}
	if len(quote) == 0 {
		t.Errorf("no indented quote lines:\n%s", out)
	}
	for _, ln := range quote {
		if runeLen(ln) > 40-quoteIndent {
			t.Errorf("quote line exceeds inset measure: %q", ln)
		}
	}
	if len(attrib) != 1 || runeLen(attrib[0]) != 40-quoteIndent {
		t.Errorf("attrib not right-aligned to width-%d: %q", quoteIndent, attrib)
	}
	if len(items) != 2 {
		t.Errorf("item lines = %q, want 2 bulleted", items)
	}
	// Continuation of the first item hangs under the bullet.
	found := false
	for i, ln := range lines {
		if strings.HasPrefix(ln, "• first") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "  ") {
			found = true
		}
	}
	if !found {
		t.Errorf("no hanging continuation line:\n%s", out)
	}
}
