// The pdf subcommand: lay rendered monospace text into an N-column
// newspaper-style PDF with orphan/widow control.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pavlos/typeset"
	"github.com/pavlos/typeset/pdf"
)

// Layout constants (points). Text sizing is derived from the input:
// the font size is chosen so the widest line exactly fills a column
// (unless -pt overrides it).
const (
	sheetMargin = 40.0
	sheetGutter = 16.0
	lineSpacing = 1.25 // line height in ems
	minPt       = 4.5
	maxPt       = 14.0
)

// minKeep is the orphan/widow threshold: a split never leaves fewer
// than minKeep lines of a block on either side of a column break.
const minKeep = 2

// parseMixed parses args against fs, allowing flags before, between,
// and after positional arguments (stdlib flag stops at the first
// non-flag token, so it is re-invoked past each positional). Returns
// the positionals in order.
func parseMixed(fs *flag.FlagSet, args []string) []string {
	var pos []string
	fs.Parse(args)
	rem := fs.Args()
	for len(rem) > 0 {
		pos = append(pos, rem[0])
		fs.Parse(rem[1:])
		rem = fs.Args()
	}
	return pos
}

func pdfCmd(args []string) int {
	fs := flag.NewFlagSet("pdf", flag.ExitOnError)
	out := fs.String("o", "", "output file (default stdout)")
	cols := fs.Int("cols", 3, "columns per page")
	paper := fs.String("paper", "a4", "paper size: a4, a5, or letter")
	ptFlag := fs.Float64("pt", 0, "font size in points (default: fit widest line to column)")
	title := fs.String("title", "", "masthead text (default: first line of input)")
	nomast := fs.Bool("nomast", false, "no masthead; all input flows into columns")
	pos := parseMixed(fs, args)
	if len(pos) > 1 {
		fmt.Fprintln(os.Stderr, "pica pdf: at most one input file (default stdin)")
		return 1
	}

	var (
		text []byte
		err  error
	)
	if len(pos) == 0 || pos[0] == "-" {
		text, err = io.ReadAll(os.Stdin)
	} else {
		text, err = os.ReadFile(pos[0])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica: read input: %v\n", err)
		return 1
	}

	var size pdf.PageSize
	switch *paper {
	case "a4":
		size = pdf.PageA4
	case "a5":
		size = pdf.PageA5
	case "letter":
		size = pdf.PageLetter
	default:
		fmt.Fprintf(os.Stderr, "pica pdf: unknown paper %q\n", *paper)
		return 1
	}
	if *cols < 1 || *cols > 6 {
		fmt.Fprintln(os.Stderr, "pica pdf: -cols must be 1..6")
		return 1
	}

	masthead, body := splitMasthead(string(text), *title, *nomast)
	doc, err := broadsheet(masthead, body, size, *cols, *ptFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pica pdf: %v\n", err)
		return 1
	}

	if *out == "" {
		if _, err := os.Stdout.Write(doc); err != nil {
			fmt.Fprintf(os.Stderr, "pica pdf: write: %v\n", err)
			return 1
		}
		return 0
	}
	if err := os.WriteFile(*out, doc, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "pica pdf: %v\n", err)
		return 1
	}
	return 0
}

// splitMasthead resolves the masthead text and the body lines. By
// default the first non-blank input line becomes the masthead;
// -title overrides it (keeping the whole input as body) and -nomast
// disables the masthead entirely.
func splitMasthead(text, title string, nomast bool) (string, []string) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if nomast {
		return "", lines
	}
	if title != "" {
		return title, lines
	}
	for i, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			body := lines[i+1:]
			for len(body) > 0 && strings.TrimSpace(body[0]) == "" {
				body = body[1:]
			}
			return strings.TrimSpace(ln), body
		}
	}
	return "", nil
}

// ── Blocks ──────────────────────────────────────────────────────────

// block is a run of consecutive non-blank lines: the atomic unit the
// column flow tries to keep together.
type block struct {
	lines   []string
	heading bool // single line with a following block: keep with next
	table   bool // second line is a table separator: splits repeat the header
}

