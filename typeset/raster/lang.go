package raster

import (
	"fmt"
	"strconv"
	"strings"
)

// Compile turns source (RASTER.t, "Authoring") into a raster. Errors
// carry the 1-based source line. Compilation is reproducible: the
// same source yields the same raster.
func Compile(src string) (*Raster, error) {
	c := compiler{r: New(), colRow: -1}
	src = strings.TrimSuffix(src, "\n")
	for n := 1; ; n++ {
		raw, rest, more := strings.Cut(src, "\n")
		if err := c.line(raw); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if !more {
			if c.defining != nil {
				return nil, fmt.Errorf("line %d: raster: .def %s without .enddef", n, c.defining.name)
			}
			return c.r, nil
		}
		src = rest
	}
}

type compiler struct {
	r *Raster

	pen    Ink
	curRow int // the cursor: the next run lands here, at column 0

	penRow, penCol int  // just past the last run ("+" continues there)
	atCol          int  // the column of a pending .at or .col, else 0
	colRow         int  // the row of a pending .col (the last run's), else -1
	haveRun        bool // a run to attach to; false after .at

	aliases  map[string]*alias // by name
	defining *alias            // the .def being collected, else nil
}

// A line's kind (RASTER.t, "Authoring"): a command or alias use (a
// dot and a lowercase letter), a continuation ("+ "), or content.
type kind int

const (
	kindContent kind = iota
	kindCommand
	kindContinuation
)

// lex classifies a line and splits it: a command's name (with its
// dot) and the rest after one space; a continuation's run, which is
// everything after the "+"; content whole.
func lex(raw string) (k kind, name, rest string) {
	switch {
	case strings.HasPrefix(raw, "+ "):
		return kindContinuation, "", raw[1:]
	case len(raw) > 1 && raw[0] == '.' && raw[1] >= 'a' && raw[1] <= 'z':
		name, rest, _ = strings.Cut(raw, " ")
		return kindCommand, name, rest
	default:
		return kindContent, "", raw
	}
}

// commands is the closed set; an alias may not take one of its names.
var commands = map[string]bool{".at": true, ".col": true, ".fg": true, ".bg": true, ".fill": true, ".rem": true, ".def": true, ".enddef": true}

func (c *compiler) line(raw string) error {
	if c.defining != nil {
		return c.collect(raw)
	}
	k, name, rest := lex(raw)
	switch k {
	case kindContinuation:
		return c.continuation(rest)
	case kindCommand:
		if a, ok := c.aliases[name[1:]]; ok {
			return c.expand(a, rest)
		}
		return c.command(name, rest)
	default:
		return c.content(raw)
	}
}

func (c *compiler) command(name, rest string) error {
	switch name {
	case ".rem":
		return nil
	case ".def":
		return c.define(rest)
	case ".enddef":
		return fmt.Errorf("raster: .enddef without .def")
	case ".fg", ".bg":
		var i byte // bare: default
		if rest = strings.TrimSpace(rest); rest != "" {
			if strings.ContainsAny(rest, " \t") {
				return fmt.Errorf("raster: %s wants one color name, or none for default", name)
			}
			var err error
			if i, err = colorIndex(rest); err != nil {
				return err
			}
		}
		if name == ".fg" {
			c.pen.FG = i
		} else {
			c.pen.BG = i
		}
		return nil
	}
	var n [4]int
	switch name {
	case ".col":
		if _, err := ints(rest, n[:], 1, 1); err != nil {
			return err
		}
		if !c.haveRun {
			return fmt.Errorf("raster: .col with no run to attach to (.at begins anew)")
		}
		if n[0] < 0 || n[0] >= Cols {
			return fmt.Errorf("raster: .col %d outside columns 0..%d", n[0], Cols-1)
		}
		c.colRow, c.atCol = c.penRow, n[0]
		return nil
	case ".at":
		have, err := ints(rest, n[:], 1, 2)
		if err != nil {
			return err
		}
		col := 0
		if have == 2 {
			col = n[1]
		}
		if n[0] < 0 || n[0] >= MaxRows || col < 0 || col >= Cols {
			return fmt.Errorf("raster: .at %d %d outside rows 0..%d, cols 0..%d", n[0], col, MaxRows-1, Cols-1)
		}
		c.curRow, c.atCol, c.colRow = n[0], col, -1
		c.haveRun = false
		return nil
	case ".fill":
		have, err := ints(rest, n[:], 0, 4)
		if err != nil {
			return err
		}
		row, col, rows, cols := c.curRow, 0, 1, 0
		if have > 0 {
			row = n[0]
		}
		if have > 1 {
			col = n[1]
		}
		if have > 2 {
			rows = n[2]
		}
		if have > 3 {
			cols = n[3]
		} else {
			cols = Cols - col
		}
		return c.fill(row, col, rows, cols)
	}
	return fmt.Errorf("raster: unknown command %s (.at .col .fg .bg .fill .rem .def .enddef)", name)
}

