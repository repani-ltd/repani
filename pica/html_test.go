package pica

import (
	"strings"
	"testing"
)

func TestHTML(t *testing.T) {
	src := strings.Join([]string{
		"The <Title>",
		".by A & B",
		".date 2026-08-20",
		".rights (c) Repani",
		"",
		"First para.",
		"",
		"# Section",
		"## Sub",
		"---",
		".item one",
		".item two",
		"",
		".pre",
		"  x < y",
		".end",
		".link https://repani.com Repani",
		".link https://example.org/a",
		".quote",
		"Said thing.",
		".attrib Who",
		".end",
		".table 20 10L 8N",
		"name | amount",
		".. | eur",
		"a | 1.50",
		"= total | 1.50",
		".end",
		".table - 8L 8L",
		"only | data",
		".end",
		".width 60",
	}, "\n") + "\n"
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	out := doc.HTML()
	for _, want := range []string{
		"<article>\n<h1>The &lt;Title&gt;</h1>",
		`<p class="byline">by A &amp; B -- 2026-08-20</p>`,
		"<p>First para.</p>",
		"<h2>Section</h2>", "<h3>Sub</h3>", "<hr>",
		"<ul>\n<li>one</li>\n<li>two</li>\n</ul>",
		"<pre>  x &lt; y</pre>",
		`<p class="link"><a href="https://repani.com">Repani</a></p>`,
		`<p class="link"><a href="https://example.org/a">https://example.org/a</a></p>`,
		"<blockquote>\n<p>Said thing.</p>\n<p class=\"attrib\">Who</p>\n</blockquote>",
		`<table style="max-width:20ch">`,
		"<thead>\n<tr><th>name</th><th style=\"text-align:right\">amount</th></tr>\n</thead>",
		`<tr class="note"><td></td><td style="text-align:right">eur</td></tr>`,
		`<tr><td>a</td><td style="text-align:right">1.50</td></tr>`,
		`<tr class="total"><td>total</td><td style="text-align:right">1.50</td></tr>`,
		"<footer>(c) Repani</footer>\n</article>\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Headerless table: no thead; no .width leak.
	i := strings.LastIndex(out, "<table>")
	if i < 0 || strings.Contains(out[i:], "<thead>") {
		t.Error("headerless table should have no thead")
	}
	if strings.Contains(out, "60") {
		t.Error("layout width leaked into output")
	}
}
