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
	r := New()
	k := compiler{r: r, colRow: -1}
	src = strings.TrimSuffix(src, "\n")
	for n := 1; ; n++ {
		raw, rest, more := strings.Cut(src, "\n")
		k.n = n
		if err := k.line(raw); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if !more {
			if k.defining != nil {
				return nil, fmt.Errorf("line %d: raster: .def %s without .enddef", n, k.defining.name)
			}
			return r, nil
		}
		src = rest
	}
}

type compiler struct {
	r *Raster
	n int // the current source line

	pen    Ink
	curRow int // the cursor: the next run lands here, at column 0

	penRow, penCol int  // just past the last run ("+" continues there)
	atCol          int  // the column of a pending .at or .col, else 0
	colRow         int  // the row of a pending .col (the last run's), else -1
	havePen        bool // false after .at

	aliases  map[string]*alias // by name
	defining *alias            // the .def being collected, else nil
}

// An alias is a named body of lines with one slot, $NAME (RASTER.t,
// "Aliases"). The body is parsed when the definition closes: a
// command line is kept as written, a content line is split into
// literal text and the slot. A use fills the slot with its text as
// content, never as source.
type alias struct {
	name string
	ops  []aliasOp
	slot bool // the body uses $NAME
}

// An aliasOp is one body line: a command (raw), or content or a
// continuation as pieces, literal text and the slot.
type aliasOp struct {
	raw    string // a command line, else ""
	cont   bool   // a "+ " continuation
	pieces []piece
}

type piece struct {
	text string
	slot bool
}

func colorIndex(name string) (byte, error) {
	for i, n := range ColorNames {
		if n == name {
			return byte(i), nil
		}
	}
	return 0, fmt.Errorf("raster: unknown color %q (default red green yellow blue magenta cyan white)", name)
}

func (c *compiler) line(raw string) error {
	if c.defining != nil {
		if raw == ".enddef" {
			a := c.defining
			c.defining = nil
			c.aliases[a.name] = a
			return nil
		}
		return c.collect(raw)
	}
	switch {
	case strings.HasPrefix(raw, "+ "):
		return c.continuation(raw[1:])
	case len(raw) > 1 && raw[0] == '.' && raw[1] >= 'a' && raw[1] <= 'z':
		if a, ok := c.aliasOf(raw); ok {
			return c.expand(a, raw)
		}
		return c.command(raw)
	default:
		return c.content(raw)
	}
}

// commands is the closed set; an alias may not take one of its names.
var commands = map[string]bool{"at": true, "col": true, "fg": true, "bg": true, "fill": true, "rem": true, "def": true, "enddef": true}

