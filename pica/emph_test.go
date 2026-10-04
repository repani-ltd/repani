package pica

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestEmphSegments(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []EmphSeg
	}{
		{"plain", "no markers here", []EmphSeg{{Text: "no markers here"}}},
		{"single word", "a _b_ c", []EmphSeg{{Text: "a "}, {Text: "b", Emph: true}, {Text: " c"}}},
		{"multi word", "_two words_ end", []EmphSeg{{Text: "two words", Emph: true}, {Text: " end"}}},
		{"whole text", "_all_", []EmphSeg{{Text: "all", Emph: true}}},
		{"trailing comma", "say _word_, then", []EmphSeg{{Text: "say "}, {Text: "word", Emph: true}, {Text: ", then"}}},
		{"parenthesized", "(_word_)", []EmphSeg{{Text: "("}, {Text: "word", Emph: true}, {Text: ")"}}},
		{"snake case", "a snake_case_name b", []EmphSeg{{Text: "a snake_case_name b"}}},
		{"joined name", "in repos/_attic now", []EmphSeg{{Text: "in repos/_attic now"}}},
		{"dotted name", "see pkg._foo here", []EmphSeg{{Text: "see pkg._foo here"}}},
		{"dash opener", "so--_word_ works", []EmphSeg{{Text: "so--"}, {Text: "word", Emph: true}, {Text: " works"}}},
		{"double underscore", "a __ b", []EmphSeg{{Text: "a __ b"}}},
		{"interior underscore in span", "_a_b_", []EmphSeg{{Text: "a_b", Emph: true}}},
		{"two spans", "_a_ and _b_", []EmphSeg{{Text: "a", Emph: true}, {Text: " and "}, {Text: "b", Emph: true}}},
		// An unclosed opener is literal here (Parse rejects it first).
		{"unclosed literal", "we renamed _foo today", []EmphSeg{{Text: "we renamed _foo today"}}},
	}
	for _, c := range cases {
		if got := EmphSegments(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: EmphSegments(%q) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestEmphUnclosed(t *testing.T) {
	if i := emphUnclosed("all _closed_ here"); i >= 0 {
		t.Errorf("closed text reported unclosed at %d", i)
	}
	if i := emphUnclosed("an _open span"); i != 3 {
		t.Errorf("unclosed opener index = %d, want 3", i)
	}
	// A second potential opener is not a closer: still unclosed.
	if i := emphUnclosed("we renamed _foo to _bar"); i < 0 {
		t.Error("two openers with no closer reported closed")
	}
}

func TestEmphLinesCarryAcrossLines(t *testing.T) {
	// A span opened on one wrapped line closes on the next: the
	// first line underlines from its marker to its end, a middle
	// line whole, the last from its start to its marker, and the
	// marker cells are blanked in place -- the grid never moves. A
	// marker-free line outside any span passes through untouched.
	clean, spans := EmphLines("say _two middle words_ end plain text",
		[]string{"say _two", "middle", "words_ end", "plain text"})
	wantClean := []string{"say  two", "middle", "words  end", "plain text"}
	wantSpans := [][]Span{{{Start: 4, End: 8}}, {{Start: 0, End: 6}}, {{Start: 0, End: 6}}, nil}
	if !reflect.DeepEqual(clean, wantClean) || !reflect.DeepEqual(spans, wantSpans) {
		t.Errorf("clean %q spans %v", clean, spans)
	}
}

func TestEmphLinesHyphenatedMarker(t *testing.T) {
	// The paragraph's gates, not the line's: an underscore before a
	// hyphen the breaker added is interior, not a closer, so
	// _snake_case_words_ is one span over three hyphenated lines.
	lines := []string{"_snake_-", "case_-", "words_", "end"}
	clean, spans := EmphLines("_snake_case_words_ end", lines)
	wantClean := []string{" snake_-", "case_-", "words ", "end"}
	wantSpans := [][]Span{{{Start: 0, End: 8}}, {{Start: 0, End: 6}}, {{Start: 0, End: 6}}, nil}
	if !reflect.DeepEqual(clean, wantClean) || !reflect.DeepEqual(spans, wantSpans) {
		t.Errorf("clean %q spans %v", clean, spans)
	}
}

func TestParseRejectsUnclosedEmphasis(t *testing.T) {
	for _, src := range []string{
		"Title\n\nan _open span never closes.\n",
		"Title\n\n.quote\nan _open span.\n.end\n",
		"Title\n\n.item an _open item\n",
	} {
		if _, err := Parse(src); !errors.Is(err, ErrUnclosedEmph) {
			t.Errorf("Parse(%q) err = %v, want ErrUnclosedEmph", src, err)
		}
	}
	// The error names the block's opening line.
	_, err := Parse("Title\n\nfine paragraph.\n\nan _open span.\n")
	if err == nil || !strings.Contains(err.Error(), "line 5") {
		t.Errorf("err = %v, want line 5", err)
	}
}

func TestEmphasisOnlyInProse(t *testing.T) {
	// Underscores outside flowing prose are characters: the same
	// would-be-unclosed pattern parses cleanly in a heading, a
	// table cell, and a .pre body.
	src := "Title\n\n# the _open heading\n\n.pre\nlit _open here\n.end\n\n.table 8L 8L\n^lit _open | b\n.end\n"
	if _, err := Parse(src); err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

func TestTextWriterKeepsUnderscores(t *testing.T) {
	// The text page is the typescript: emphasis markers pass
	// through verbatim, so the wire carries the convention.
	doc, err := Parse("Title\n\nsay _the word_ plainly.\n")
	if err != nil {
		t.Fatal(err)
	}
	out, err := doc.Text()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "_the word_") {
		t.Errorf("text output lost the markers:\n%s", out)
	}
}

func TestHTMLEmphasis(t *testing.T) {
	doc, err := Parse("Title\n\nsay _the word_ plainly.\n\n.item an _emphatic_ item\n\n.quote\na _quoted_ stress.\n.end\n\n# a _heading_ underscore\n")
	if err != nil {
		t.Fatal(err)
	}
	out := doc.HTML()
	for _, want := range []string{
		"<p>say <em>the word</em> plainly.</p>",
		"<li>an <em>emphatic</em> item</li>",
		"<p>a <em>quoted</em> stress.</p>",
		"<h2>a _heading_ underscore</h2>", // headings: literal
	} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML missing %q in:\n%s", want, out)
		}
	}
}

