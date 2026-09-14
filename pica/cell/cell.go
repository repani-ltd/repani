package cell

import (
	_ "embed"
	"fmt"
	"strings"
	"unicode/utf8"

	"repani.com/pica"
	"repani.com/typeset/raster"
)

// Vocabulary is the default vocabulary (vocabulary.rt): the alias
// definitions Render prepends to the source it writes.
//
//go:embed vocabulary.rt
var Vocabulary string

// Layout is where a document lands: the screens, and how each is
// divided into columns.
type Layout struct {
	Rows    int // rows per screen; 0 means one screen of any height
	Screens int // screens at most; 0 means as many as the document needs
	Columns int // columns per screen; 0 means the document's .cols
	Gutter  int // blank cells between columns; 0 means 1
	Margin  int // blank cells before the first column
	// Head is the rows the masthead occupies on the first screen
	// before the columns begin; 0 means as many as it needs (the
	// title, the byline, a blank row, a rule, a blank row). More pads
	// with blank rows, so a screen can match a printed page whose
	// masthead is taller than its type.
	Head int
}

// Link is a .link block as the raster carries it: the bracketed text
// on the raster and the URL it stood for in the document.
type Link struct{ Text, URL string }

// Result is a rendered document: one raster per screen, each with
// the source it was compiled from (the vocabulary, then the screen),
// and the links.
type Result struct {
	Sources []string
	Rasters []*raster.Raster
	Links   []Link
}

// minWidth is the narrowest column the writer will set.
const minWidth = 8

// Render sets doc on the layout in the vocabulary (Vocabulary if
// empty). A document that does not fit in the screens is an error
// saying how many lines are left over, as is a rune outside the
// raster repertoire.
func Render(doc *pica.Doc, l Layout, vocabulary string) (*Result, error) {
	if vocabulary == "" {
		vocabulary = Vocabulary
	}
	n := l.Columns
	if n <= 0 {
		n = doc.Layout.Cols
	}
	if n <= 0 {
		n = 1
	}
	gutter := l.Gutter
	if gutter <= 0 {
		gutter = 1
	}
	margin := max(l.Margin, 0)
	usable := raster.Cols - margin - (n-1)*gutter
	colW := usable / n
	if colW < minWidth {
		return nil, fmt.Errorf("cell: %d columns of %d cells leave a measure of %d, under %d", n, raster.Cols, colW, minWidth)
	}

	var links []Link
	blocks, err := compose(doc, colW, &links)
	if err != nil {
		return nil, err
	}

	// The masthead on the first screen: the title and byline centered
	// over the columns, a blank row, a rule across them, a blank row.
	span := n*colW + (n-1)*gutter
	mast := 0
	if doc.Title != "" {
		mast = 4
		if doc.Byline() != "" {
			mast++
		}
	}
	if l.Head > mast {
		mast = l.Head
	}
	rows := l.Rows
	if rows <= 0 {
		rows = raster.MaxRows
	}
	capacity := func(col int) int {
		if col/n == 0 {
			return rows - mast
		}
		return rows
	}
	cols := flow(blocks, capacity)
	screens := (len(cols) + n - 1) / n
	if l.Rows <= 0 && screens > 1 || l.Screens > 0 && screens > l.Screens {
		limit := max(l.Screens, 1)
		left := 0
		for _, c := range cols[limit*n:] {
			left += len(c)
		}
		return nil, fmt.Errorf("cell: %d lines do not fit in %d screens", left, limit)
	}
	depth := make([]int, len(cols))
	for ci, col := range cols {
		depth[ci] = len(col)
	}

	res := &Result{Links: links}
	for s := range screens {
		var src strings.Builder
		say := func(format string, args ...any) { fmt.Fprintf(&src, format+"\n", args...) }
		top := 0
		if s == 0 {
			top = mast
		}
		if s == 0 && doc.Title != "" {
			// Centered over the columns.
			center := func(row int, text string) {
				text = pica.TruncLine(text, span)
				say(".at %d %d", row, margin+(span-utf8.RuneCountInString(text))/2)
			}
			row := 0
			center(row, doc.Title)
			say(".title %s", pica.TruncLine(doc.Title, span))
			row++
			if bl := doc.Byline(); bl != "" {
				center(row, bl)
				say(".byline %s", pica.TruncLine(bl, span))
				row++
			}
			row++
			say(".at %d %d", row, margin)
			say("%s", strings.Repeat("─", span))
		}
		for k := range n {
			ci := s*n + k
			if ci >= len(cols) {
				break
			}
			start := margin + k*(colW+gutter)
			row := top
			for _, ln := range cols[ci] {
				emit(say, ln, row, start)
				row++
			}
		}
		// Hairlines down the gutters, to content depth, when the gutter
		// has a blank cell on either side of the rule.
		if gutter >= 3 {
			for k := 1; k < n; k++ {
				left, right := s*n+k-1, s*n+k
				if left >= len(cols) {
					break
				}
				d := depth[left]
				if right < len(depth) && depth[right] > d {
					d = depth[right]
				}
				if d == 0 {
					continue
				}
				say(".at %d", top)
				rule := strings.Repeat(" ", margin+k*(colW+gutter)-gutter+gutter/2) + "│"
				for range d {
					say("%s", rule)
				}
			}
		}
		full := vocabulary + src.String()
		r, err := raster.Compile(full)
		if err != nil {
			return nil, err
		}
		res.Sources = append(res.Sources, full)
		res.Rasters = append(res.Rasters, r)
	}
	return res, nil
}