// parseBlocks splits body lines into blocks at blank lines.
func parseBlocks(lines []string) []block {
	var blocks []block
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			blocks = append(blocks, block{lines: cur})
			cur = nil
		}
	}
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			flush()
			continue
		}
		cur = append(cur, ln)
	}
	flush()
	for i := range blocks {
		blocks[i].heading = len(blocks[i].lines) == 1 && i+1 < len(blocks)
		blocks[i].table = isTableBlock(blocks[i].lines)
	}
	return blocks
}

// isTableBlock reports whether lines look like a rendered table:
// a header row followed by a dashes-and-spaces separator row.
func isTableBlock(lines []string) bool {
	if len(lines) < 3 {
		return false
	}
	sep := strings.TrimRight(lines[1], " ")
	if !strings.Contains(sep, "-") {
		return false
	}
	for _, r := range sep {
		if r != '-' && r != ' ' {
			return false
		}
	}
	return true
}

// splitPoint returns how many lines of b to place into a slot of
// avail lines, honoring orphan/widow constraints, or 0 when no
// acceptable split exists (the block should move as a whole).
func splitPoint(b block, avail int) int {
	n := len(b.lines)
	if b.table {
		// First part keeps the 2 header lines plus >= minKeep data
		// rows; the remainder must retain >= minKeep data rows (the
		// header is re-attached to the remainder separately).
		k := min(avail, n-minKeep)
		if k < 2+minKeep {
			return 0
		}
		return k
	}
	if n < 2*minKeep {
		return 0 // too small to split acceptably
	}
	k := min(avail, n-minKeep)
	if k < minKeep {
		return 0
	}
	return k
}

// rest returns the unplaced remainder of a block split at k. Table
// remainders get the header rows re-attached so every continuation
// column repeats them.
func (b block) rest(k int) block {
	if b.table {
		hdr := b.lines[:2]
		remainder := append(append([]string{}, hdr...), b.lines[k:]...)
		return block{lines: remainder, table: true}
	}
	return block{lines: b.lines[k:]}
}

// ── Column flow ─────────────────────────────────────────────────────

// flow distributes blocks into columns. capacity(i) is the line
// capacity of the i-th column overall (page-one columns can be
// shorter than later pages' because of the masthead). Rules:
//
//   - blocks are separated by one blank line within a column
//   - a heading is never left alone at a column bottom: it needs
//     room for itself plus minKeep lines of the next block
//   - a split leaves at least minKeep lines on both sides (no
//     orphans at column bottoms, no widows at column tops)
//   - blocks of fewer than 2*minKeep lines never split
//   - a table split re-draws the header rows in the next column
//
// A block taller than an entire fresh column is force-split as a
// last resort.
func flow(blocks []block, capacity func(int) int) [][]string {
	var out [][]string
	var cur []string
	colIdx := 0

	closeCol := func() {
		out = append(out, cur)
		cur = nil
		colIdx++
	}
	place := func(lines []string) {
		if len(cur) > 0 {
			cur = append(cur, "")
		}
		cur = append(cur, lines...)
	}

	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		for {
			cap := capacity(colIdx)
			sep := 0
			if len(cur) > 0 {
				sep = 1
			}
			avail := cap - len(cur) - sep
			n := len(b.lines)

			// Keep-with-next: a heading must fit together with the
			// first minKeep lines of what follows.
			if b.heading && i+1 < len(blocks) && len(cur) > 0 {
				needNext := n + 1 + min(minKeep, len(blocks[i+1].lines))
				if avail < needNext {
					closeCol()
					continue
				}
			}

			if n <= avail {
				place(b.lines)
				break
			}

			k := splitPoint(b, avail)
			if k <= 0 {
				if len(cur) > 0 {
					closeCol()
					continue
				}
				// Top of an empty column and still no acceptable
				// split: the block is taller than a full column.
				// Force-split to keep making progress.
				k = min(avail, n-1)
				if k < 1 {
					k = 1
				}
			}
			place(b.lines[:k])
			b = b.rest(k)
			closeCol()
		}
	}
	if len(cur) > 0 || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

// ── Drawing ─────────────────────────────────────────────────────────

