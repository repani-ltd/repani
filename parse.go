// Parse: source text -> Doc, the typed block model consumed by the
// writers. The language is specified in doc.go.
package typeset

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// BlockKind classifies a Doc block.
type BlockKind int

const (
	Para     BlockKind = iota // flowing prose; writers wrap it
	Heading                   // section heading (text without the "# ")
	TableBlk                  // structured table
	Pre                       // verbatim lines; atomic for column flow
	RuleBlk                   // horizontal rule
	LinkBlk                   // .link reference: URL and optional title
)

// Block is one element of a parsed document.
type Block struct {
	Kind   BlockKind
	Text   string   // Para: unwrapped prose. Heading: text. LinkBlk: "URL [TITLE]".
	Table  *Table   // TableBlk
	Width  int      // TableBlk: fixed width from the spec (0 = document width)
	Lines  []string // Pre
	Repeat int      // Pre: leading lines a splitting writer repeats
	// Tight marks a block that was contiguous with the previous one
	// in the source (no blank line between); writers preserve that.
	Tight bool
}

// TableWidth resolves a table block's layout width against the
// document width: the spec's fixed width applies only when it is
// smaller (a wider request cannot be honored on a narrower page).
// Both writers use this, so text and PDF always agree.
func (b Block) TableWidth(docWidth int) int {
	if b.Width > 0 && b.Width < docWidth {
		return b.Width
	}
	return docWidth
}

// Layout holds the document-global attributes from the layout
// trailer, with defaults applied.
type Layout struct {
	Width int    // characters per line/column
	Paper string // "a4", "a5", or "letter"
	Cols  int    // pdf columns per page
	Font  string // "mono" or "sans"; proportional writers honor it
}

// DefaultLayout is the layout of a document with no trailer.
func DefaultLayout() Layout { return Layout{Width: 40, Paper: "a4", Cols: 3, Font: "mono"} }

// Doc is a parsed document.
type Doc struct {
	Title  string
	Layout Layout
	Blocks []Block
}

// Sentinel errors for Parse.
var (
	ErrEmptyDoc          = errors.New("typeset: document has no title line")
	ErrUnknownCommand    = errors.New("typeset: unknown dot command")
	ErrUnterminatedBlock = errors.New("typeset: .table or .pre block without .end")
	ErrStrayEnd          = errors.New("typeset: .end without an open block")
	ErrContentAfterTrail = errors.New("typeset: content after a layout command")
	ErrDuplicateAttr     = errors.New("typeset: duplicate layout command")
	ErrBadAttr           = errors.New("typeset: invalid command value")
)

// isDotCommand applies the wire lexing rule: a dot followed by a
// lowercase letter opens a command; ". " and ".." begin ordinary
// text.
func isDotCommand(trimmed string) bool {
	return len(trimmed) >= 2 && trimmed[0] == '.' &&
		trimmed[1] >= 'a' && trimmed[1] <= 'z'
}

// isRule reports a horizontal rule: 3+ dashes and nothing else.
func isRule(trimmed string) bool {
	if len(trimmed) < 3 {
		return false
	}
	for _, r := range trimmed {
		if r != '-' {
			return false
		}
	}
	return true
}

// parser carries the scan state.
type parser struct {
	doc      *Doc
	para     []string // accumulating prose lines
	blankRun bool     // a blank line precedes the next block
	attrs    map[string]bool
}

// inTrail reports whether the layout trailer has started (a layout
// command has been seen), after which only layout commands may
// follow.
func (p *parser) inTrail() bool { return len(p.attrs) > 0 }

// Parse turns source text into a Doc.
func Parse(src string) (*Doc, error) {
	lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
	p := &parser{
		doc:   &Doc{Layout: DefaultLayout()},
		attrs: map[string]bool{},
	}

	// Title: the first non-blank line.
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i == len(lines) {
		return nil, ErrEmptyDoc
	}
	p.doc.Title = strings.TrimSpace(lines[i])
	p.blankRun = true
	i++

	for ; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		n := i + 1 // 1-based line number for errors

		if trimmed == "" {
			p.flush()
			p.blankRun = true
			continue
		}

		if isDotCommand(trimmed) {
			var err error
			i, err = p.command(lines, i, trimmed)
			if err != nil {
				return nil, err
			}
			continue
		}

		if p.inTrail() {
			return nil, fmt.Errorf("%w (line %d)", ErrContentAfterTrail, n)
		}

		switch {
		case isRule(trimmed):
			p.flush()
			p.add(Block{Kind: RuleBlk})

		case strings.HasPrefix(trimmed, "# "):
			p.flush()
			p.add(Block{Kind: Heading, Text: strings.TrimSpace(trimmed[2:])})

		default:
			// Everything unmarked is prose -- fill mode, as in
			// troff. Aligned or preformatted content must say .pre;
			// the parser never infers structure from spacing.
			p.para = append(p.para, trimmed)
		}
	}
	p.flush()
	return p.doc, nil
}