// A line to place: its text, the alias that paints the whole row
// ("" for default ink), and the spans overpainted in an alias.
type line struct {
	text  string
	alias string
	spans []span
}

type span struct {
	start, end int // rune interval of text
	alias      string
}

// emit writes one line at (row, start): a whole-row alias, or the
// text in default ink through the identity alias .t (so a line that
// begins like a command is painted as written) and then its spans.
// Raster carries no position from line to line, so every line is
// placed by its own .at.
func emit(say func(string, ...any), ln line, row, start int) {
	if ln.alias != "" {
		say(".at %d %d", row, start+leadSpaces(ln.text))
		say(".%s %s", ln.alias, strings.TrimSpace(ln.text))
		return
	}
	say(".at %d %d", row, start)
	say(".t %s", ln.text)
	for _, sp := range ln.spans {
		runes := []rune(ln.text)
		t := string(runes[sp.start:sp.end])
		lead := leadSpaces(t)
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		say(".col %d", start+sp.start+lead)
		say(".%s %s", sp.alias, t)
	}
}

func leadSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

// A block to flow: its segments, the lines that move together; the
// leading segments repeated after a split (a table's header, .pre
// N); whether it splits at all; whether it keeps with what follows
// (a heading); whether a blank line precedes it.
type block struct {
	segs     [][]line
	repeat   int
	atomic   bool
	keepNext bool
	tight    bool
}

func (b block) height() int {
	h := 0
	for _, s := range b.segs {
		h += len(s)
	}
	return h
}

// compose lays every block out at width w -- prose as the PDF's
// mono page sets it, tables, verbatim and headings as the text
// writer's -- and marks what the cell writer inks.
func compose(doc *pica.Doc, w int, links *[]Link) ([]block, error) {
	var out []block
	for _, b := range doc.Blocks {
		blk := block{tight: b.Tight}
		switch b.Kind {
		case pica.Heading:
			// One row at the full measure, in the role's ink; a heading
			// longer than the measure wraps. (Press folds a heading at
			// the larger glyph's measure on a slot two lines tall; the
			// grid has one glyph, and copying the fold made headings
			// that looked wrapped for no reason -- DESIGN.t §15.)
			alias := "heading"
			if b.Level == 2 {
				alias = "subheading"
			}
			for _, ln := range pica.WrapLines(b.Text, w, pica.Mono) {
				blk.segs = append(blk.segs, []line{{text: strings.Join(ln.Words, " "), alias: alias}})
			}
			blk.keepNext = true

		case pica.RuleBlk:
			blk.segs = [][]line{{{text: strings.Repeat("─", w)}}}

		case pica.LinkBlk:
			url, title, _ := strings.Cut(b.Text, " ")
			if title == "" {
				title = url
			}
			title = pica.TruncLine(title, w-2)
			*links = append(*links, Link{Text: title, URL: url})
			blk.segs = [][]line{{{text: "[" + title + "]"}}}

		case pica.Pre:
			// Atomic, unless it declares a repeated lead-in, and then
			// it splits freely, as in press.
			blk.atomic = b.Repeat == 0
			blk.repeat = b.Repeat
			for _, s := range b.Lines {
				blk.segs = append(blk.segs, []line{{text: pica.TruncLine(s, w)}})
			}

		case pica.TableBlk:
			var err error
			blk, err = composeTable(b, w)
			if err != nil {
				return nil, err
			}
			blk.tight = b.Tight

		default: // Para, Quote, Item, Term: justified prose with emphasis
			open := false
			for i, s := range justified(b, w) {
				var ln line
				ln, open = prose(s, open)
				if b.Kind == pica.Term && i == 0 {
					n := utf8.RuneCountInString(b.Label)
					ln.spans = append([]span{{0, n, "label"}}, ln.spans...)
				}
				blk.segs = append(blk.segs, []line{ln})
			}
		}
		out = append(out, blk)
	}
	if doc.Rights != "" {
		out = append(out, block{segs: [][]line{{{text: pica.TruncLine(doc.Rights, w), alias: "rights"}}}})
	}
	return out, nil
}