// define begins collecting an alias: ".def NAME".
func (c *compiler) define(raw string) error {
	fields := strings.Fields(raw)[1:]
	if len(fields) != 1 {
		return fmt.Errorf("raster: .def wants a name and nothing else")
	}
	name := fields[0]
	if commands[name] {
		return fmt.Errorf("raster: .def %s: a command's name", name)
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
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
	switch {
	case strings.HasPrefix(raw, ".def ") || raw == ".def":
		return fmt.Errorf("raster: .def inside .def %s", a.name)
	case strings.HasPrefix(raw, "+ "):
		pieces := a.split(raw[2:])
		a.ops = append(a.ops, aliasOp{cont: true, pieces: pieces})
		return nil
	case len(raw) > 1 && raw[0] == '.' && raw[1] >= 'a' && raw[1] <= 'z':
		if raw == ".rem" || strings.HasPrefix(raw, ".rem ") {
			return nil
		}
		cmd, args, _ := strings.Cut(raw, " ")
		if _, ok := c.aliases[cmd[1:]]; ok {
			return fmt.Errorf("raster: .def %s: an alias inside an alias", a.name)
		}
		if !commands[cmd[1:]] {
			return fmt.Errorf("raster: .def %s: unknown command %s", a.name, cmd)
		}
		if cmd == ".at" {
			return fmt.Errorf("raster: .def %s: .at inside an alias (a body is relative)", a.name)
		}
		if cmd == ".fill" && strings.TrimSpace(args) != "" {
			return fmt.Errorf("raster: .def %s: .fill takes no arguments inside an alias (its own row)", a.name)
		}
		if strings.Contains(args, "$"+a.name) {
			return fmt.Errorf("raster: .def %s: $%s in a command (the slot fills content only)", a.name, a.name)
		}
		a.ops = append(a.ops, aliasOp{raw: raw})
		return nil
	default:
		a.ops = append(a.ops, aliasOp{pieces: a.split(raw)})
		return nil
	}
}

// split cuts a content line at its slots: "$NAME" where the next
// character is not a name character. Any other "$" is text.
func (a *alias) split(s string) []piece {
	var pieces []piece
	slot := "$" + a.name
	lit := ""
	for s != "" {
		i := strings.Index(s, slot)
		if i < 0 {
			break
		}
		end := i + len(slot)
		if end < len(s) && isNameChar(s[end]) {
			lit += s[:end]
			s = s[end:]
			continue
		}
		pieces = append(pieces, piece{text: lit + s[:i]}, piece{slot: true})
		lit = ""
		s = s[end:]
		a.slot = true
	}
	return append(pieces, piece{text: lit + s})
}

func isNameChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

// aliasOf returns the alias a line uses, if any.
func (c *compiler) aliasOf(raw string) (*alias, bool) {
	if len(raw) < 2 || raw[0] != '.' {
		return nil, false
	}
	name, _, _ := strings.Cut(raw[1:], " ")
	a, ok := c.aliases[name]
	return a, ok
}

// expand compiles a use: the text after the name and one space fills
// the slot, the body runs at the cursor, and the pen is restored
// after.
func (c *compiler) expand(a *alias, raw string) error {
	text := strings.TrimPrefix(raw[1+len(a.name):], " ")
	if text != "" && !a.slot {
		return fmt.Errorf("raster: .%s takes no text", a.name)
	}
	pen := c.pen
	defer func() { c.pen = pen }()
	for k, op := range a.ops {
		var err error
		switch {
		case op.raw != "":
			err = c.command(op.raw)
		default:
			var b strings.Builder
			for _, p := range op.pieces {
				if p.slot {
					b.WriteString(text)
				} else {
					b.WriteString(p.text)
				}
			}
			if op.cont {
				err = c.continuation(" " + b.String())
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

// args holds a command's arguments: at most four, space-separated.
type args struct {
	s [4]string
	n int
}

// parse splits a command line into its name and arguments.
func parse(raw string) (cmd string, a args, err error) {
	raw = strings.TrimSpace(raw)
	cmd, rest, _ := strings.Cut(raw, " ")
	for rest = strings.TrimSpace(rest); rest != ""; rest = strings.TrimSpace(rest) {
		if a.n == len(a.s) {
			return cmd, a, fmt.Errorf("raster: %s: too many arguments", cmd)
		}
		a.s[a.n], rest, _ = strings.Cut(rest, " ")
		a.n++
	}
	return cmd, a, nil
}

// ints parses min..max integer arguments into out.
func (a args) ints(out []int, min, max int) (int, error) {
	if a.n < min || a.n > max {
		if min == max {
			return 0, fmt.Errorf("raster: want %d arguments, have %d", min, a.n)
		}
		return 0, fmt.Errorf("raster: want %d to %d arguments, have %d", min, max, a.n)
	}
	for i := range a.n {
		n, err := strconv.Atoi(a.s[i])
		if err != nil {
			return 0, fmt.Errorf("raster: bad number %q", a.s[i])
		}
		out[i] = n
	}
	return a.n, nil
}

func (c *compiler) command(raw string) error {
	if raw == ".rem" || strings.HasPrefix(raw, ".rem ") {
		return nil
	}
	if raw == ".def" || strings.HasPrefix(raw, ".def ") {
		return c.define(raw)
	}
	if raw == ".enddef" {
		return fmt.Errorf("raster: .enddef without .def")
	}
	cmd, a, err := parse(raw)
	if err != nil {
		return err
	}
	var n [4]int
	switch cmd {
	case ".col":
		if _, err := a.ints(n[:], 1, 1); err != nil {
			return err
		}
		if !c.havePen {
			return fmt.Errorf("raster: .col with no run to attach to (.at begins anew)")
		}
		if n[0] < 0 || n[0] >= Cols {
			return fmt.Errorf("raster: .col %d outside columns 0..%d", n[0], Cols-1)
		}
		c.colRow, c.atCol = c.penRow, n[0]
		return nil
	case ".at":
		have, err := a.ints(n[:], 1, 2)
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
		c.havePen = false
		return nil
	case ".fg", ".bg":
		if a.n > 1 {
			return fmt.Errorf("raster: %s wants one color name, or none for default", cmd)
		}
		var i byte // bare: default
		if a.n == 1 {
			var err error
			if i, err = colorIndex(a.s[0]); err != nil {
				return err
			}
		}
		if cmd == ".fg" {
			c.pen.FG = i
		} else {
			c.pen.BG = i
		}
		return nil
	case ".fill":
		have, err := a.ints(n[:], 0, 4)
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
	return fmt.Errorf("raster: unknown command %s (.at .col .fg .bg .fill .rem .def .enddef)", cmd)
}

// paint places a run's cells at (row, col) in the pen's ink: leading
// spaces position and paint nothing, the rest is painted.
func (c *compiler) paint(row, col int, cells []byte) error {
	lead := 0
	for lead < len(cells) && cells[lead] == ' ' {
		lead++
	}
	if end := col + len(cells); end > Cols {
		return fmt.Errorf("raster: row %d: %d cells at column %d overflow the row", row, len(cells), col)
	}
	r := c.r.Row(row)
	for i, b := range cells[lead:] {
		r[col+lead+i] = Cell{Glyph: b, Ink: c.pen}
	}
	c.penRow, c.penCol, c.havePen = row, col+len(cells), true
	return nil
}

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
	if raw == "" {
		if !onLastRow {
			c.curRow++ // an empty line, or one of only spaces, flows one row
		}
		return nil
	}
	cells, err := Transcode(raw)
	if err != nil {
		return err
	}
	if row >= MaxRows {
		return fmt.Errorf("raster: content below row %d", MaxRows-1)
	}
	if err := c.paint(row, col, cells); err != nil {
		return err
	}
	if !onLastRow {
		c.curRow++
	}
	return nil
}

func (c *compiler) continuation(rest string) error {
	if !c.havePen {
		return fmt.Errorf("raster: + with nothing to continue (.at begins anew)")
	}
	rest = strings.TrimRight(rest, " \t")
	if rest == "" {
		return fmt.Errorf("raster: empty continuation")
	}
	cells, err := Transcode(rest)
	if err != nil {
		return err
	}
	return c.paint(c.penRow, c.penCol, cells)
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
