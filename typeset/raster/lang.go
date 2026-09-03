package raster

import (
	"fmt"
	"strconv"
	"strings"
)

// Compile turns source (RASTER.t, "Authoring") into a page of the
// geometry: the source paints a canvas, and the canvas is encoded.
// Errors carry the 1-based source line. Compilation is reproducible:
// the same source and geometry yield the same bytes.
func Compile(g Geometry, src string) (*Page, error) {
	c := compiler{canvas: NewCanvas(g), lines: map[int]int{}}
	for n, raw := range strings.Split(strings.TrimSuffix(src, "\n"), "\n") {
		c.n = n + 1
		if err := c.line(raw); err != nil {
			return nil, fmt.Errorf("line %d: %w", c.n, err)
		}
	}
	p := New(g)
	for panel := range g.Panels {
		for row := range g.Rows {
			if err := encodeRow(c.canvas.Row(panel, row), p.Row(panel, row)); err != nil {
				return nil, fmt.Errorf("line %d: raster: panel %d row %d: %w", c.lines[panel*g.Rows+row], panel, row, err)
			}
		}
	}
	return p, nil
}

type compiler struct {
	canvas *Canvas
	n      int         // the current source line
	lines  map[int]int // panel*Rows+row -> the last line that painted it

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

func (c *compiler) command(raw string) error {
	g := c.canvas.Geometry
	fields := strings.Fields(raw)
	cmd, args := fields[0], fields[1:]
	switch cmd {
	case ".rem":
		return nil
	case ".panel":
		n, err := ints(args, 1, 1)
		if err != nil {
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
		n, err := ints(args, 1, 1)
		if err != nil {
			return err
		}
		if n[0] < 0 || n[0] >= g.Cols {
			return fmt.Errorf("raster: margin %d outside columns 0..%d", n[0], g.Cols-1)
		}
		c.margin, c.atCol = n[0], n[0]
		return nil
	case ".at":
		n, err := ints(args, 1, 2)
		if err != nil {
			return err
		}
		col := c.margin
		if len(n) == 2 {
			col = n[1]
		}
		if n[0] < 0 || n[0] >= g.Rows || col < 0 || col >= g.Cols {
			return fmt.Errorf("raster: .at %d %d outside rows 0..%d, cols 0..%d", n[0], col, g.Rows-1, g.Cols-1)
		}
		c.curRow, c.atCol = n[0], col
		c.havePen = false
		return nil
	case ".fg", ".bg":
		if len(args) != 1 {
			return fmt.Errorf("raster: %s wants one color name", cmd)
		}
		i, err := colorIndex(args[0])
		if err != nil {
			return err
		}
		if cmd == ".fg" {
			c.pen.FG = i
		} else {
			c.pen.BG = i
		}
		return nil
	case ".fill":
		n, err := ints(args, 1, 4)
		if err != nil {
			return err
		}
		row, col, rows, cols := n[0], 0, 1, 0
		if len(n) > 1 {
			col = n[1]
		}
		if len(n) > 2 {
			rows = n[2]
		}
		if len(n) > 3 {
			cols = n[3]
		} else {
			cols = g.Cols - col
		}
		return c.fill(row, col, rows, cols)
	}
	return fmt.Errorf("raster: unknown command %s (.panel .margin .at .fg .bg .fill .rem)", cmd)
}

// ints parses min..max integer arguments.
func ints(args []string, min, max int) ([]int, error) {
	if len(args) < min || len(args) > max {
		if min == max {
			return nil, fmt.Errorf("raster: want %d arguments, have %d", min, len(args))
		}
		return nil, fmt.Errorf("raster: want %d to %d arguments, have %d", min, max, len(args))
	}
	out := make([]int, len(args))
	for i, a := range args {
		n, err := strconv.Atoi(a)
		if err != nil {
			return nil, fmt.Errorf("raster: bad number %q", a)
		}
		out[i] = n
	}
	return out, nil
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
	c.lines[c.panel*g.Rows+row] = c.n
	c.penRow, c.penCol, c.havePen = row, col+len(cells), true
	return nil
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
	cells, err := Transcode(raw)
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
	cells, err := Transcode(rest)
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
		c.lines[c.panel*g.Rows+y] = c.n
	}
	return nil
}