// justified sets a prose block as the PDF's mono page sets it: the
// justified Knuth-Plass breaker with its hyphenation, on the text
// writer's geometry (the quote inset, the item indent, the term
// run-in), the last line of a paragraph ragged.
func justified(b pica.Block, w int) []string {
	switch b.Kind {
	case pica.Quote:
		inset := strings.Repeat(" ", pica.QuoteIndent)
		var out []string
		for _, ln := range pica.JustifyParagraph(b.Text, w-2*pica.QuoteIndent) {
			out = append(out, inset+ln)
		}
		if b.Attrib != "" {
			out = append(out, pica.AttribLine(b.Attrib, w))
		}
		return out
	case pica.Item:
		hang := strings.Repeat(" ", pica.ItemIndent)
		var out []string
		for i, ln := range pica.JustifyParagraph(b.Text, w-pica.ItemIndent) {
			if i == 0 {
				out = append(out, pica.Bullet+" "+ln)
			} else {
				out = append(out, hang+ln)
			}
		}
		return out
	case pica.Term:
		hang := strings.Repeat(" ", pica.ItemIndent)
		first, runIn := pica.TermRunIn(b.Label, w)
		if !runIn {
			out := []string{pica.TruncLine(b.Label, w)}
			for _, ln := range pica.JustifyParagraph(b.Text, w-pica.ItemIndent) {
				out = append(out, hang+ln)
			}
			return out
		}
		var out []string
		for i, ln := range pica.JustifyParagraphRunIn(b.Text, first, w-pica.ItemIndent) {
			if i == 0 {
				out = append(out, b.Label+strings.Repeat(" ", pica.TermGap)+ln)
			} else {
				out = append(out, hang+ln)
			}
		}
		return out
	}
	return pica.JustifyParagraph(b.Text, w)
}

// prose marks one wrapped line's emphasis: the marker underscores
// become blank cells around the emphasized span.
func prose(s string, open bool) (line, bool) {
	clean, spans, still := pica.EmphLine(s, open)
	ln := line{text: clean}
	for _, sp := range spans {
		ln.spans = append(ln.spans, span{sp.Start, sp.End, "emph"})
	}
	return ln, still
}

// composeTable lays a table out and marks its header and total rows;
// its separators are drawn in the rule glyph. The lead-in (header,
// notes, separator) repeats after a split; each row is a segment,
// a total row with the separator above it.
func composeTable(b pica.Block, w int) (block, error) {
	tl, err := b.Table.Layout(b.TableWidth(w))
	if err != nil {
		return block{}, err
	}
	lines := tl.Lines()
	rows := tl.RowLines()
	lead := len(lines)
	if len(rows) > 0 {
		lead = rows[0].Start
	}
	rule := func(s string) string { return strings.ReplaceAll(s, "-", "─") }
	blk := block{repeat: lead}
	for i := 0; i < lead; i++ {
		ln := line{text: lines[i]}
		switch {
		case i < len(tl.Header):
			ln.alias = "tablehead"
		case i == lead-1 && len(tl.Header) > 0:
			ln.text = rule(ln.text)
		}
		blk.segs = append(blk.segs, []line{ln})
	}
	for i, r := range rows {
		var seg []line
		if tl.Totals[i] {
			seg = append(seg, line{text: rule(lines[r.Start-1])})
		}
		for j := r.Start; j < r.End; j++ {
			ln := line{text: lines[j]}
			if tl.Totals[i] {
				ln.alias = "total"
			}
			seg = append(seg, ln)
		}
		blk.segs = append(blk.segs, seg)
	}
	return blk, nil
}