// ints parses min..max space-separated integers from s into out.
func ints(s string, out []int, min, max int) (int, error) {
	n := 0
	for f := range strings.FieldsSeq(s) {
		if n == max {
			n++
			break
		}
		v, err := strconv.Atoi(f)
		if err != nil {
			return 0, fmt.Errorf("raster: bad number %q", f)
		}
		out[n] = v
		n++
	}
	if n < min || n > max {
		if min == max {
			return 0, fmt.Errorf("raster: want %d arguments, have %d", min, n)
		}
		return 0, fmt.Errorf("raster: want %d to %d arguments, have %d", min, max, n)
	}
	return n, nil
}

func colorIndex(name string) (byte, error) {
	for i, n := range ColorNames {
		if n == name {
			return byte(i), nil
		}
	}
	return 0, fmt.Errorf("raster: unknown color %q (%s)", name, strings.Join(ColorNames[:], " "))
}

// content paints a line at the cursor, or where a pending .at or .col
// put it, and moves the cursor down unless .col held it.
func (c *compiler) content(raw string) error {
	// Right-trim: invisible trailing spaces must not clobber
	// neighbours; clearing is .fill's explicit job.
	raw = strings.TrimRight(raw, " \t")
	row, col := c.curRow, c.atCol
	onLastRow := c.colRow >= 0 // a pending .col: the last run's row, cursor untouched
	if onLastRow {
		row = c.colRow
	}
	c.atCol, c.colRow = 0, -1
	if raw != "" {
		if row >= MaxRows {
			return fmt.Errorf("raster: content below row %d", MaxRows-1)
		}
		if err := c.paint(row, col, raw); err != nil {
			return err
		}
	}
	if !onLastRow {
		c.curRow++ // an empty line, or one of only spaces, flows one row too
	}
	return nil
}

// continuation paints text just past the last run, on its row.
func (c *compiler) continuation(text string) error {
	if !c.haveRun {
		return fmt.Errorf("raster: + with nothing to continue (.at begins anew)")
	}
	text = strings.TrimRight(text, " \t")
	if text == "" {
		return fmt.Errorf("raster: empty continuation")
	}
	return c.paint(c.penRow, c.penCol, text)
}

// paint places a run at (row, col) in the pen's ink: leading spaces
// position and paint nothing, the rest is painted.
func (c *compiler) paint(row, col int, text string) error {
	cells, err := Transcode(text)
	if err != nil {
		return err
	}
	if end := col + len(cells); end > Cols {
		return fmt.Errorf("raster: row %d: %d cells at column %d overflow the row", row, len(cells), col)
	}
	lead := 0
	for lead < len(cells) && cells[lead] == ' ' {
		lead++
	}
	r := c.r.Row(row)
	for i, b := range cells[lead:] {
		r[col+lead+i] = Cell{Glyph: b, Ink: c.pen}
	}
	c.penRow, c.penCol, c.haveRun = row, col+len(cells), true
	return nil
}

// fill paints a region of spaces in the pen's ink, over anything:
// clearing is fill's job.
func (c *compiler) fill(row, col, rows, cols int) error {
	if rows < 1 || cols < 1 || row < 0 || col < 0 || row+rows > MaxRows || col+cols > Cols {
		return fmt.Errorf("raster: .fill %d %d %d %d outside the raster", row, col, rows, cols)
	}
	for y := row; y < row+rows; y++ {
		r := c.r.Row(y)
		for x := col; x < col+cols; x++ {
			r[x] = Cell{Glyph: ' ', Ink: c.pen}
		}
	}
	return nil
}