// command dispatches a dot command at lines[i] and returns the new
// scan index. The vocabulary is closed: anything unrecognized is an
// error.
func (p *parser) command(lines []string, i int, trimmed string) (int, error) {
	n := i + 1
	word, rest, _ := strings.Cut(trimmed, " ")
	rest = strings.TrimSpace(rest)

	if handled, err := p.layoutCommand(word, rest, n); handled || err != nil {
		return i, err
	}

	if p.inTrail() {
		return 0, fmt.Errorf("%w (line %d)", ErrContentAfterTrail, n)
	}

	switch word {
	case ".end":
		return 0, fmt.Errorf("%w (line %d)", ErrStrayEnd, n)

	case ".pre":
		p.flush()
		repeat := 0
		if rest != "" {
			v, err := strconv.Atoi(rest)
			if err != nil || v < 0 {
				return 0, fmt.Errorf("%w: .pre %q (line %d)", ErrBadAttr, rest, n)
			}
			repeat = v
		}
		body, next, err := collectUntilEnd(lines, i+1, ".pre")
		if err != nil {
			return 0, err
		}
		p.add(Block{Kind: Pre, Lines: body, Repeat: repeat})
		return next, nil

	case ".table":
		p.flush()
		body, next, err := collectUntilEnd(lines, i+1, ".table")
		if err != nil {
			return 0, err
		}
		blk, err := parseTableBlock(rest, body, n)
		if err != nil {
			return 0, err
		}
		p.add(blk)
		return next, nil

	case ".link":
		// .link URL [TITLE...]: the first field is the URL (which
		// cannot contain a literal space), everything after it is
		// the title phrase; writers that can render real links
		// show the title as the anchor text.
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0, fmt.Errorf("%w: .link wants URL [TITLE] (line %d)", ErrBadAttr, n)
		}
		p.flush()
		p.add(Block{Kind: LinkBlk, Text: strings.Join(fields, " ")})
		return i, nil
	}

	return 0, fmt.Errorf("%w: %s (line %d)", ErrUnknownCommand, word, n)
}

// layoutCommand recognizes and applies a layout trailer command,
// owning the whole trailer mechanism (vocabulary, duplicate check,
// validation) in one place. Returns handled == false for words that
// are not layout commands.
func (p *parser) layoutCommand(word, rest string, n int) (handled bool, err error) {
	bad := func() (bool, error) {
		return true, fmt.Errorf("%w: %s %q (line %d)", ErrBadAttr, word, rest, n)
	}
	set := func(apply func()) (bool, error) {
		p.flush()
		if p.attrs[word] {
			return true, fmt.Errorf("%w: %s (line %d)", ErrDuplicateAttr, word, n)
		}
		p.attrs[word] = true
		apply()
		return true, nil
	}

	switch word {
	case ".width":
		v, err := strconv.Atoi(rest)
		if err != nil || v < 10 || v > 200 {
			return bad()
		}
		return set(func() { p.doc.Layout.Width = v })
	case ".paper":
		switch rest {
		case "a4", "a5", "letter":
			return set(func() { p.doc.Layout.Paper = rest })
		default:
			return bad()
		}
	case ".cols":
		v, err := strconv.Atoi(rest)
		if err != nil || v < 1 || v > 6 {
			return bad()
		}
		return set(func() { p.doc.Layout.Cols = v })
	case ".font":
		switch rest {
		case "mono", "sans":
			return set(func() { p.doc.Layout.Font = rest })
		default:
			return bad()
		}
	}
	return false, nil
}

func (p *parser) add(b Block) {
	// blankRun starts true, so the first block is never Tight.
	b.Tight = !p.blankRun
	p.doc.Blocks = append(p.doc.Blocks, b)
	p.blankRun = false
}

// flush closes the accumulating paragraph, if any.
func (p *parser) flush() {
	if len(p.para) > 0 {
		p.add(Block{Kind: Para, Text: strings.Join(p.para, " ")})
		p.para = nil
	}
}

// collectUntilEnd gathers lines from start until a lone ".end",
// returning the body and the index of the .end line.
func collectUntilEnd(lines []string, start int, kind string) ([]string, int, error) {
	for j := start; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == ".end" {
			return lines[start:j], j, nil
		}
	}
	return nil, 0, fmt.Errorf("%w: %s opened at line %d", ErrUnterminatedBlock, kind, start)
}

// parseTableBlock builds a TableBlk from a .table spec (with
// optional leading fixed width, then an optional "-" for a
// headerless table) and its |-separated rows. Unless "-" is given,
// the first non-empty row is the header.
func parseTableBlock(spec string, body []string, atLine int) (Block, error) {
	fixed := 0
	if first, rest, ok := strings.Cut(spec, " "); ok {
		if w, err := strconv.Atoi(first); err == nil {
			if w < 1 {
				return Block{}, fmt.Errorf("%w: .table width %q (line %d)", ErrBadAttr, first, atLine)
			}
			fixed = w
			spec = rest
		}
	}
	header := true
	if rest, ok := strings.CutPrefix(spec, "- "); ok {
		header = false
		spec = rest
	}
	tbl, err := NewTable(spec)
	if err != nil {
		return Block{}, fmt.Errorf("%w (line %d)", err, atLine)
	}
	for _, line := range body {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cells := splitCells(line)
		if header {
			tbl.Header(cells...)
			header = false
		} else {
			tbl.Row(cells...)
		}
	}
	return Block{Kind: TableBlk, Table: tbl, Width: fixed}, nil
}

// splitCells splits a table row by "|" and trims each cell.
func splitCells(line string) []string {
	parts := strings.Split(line, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}