// broadsheet renders masthead + body into an N-column PDF.
func broadsheet(masthead string, body []string, size pdf.PageSize, ncols int, ptOverride float64) ([]byte, error) {
	pageW, pageH := size.Dimensions()
	usableW := pageW - 2*sheetMargin
	colW := (usableW - float64(ncols-1)*sheetGutter) / float64(ncols)

	// Font size: fit the widest body line to the column width
	// (monospace: one rune is 0.6 em).
	pt := ptOverride
	if pt == 0 {
		widest := typeset.MaxLineWidth(strings.Join(body, "\n"))
		if widest < 10 {
			widest = 10
		}
		pt = colW / (0.6 * float64(widest))
		pt = max(minPt, min(maxPt, pt))
	}
	lineH := pt * lineSpacing

	// Masthead band on page one.
	topY := pageH - sheetMargin
	colTopRest := topY
	colTopFirst := topY
	var mastPt float64
	if masthead != "" {
		mastPt = usableW / (0.6 * float64(max(len([]rune(masthead)), 8)))
		mastPt = max(12, min(30, mastPt))
		colTopFirst = topY - mastPt*1.35 - 10
	}
	colBottom := sheetMargin

	linesFirst := int((colTopFirst - colBottom) / lineH)
	linesRest := int((colTopRest - colBottom) / lineH)
	if linesFirst < 2*minKeep+2 || linesRest < 2*minKeep+2 {
		return nil, fmt.Errorf("page too small for %d columns at %.1fpt", ncols, pt)
	}

	capacity := func(col int) int {
		if col < ncols {
			return linesFirst
		}
		return linesRest
	}
	blocks := parseBlocks(body)
	columns := flow(blocks, capacity)

	// Balance a single underfull page: find the smallest uniform
	// capacity that still fits the content in ncols columns, so
	// short content spreads across the page instead of stacking in
	// the first column. Multi-page documents keep full columns.
	if len(columns) <= ncols {
		lo, hi, best := 2*minKeep+2, linesFirst, linesFirst
		for lo <= hi {
			mid := (lo + hi) / 2
			if c := flow(blocks, func(int) int { return mid }); len(c) <= ncols {
				best, hi = mid, mid-1
			} else {
				lo = mid + 1
			}
		}
		columns = flow(blocks, func(int) int { return best })
	}

	doc := &pdf.Doc{Title: masthead, Creator: "pica", PageSize: size, Compress: true}
	for pg := 0; pg*ncols < len(columns) || pg == 0; pg++ {
		var p pdf.Page
		colTop := colTopRest
		if pg == 0 && masthead != "" {
			colTop = colTopFirst
			// Centered bold masthead with a rule underneath.
			p.SetFont(pdf.Bold, mastPt)
			w := pdf.Width(masthead, pdf.Bold, mastPt)
			p.Text((pageW-w)/2, topY-mastPt, masthead)
			p.StrokeGray(0)
			p.Line(sheetMargin, colTop+lineH*0.6, pageW-sheetMargin, colTop+lineH*0.6, 1.0)
		}

		// Column text.
		p.SetFont(pdf.Regular, pt)
		for c := 0; c < ncols; c++ {
			idx := pg*ncols + c
			if idx >= len(columns) {
				break
			}
			x := sheetMargin + float64(c)*(colW+sheetGutter)
			tb := p.BeginText()
			tb.Move(x, colTop-pt)
			for _, ln := range columns[idx] {
				if strings.TrimSpace(ln) != "" {
					tb.Show(ln)
				}
				tb.MoveRel(0, -lineH)
			}
			tb.End()
		}

		// Hairline rules centered in the gutters, running to the
		// depth of the page's deepest column.
		deepest := 0
		for c := 0; c < ncols; c++ {
			if idx := pg*ncols + c; idx < len(columns) && len(columns[idx]) > deepest {
				deepest = len(columns[idx])
			}
		}
		ruleBottom := max(colTop-float64(deepest)*lineH, colBottom)
		p.StrokeGray(0.55)
		for c := 1; c < ncols; c++ {
			x := sheetMargin + float64(c)*(colW+sheetGutter) - sheetGutter/2
			p.Line(x, colTop, x, ruleBottom, 0.4)
		}

		// Page number, centered in the bottom margin.
		p.SetFont(pdf.Regular, pt*0.9)
		p.Gray(0.4)
		num := fmt.Sprintf("- %d -", pg+1)
		p.Text((pageW-pdf.Width(num, pdf.Regular, pt*0.9))/2, sheetMargin/2-2, num)
		p.Gray(0)

		doc.Add(&p)
		if (pg+1)*ncols >= len(columns) {
			break
		}
	}
	return doc.Bytes(), nil
}