func TestJustifyLinesEmphFlags(t *testing.T) {
	// wide measurer stand-ins: emphasis face wider than body, so a
	// width mismatch between faces is visible in Line.Width.
	body, em := Mono, doubleMeasurer{}
	lines := JustifyLinesEmph("aa _bb_ cc", 20, body, em)
	if len(lines) != 1 {
		t.Fatalf("lines = %v", lines)
	}
	ln := lines[0]
	if want := []string{"aa", "bb", "cc"}; !reflect.DeepEqual(ln.Words, want) {
		t.Errorf("words = %v, want %v", ln.Words, want)
	}
	if want := []bool{false, true, false}; !reflect.DeepEqual(ln.Emph, want) {
		t.Errorf("emph = %v, want %v", ln.Emph, want)
	}
	// aa(2) + sp + bb(2x2 under em) + sp + cc(2) = 10
	if ln.Width != 10 {
		t.Errorf("width = %d, want 10 (emphasized token must be measured with the emphasis face)", ln.Width)
	}
	// No markers: identical to JustifyLines, and Emph stays nil.
	plain := JustifyLinesEmph("aa bb cc", 20, body, em)
	if plain[0].Emph != nil {
		t.Errorf("plain paragraph grew Emph flags: %v", plain[0].Emph)
	}
	if !reflect.DeepEqual(plain[0].Words, []string{"aa", "bb", "cc"}) || plain[0].Width != 8 {
		t.Errorf("plain line = %+v", plain[0])
	}
}

// doubleMeasurer doubles Mono's widths: a fake emphasis face whose
// metrics differ from the body's, so tests can see which face
// measured a token.
type doubleMeasurer struct{}

func (doubleMeasurer) Width(s string) int { return 2 * runeLen(s) }
func (doubleMeasurer) Space() int         { return 1 }

func TestJustifyLinesEmphHyphenKeepsFlag(t *testing.T) {
	// An emphasized word split at a hyphenation point keeps its
	// flag on both fragments.
	lines := JustifyLinesEmph("filler _hyphenation_ x", 10, Mono, Mono)
	var frags int
	for _, ln := range lines {
		for i, w := range ln.Words {
			if strings.Contains("hyphenation", strings.TrimSuffix(w, "-")) && len(w) > 2 {
				frags++
				if ln.Emph == nil || !ln.Emph[i] {
					t.Errorf("fragment %q lost its emphasis flag (line %+v)", w, ln)
				}
			}
		}
	}
	if frags < 2 {
		t.Fatalf("expected the word to hyphenate into fragments, got lines %v", lines)
	}
}