// minKeep is the orphan/widow threshold: a split never leaves fewer
// than minKeep segments of a block on either side of a column break.
const minKeep = 2

// flow distributes blocks into columns of capacity(i) lines each,
// the press compositor's rules in whole lines: splits happen only
// between segments, leaving at least minKeep segments on both sides;
// the repeated lead-in is re-emitted after each split; atomic blocks
// move whole unless taller than an entire fresh column; a heading is
// never left at a column bottom without minKeep segments of what
// follows. Blocks are separated by one blank line unless tight.
func flow(blocks []block, capacity func(int) int) [][]line {
	var out [][]line
	var cur []line
	colIdx := 0

	closeCol := func() {
		out = append(out, cur)
		cur = nil
		colIdx++
	}
	place := func(b block, upto int) {
		if len(cur) > 0 && !b.tight {
			cur = append(cur, line{})
		}
		for _, s := range b.segs[:upto] {
			cur = append(cur, s...)
		}
	}

	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		for {
			sep := 0
			if len(cur) > 0 && !b.tight {
				sep = 1
			}
			avail := capacity(colIdx) - len(cur) - sep
			h := b.height()

			if b.keepNext && i+1 < len(blocks) && len(cur) > 0 {
				need := h
				j := i + 1
				for ; j < len(blocks) && blocks[j].keepNext; j++ {
					need += 1 + blocks[j].height()
				}
				if j < len(blocks) {
					// The opening of a block is its lead-in and
					// minKeep segments of content.
					nb := blocks[j]
					need++
					for _, s := range nb.segs[:min(nb.repeat+minKeep, len(nb.segs))] {
						need += len(s)
					}
				}
				if avail < need {
					closeCol()
					continue
				}
			}

			if h <= avail {
				place(b, len(b.segs))
				break
			}

			k := splitSegs(b, avail)
			if k <= 0 {
				if len(cur) > 0 {
					closeCol()
					continue
				}
				k = forceSplit(b, avail)
			}
			place(b, k)
			b = b.rest(k)
			b.tight = false
			closeCol()
			if len(b.segs) == 0 {
				break
			}
		}
	}
	if len(cur) > 0 || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

// fitSegs returns how many leading segments of b fit in avail lines.
func fitSegs(b block, avail int) int {
	k, h := 0, 0
	for k < len(b.segs) && h+len(b.segs[k]) <= avail {
		h += len(b.segs[k])
		k++
	}
	return k
}

// splitSegs returns how many leading segments of b fit in avail
// lines under the orphan/widow rules, or 0 for "move whole".
func splitSegs(b block, avail int) int {
	if b.atomic {
		return 0
	}
	n := len(b.segs)
	k := min(fitSegs(b, avail), n-minKeep)
	if k < b.repeat+minKeep {
		return 0
	}
	return k
}

// forceSplit fits as many segments as possible into an empty column,
// ignoring the keep rules, and always makes progress.
func forceSplit(b block, avail int) int {
	if b.atomic && b.height() <= avail || len(b.segs) == 1 {
		return len(b.segs)
	}
	k := max(fitSegs(b, avail), b.repeat+1)
	return min(k, len(b.segs))
}

// rest returns the unplaced remainder after splitting at k, with
// the repeated lead-in re-attached.
func (b block) rest(k int) block {
	if k >= len(b.segs) {
		return block{}
	}
	segs := append(append([][]line{}, b.segs[:b.repeat]...), b.segs[k:]...)
	return block{segs: segs, repeat: b.repeat}
}