// An alias is a named body of lines with one slot, $NAME (RASTER.t,
// "Aliases"). The body is parsed when the definition closes: a
// command line is kept as its name and rest, a content line or a
// continuation is split into literal text and the slot. A use fills
// the slot with its text as content, never as source.
type alias struct {
	name string
	ops  []aliasOp
	slot bool // the body uses $NAME
}

// An aliasOp is one body line.
type aliasOp struct {
	kind       kind
	name, rest string  // a command's
	pieces     []piece // content's or a continuation's
}

type piece struct {
	text string
	slot bool
}

// define begins collecting an alias: ".def NAME".
func (c *compiler) define(rest string) error {
	name := strings.TrimSpace(rest)
	if name == "" || strings.ContainsAny(name, " \t") {
		return fmt.Errorf("raster: .def wants a name and nothing else")
	}
	if commands["."+name] {
		return fmt.Errorf("raster: .def %s: a command's name", name)
	}
	for i := range len(name) {
		if !isNameChar(name[i]) {
			return fmt.Errorf("raster: .def %s: names are letters, digits and _", name)
		}
	}
	if c.aliases == nil {
		c.aliases = map[string]*alias{}
	}
	if _, dup := c.aliases[name]; dup {
		return fmt.Errorf("raster: .def %s: already defined", name)
	}
	c.defining = &alias{name: name}
	return nil
}

// collect parses one body line of the alias being defined.
func (c *compiler) collect(raw string) error {
	a := c.defining
	if raw == ".enddef" {
		c.defining = nil
		c.aliases[a.name] = a
		return nil
	}
	k, name, rest := lex(raw)
	if k != kindCommand {
		a.ops = append(a.ops, aliasOp{kind: k, pieces: a.split(rest)})
		return nil
	}
	switch {
	case name == ".rem":
		return nil
	case name == ".def":
		return fmt.Errorf("raster: .def inside .def %s", a.name)
	case c.aliases[name[1:]] != nil:
		return fmt.Errorf("raster: .def %s: an alias inside an alias", a.name)
	case !commands[name]:
		return fmt.Errorf("raster: .def %s: unknown command %s", a.name, name)
	case name == ".at":
		return fmt.Errorf("raster: .def %s: .at inside an alias (a body is relative)", a.name)
	case name == ".fill" && strings.TrimSpace(rest) != "":
		return fmt.Errorf("raster: .def %s: .fill takes no arguments inside an alias (its own row)", a.name)
	case strings.Contains(rest, "$"+a.name):
		return fmt.Errorf("raster: .def %s: $%s in a command (the slot fills content only)", a.name, a.name)
	}
	a.ops = append(a.ops, aliasOp{kind: kindCommand, name: name, rest: rest})
	return nil
}

// split cuts a line at its slots: "$NAME" where the next character is
// not a name character. Any other "$" is text.
func (a *alias) split(s string) []piece {
	var pieces []piece
	slot := "$" + a.name
	from := 0 // the start of the text not yet cut
	for i := 0; ; {
		j := strings.Index(s[i:], slot)
		if j < 0 {
			break
		}
		i += j + len(slot)
		if i < len(s) && isNameChar(s[i]) {
			continue
		}
		pieces = append(pieces, piece{text: s[from : i-len(slot)]}, piece{slot: true})
		from = i
		a.slot = true
	}
	return append(pieces, piece{text: s[from:]})
}

func isNameChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

// expand compiles a use: the text after the name and one space fills
// the slot, the body runs at the cursor, and the pen is restored
// after.
func (c *compiler) expand(a *alias, rest string) error {
	if rest != "" && !a.slot {
		return fmt.Errorf("raster: .%s takes no text", a.name)
	}
	pen := c.pen
	defer func() { c.pen = pen }()
	for k, op := range a.ops {
		var err error
		switch op.kind {
		case kindCommand:
			err = c.command(op.name, op.rest)
		default:
			var b strings.Builder
			for _, p := range op.pieces {
				if p.slot {
					b.WriteString(rest)
				} else {
					b.WriteString(p.text)
				}
			}
			if op.kind == kindContinuation {
				err = c.continuation(b.String())
			} else {
				err = c.content(b.String())
			}
		}
		if err != nil {
			return fmt.Errorf("%w (.%s line %d)", err, a.name, k+1)
		}
	}
	return nil
}
