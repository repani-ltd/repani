// Parse: source text -> Doc, the typed block model consumed by the
// writers. The language is specified in doc.go.
package pica

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
	Heading                   // section (#) or subsection (##) heading; see Level
	TableBlk                  // structured table
	Pre                       // verbatim lines; atomic for column flow
	RuleBlk                   // horizontal rule
	LinkBlk                   // .link reference: URL and optional title
	Quote                     // .quote: indented prose, optional attribution
	Item                      // .item: one bulleted list item
)

// Block is one element of a parsed document.
type Block struct {
	Kind   BlockKind
	Text   string   // Para/Quote/Item: unwrapped prose. Heading: text. LinkBlk: "URL [TITLE]".
	Attrib string   // Quote: attribution line ("" = none)
	Table  *Table   // TableBlk
	Width  int      // TableBlk: fixed width from the spec (0 = document width)
	Lines  []string // Pre
	Repeat int      // Pre: leading lines a splitting writer repeats
	Level  int      // Heading: 1 (section) or 2 (subsection)
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

// defaultLayout is the layout of a document with no trailer.
func defaultLayout() Layout { return Layout{Width: 80, Paper: "a4", Cols: 1, Font: "mono"} }

// Doc is a parsed document.
type Doc struct {
	Title  string
	By     string // .by byline ("" = none)
	Date   string // .date dateline ("" = none)
	Rights string // .rights imprint/copyright notice ("" = none)
	Layout Layout
	Blocks []Block
}

// Byline renders the .by/.date header line: "by BY -- DATE" with
// whichever parts are present, "" when neither is set. Both writers
// use it, so the header stays identical across surfaces.
func (d *Doc) Byline() string {
	switch {
	case d.By != "" && d.Date != "":
		return "by " + d.By + " -- " + d.Date
	case d.By != "":
		return "by " + d.By
	default:
		return d.Date
	}
}

// Sentinel errors for Parse. ErrBadAttr covers every malformed
// line in the closed vocabulary -- a command with a bad or missing
// value, a command out of its place (.attrib outside .quote, a
// third heading level), an empty block; the wrapping message names
// the fault.
var (
	ErrEmptyDoc          = errors.New("pica: document has no title line")
	ErrUnknownCommand    = errors.New("pica: unknown dot command")
	ErrUnterminatedBlock = errors.New("pica: block without .end")
	ErrStrayEnd          = errors.New("pica: .end without an open block")
	ErrContentAfterTrail = errors.New("pica: content after a layout command")
	ErrDuplicateAttr     = errors.New("pica: duplicate command")
	ErrBadAttr           = errors.New("pica: invalid command or value")
	ErrMetaAfterContent  = errors.New("pica: metadata commands (.by, .date, .rights) must precede content")
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
	openItem bool     // the last block is an .item still filling
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
		doc:   &Doc{Layout: defaultLayout()},
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
	if isDotCommand(strings.TrimSpace(lines[i])) {
		// A command in title position would otherwise be taken
		// literally as the title and silently not applied.
		return nil, fmt.Errorf("%w: line %d is a command, not a title", ErrEmptyDoc, i+1)
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

		case strings.HasPrefix(trimmed, "### "):
			// Two heading levels, deliberately and permanently: a
			// third would be silent structure rot, so it is loud.
			return nil, fmt.Errorf("%w: \"###\" heading: headings have two levels (line %d)", ErrBadAttr, n)

		case strings.HasPrefix(trimmed, "## "):
			p.flush()
			p.add(Block{Kind: Heading, Text: strings.TrimSpace(trimmed[3:]), Level: 2})

		case strings.HasPrefix(trimmed, "# "):
			p.flush()
			p.add(Block{Kind: Heading, Text: strings.TrimSpace(trimmed[2:]), Level: 1})

		default:
			// Everything unmarked is prose -- fill mode, as in
			// troff. Aligned or preformatted content must say .pre;
			// the parser never infers structure from spacing. An
			// unmarked line directly under an .item continues that
			// item's text: items fill exactly as paragraphs do, so
			// hard-wrapped source means the same thing everywhere.
			if p.openItem {
				last := &p.doc.Blocks[len(p.doc.Blocks)-1]
				last.Text += " " + trimmed
				continue
			}
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

	// A comment is invisible everywhere -- it neither ends a
	// paragraph nor counts as content in the trailer.
	if word == ".rem" {
		return i, nil
	}

	if handled, err := p.layoutCommand(word, rest, n); handled || err != nil {
		return i, err
	}

	// A stray .end is a stray .end wherever it sits, the trailer
	// included.
	if word == ".end" {
		return 0, fmt.Errorf("%w (line %d)", ErrStrayEnd, n)
	}

	if p.inTrail() {
		return 0, fmt.Errorf("%w (line %d)", ErrContentAfterTrail, n)
	}

	switch word {
	case ".by", ".date", ".rights":
		return i, p.meta(word, rest, n)

	case ".quote":
		p.flush()
		body, next, err := collectUntilEnd(lines, i, ".quote")
		if err != nil {
			return 0, err
		}
		blk, err := parseQuoteBlock(body, n)
		if err != nil {
			return 0, err
		}
		p.add(blk)
		return next, nil

	case ".attrib":
		return 0, fmt.Errorf("%w: .attrib outside .quote (line %d)", ErrBadAttr, n)

	case ".item":
		if rest == "" {
			return 0, fmt.Errorf("%w: .item wants text (line %d)", ErrBadAttr, n)
		}
		p.flush()
		p.add(Block{Kind: Item, Text: rest})
		p.openItem = true
		return i, nil

	case ".pre":
		p.flush()
		repeat := 0
		if rest != "" {
			v, err := strconv.Atoi(rest)
			if err != nil || v < 0 {
				return 0, fmt.Errorf("%w: .pre %q: the repeat count must be a non-negative integer (line %d)", ErrBadAttr, rest, n)
			}
			repeat = v
		}
		body, next, err := collectUntilEnd(lines, i, ".pre")
		if err != nil {
			return 0, err
		}
		p.add(Block{Kind: Pre, Lines: body, Repeat: repeat})
		return next, nil

	case ".table":
		p.flush()
		body, next, err := collectUntilEnd(lines, i, ".table")
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
	bad := func(want string) (bool, error) {
		return true, fmt.Errorf("%w: %s %q: want %s (line %d)", ErrBadAttr, word, rest, want, n)
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
			return bad("10-200")
		}
		return set(func() { p.doc.Layout.Width = v })
	case ".paper":
		switch rest {
		case "a4", "a5", "letter":
			return set(func() { p.doc.Layout.Paper = rest })
		default:
			return bad("a4, a5, or letter")
		}
	case ".cols":
		v, err := strconv.Atoi(rest)
		if err != nil || v < 1 || v > 6 {
			return bad("1-6")
		}
		return set(func() { p.doc.Layout.Cols = v })
	case ".font":
		switch rest {
		case "mono", "sans":
			return set(func() { p.doc.Layout.Font = rest })
		default:
			return bad("mono or sans")
		}
	}
	return false, nil
}

// meta handles the metadata commands (.by, .date, .rights): each
// must precede all content blocks and appear at most once.
func (p *parser) meta(word, rest string, n int) error {
	if rest == "" {
		return fmt.Errorf("%w: %s wants text (line %d)", ErrBadAttr, word, n)
	}
	if len(p.doc.Blocks) > 0 || len(p.para) > 0 {
		return fmt.Errorf("%w: %s (line %d)", ErrMetaAfterContent, word, n)
	}
	field := &p.doc.By
	switch word {
	case ".date":
		field = &p.doc.Date
	case ".rights":
		field = &p.doc.Rights
	}
	if *field != "" {
		return fmt.Errorf("%w: %s (line %d)", ErrDuplicateAttr, word, n)
	}
	*field = rest
	return nil
}

// parseQuoteBlock builds a Quote from a .quote body: the non-blank
// lines fill as one paragraph; an optional final ".attrib WHO" line
// sets the attribution. Content after .attrib, a second .attrib, or
// an empty quote is an error.
func parseQuoteBlock(body []string, atLine int) (Block, error) {
	var prose []string
	attrib := ""
	for _, line := range body {
		trimmed := strings.TrimSpace(line)
		word, rest, _ := strings.Cut(trimmed, " ")
		switch {
		case trimmed == "":

		case attrib != "":
			return Block{}, fmt.Errorf("%w: .quote content after .attrib (line %d)", ErrBadAttr, atLine)

		case word == ".attrib":
			rest = strings.TrimSpace(rest)
			if rest == "" {
				return Block{}, fmt.Errorf("%w: .attrib wants text (line %d)", ErrBadAttr, atLine)
			}
			attrib = rest

		default:
			prose = append(prose, trimmed)
		}
	}
	if len(prose) == 0 {
		return Block{}, fmt.Errorf("%w: empty .quote (line %d)", ErrBadAttr, atLine)
	}
	return Block{Kind: Quote, Text: strings.Join(prose, " "), Attrib: attrib}, nil
}

func (p *parser) add(b Block) {
	// blankRun starts true, so the first block is never Tight.
	b.Tight = !p.blankRun
	p.doc.Blocks = append(p.doc.Blocks, b)
	p.blankRun = false
	p.openItem = false
}

// flush closes the accumulating paragraph, if any, and ends any
// still-filling item (a blank line or any marked line ends both).
func (p *parser) flush() {
	p.openItem = false
	if len(p.para) > 0 {
		p.add(Block{Kind: Para, Text: strings.Join(p.para, " ")})
		p.para = nil
	}
}

// collectUntilEnd gathers the lines after the block opener at
// lines[open] until a lone ".end", returning the body and the index
// of the .end line.
func collectUntilEnd(lines []string, open int, kind string) ([]string, int, error) {
	for j := open + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == ".end" {
			return lines[open+1 : j], j, nil
		}
	}
	return nil, 0, fmt.Errorf("%w: %s opened at line %d", ErrUnterminatedBlock, kind, open+1)
}

// parseTableBlock builds a TableBlk from a .table spec (with
// optional leading fixed width, then an optional "-" for a
// headerless table) and its |-separated rows. Unless "-" is given,
// the first non-empty row is the header. A table with neither
// header nor rows is an error, like an empty .quote.
func parseTableBlock(spec string, body []string, atLine int) (Block, error) {
	fixed := 0
	first, rest, _ := strings.Cut(spec, " ")
	if w, err := strconv.Atoi(first); err == nil {
		if w < 1 {
			return Block{}, fmt.Errorf("%w: .table width %q: want a positive integer (line %d)", ErrBadAttr, first, atLine)
		}
		if rest == "" {
			return Block{}, fmt.Errorf("%w: .table %s: width but no column spec (line %d)", ErrBadAttr, first, atLine)
		}
		fixed = w
		spec = rest
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
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// A ".." row is a half-size note annotating the row above
		// (the header, when no data row precedes it); a "=" row is
		// a total row, bold under a rule. Neither ever becomes the
		// header itself.
		if rest, ok := strings.CutPrefix(trimmed, ".."); ok {
			tbl.Note(splitCells(rest)...)
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "="); ok {
			tbl.Total(splitCells(rest)...)
			continue
		}
		cells := splitCells(trimmed)
		if header {
			tbl.Header(cells...)
			header = false
		} else {
			tbl.Row(cells...)
		}
	}
	if tbl.header == nil && len(tbl.rows) == 0 {
		return Block{}, fmt.Errorf("%w: empty .table (line %d)", ErrBadAttr, atLine)
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
