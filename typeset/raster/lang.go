package raster

import (
	"fmt"
	"strconv"
	"strings"
)

// Compile turns source (RASTER.t, "Authoring") into a page of the
// geometry: the source paints a canvas, and the canvas is encoded.
// Errors carry the 1-based source line. Compilation is reproducible:
// the same source and geometry yield the same bytes. A caller that
// compiles repeatedly keeps a Canvas and uses its Compile instead.
func Compile(g Geometry, src string) (*Page, error) {
	c := NewCanvas(g)
	if err := c.Compile(src); err != nil {
		return nil, err
	}
	p := New(g)
	if err := c.encodeInto(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Compile resets the canvas and paints the source onto it. The bytes
// come from Encode or EncodeInto; the renderers read the canvas
// directly. Errors carry the 1-based source line.
func (c *Canvas) Compile(src string) error {
	c.Reset()
	k := compiler{canvas: c}
	src = strings.TrimSuffix(src, "\n")
	for n := 1; ; n++ {
		raw, rest, more := strings.Cut(src, "\n")
		k.n = n
		if err := k.line(raw); err != nil {
			return fmt.Errorf("line %d: %w", n, err)
		}
		if !more {
			return nil
		}
		src = rest
	}
}

// encodeInto is EncodeInto with errors attributed to the source line
// that last painted the failing row.
func (c *Canvas) encodeInto(p *Page) error {
	for panel := range c.Panels {
		for row := range c.Rows {
			if err := encodeRow(c.Row(panel, row), p.Row(panel, row)); err != nil {
				return fmt.Errorf("line %d: raster: panel %d row %d: %w", c.rowLine[panel*c.Rows+row], panel, row, err)
			}
		}
	}
	return nil
}

type compiler struct {
	canvas *Canvas
	n      int // the current source line

	panel  int
	margin int
	pen    Ink
	curRow int // the cursor: the next run lands here, at the margin

	penRow, penCol int  // just past the last run ("+" continues there)
	atCol          int  // the column of a pending .at, else the margin
	havePen        bool // false after .panel and .at
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
	switch {
	case strings.HasPrefix(raw, "+ "):
		return c.continuation(raw[1:])
	case len(raw) > 1 && raw[0] == '.' && raw[1] >= 'a' && raw[1] <= 'z':
		return c.command(raw)
	default:
		return c.content(raw)
	}
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
	g := c.canvas.Geometry
	if raw == ".rem" || strings.HasPrefix(raw, ".rem ") {
		return nil
	}
	cmd, a, err := parse(raw)
	if err != nil {
		return err
	}
	var n [4]int
	switch cmd {
	case ".panel":
		if _, err := a.ints(n[:], 1, 1); err != nil {
			return err
		}
		if n[0] < 0 || n[0] >= g.Panels {
			return fmt.Errorf("raster: panel %d out of range 0..%d", n[0], g.Panels-1)
		}
		c.panel = n[0]
		c.curRow, c.atCol = 0, c.margin
		c.havePen = false
		return nil
	case ".margin":
		if _, err := a.ints(n[:], 1, 1); err != nil {
			return err
		}
		if n[0] < 0 || n[0] >= g.Cols {
			return fmt.Errorf("raster: margin %d outside columns 0..%d", n[0], g.Cols-1)
		}
		c.margin, c.atCol = n[0], n[0]
		return nil
	case ".at":
		have, err := a.ints(n[:], 1, 2)
		if err != nil {
			return err
		}
		col := c.margin
		if have == 2 {
			col = n[1]
		}
		if n[0] < 0 || n[0] >= g.Rows || col < 0 || col >= g.Cols {
			return fmt.Errorf("raster: .at %d %d outside rows 0..%d, cols 0..%d", n[0], col, g.Rows-1, g.Cols-1)
		}
		c.curRow, c.atCol = n[0], col
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
		have, err := a.ints(n[:], 1, 4)
		if err != nil {
			return err
		}
		row, col, rows, cols := n[0], 0, 1, 0
		if have > 1 {
			col = n[1]
		}
		if have > 2 {
			rows = n[2]
		}
		if have > 3 {
			cols = n[3]
		} else {
			cols = g.Cols - col
		}
		return c.fill(row, col, rows, cols)
	}
	return fmt.Errorf("raster: unknown command %s (.panel .margin .at .fg .bg .fill .rem)", cmd)
}

// paint places a run's cells at (row, col) in the pen's ink: leading
// spaces position and paint nothing, the rest is painted.
func (c *compiler) paint(row, col int, cells []byte) error {
	g := c.canvas.Geometry
	lead := 0
	for lead < len(cells) && cells[lead] == ' ' {
		lead++
	}
	if end := col + len(cells); end > g.Cols {
		return fmt.Errorf("raster: row %d: %d cells at column %d overflow the row", row, len(cells), col)
	}
	r := c.canvas.Row(c.panel, row)
	for i, b := range cells[lead:] {
		r[col+lead+i] = Cell{Glyph: b, Ink: c.pen}
	}
	c.canvas.rowLine[c.panel*g.Rows+row] = c.n
	c.penRow, c.penCol, c.havePen = row, col+len(cells), true
	return nil
}

// transcode is Transcode into the canvas's scratch buffer.
func (c *compiler) transcode(text string) ([]byte, error) {
	cells, err := AppendTranscode(c.canvas.scratch[:0], text)
	c.canvas.scratch = cells[:0]
	return cells, err
}

func (c *compiler) content(raw string) error {
	// Right-trim: invisible trailing spaces must not clobber
	// neighbours; clearing is .fill's explicit job.
	raw = strings.TrimRight(raw, " \t")
	col := c.atCol
	c.atCol = c.margin
	if raw == "" {
		c.curRow++ // an empty line, or one of only spaces, flows one row
		return nil
	}
	cells, err := c.transcode(raw)
	if err != nil {
		return err
	}
	if c.curRow >= c.canvas.Rows {
		return fmt.Errorf("raster: content below row %d", c.canvas.Rows-1)
	}
	if err := c.paint(c.curRow, col, cells); err != nil {
		return err
	}
	c.curRow++
	return nil
}

func (c *compiler) continuation(rest string) error {
	if !c.havePen {
		return fmt.Errorf("raster: + with nothing to continue (.panel and .at begin anew)")
	}
	rest = strings.TrimRight(rest, " \t")
	if rest == "" {
		return fmt.Errorf("raster: empty continuation")
	}
	cells, err := c.transcode(rest)
	if err != nil {
		return err
	}
	return c.paint(c.penRow, c.penCol, cells)
}

// fill paints a region of spaces in the pen's ink, over anything:
// clearing is fill's job.
func (c *compiler) fill(row, col, rows, cols int) error {
	g := c.canvas.Geometry
	if rows < 1 || cols < 1 || row < 0 || col < 0 || row+rows > g.Rows || col+cols > g.Cols {
		return fmt.Errorf("raster: .fill %d %d %d %d outside the panel", row, col, rows, cols)
	}
	for y := row; y < row+rows; y++ {
		r := c.canvas.Row(c.panel, y)
		for x := col; x < col+cols; x++ {
			r[x] = Cell{Glyph: ' ', Ink: c.pen}
		}
		c.canvas.rowLine[c.panel*g.Rows+y] = c.n
	}
	return nil
}
