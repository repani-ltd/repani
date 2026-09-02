package tessera

import (
	"fmt"
	"strconv"
	"strings"
)

// Compile turns source (doc.go, "Authoring") into a page. Errors carry
// the 1-based source line. Compilation is reproducible: the same source
// yields the same 3,808 bytes.
func Compile(src string) (*Page, error) {
	c := compiler{page: new(Page), panel: -1}
	for n, raw := range strings.Split(strings.TrimSuffix(src, "\n"), "\n") {
		if err := c.line(raw); err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
	}
	return c.page, nil
}

type compiler struct {
	page  *Page
	panel int
	pen   ink

	curRow, curCol int  // the cursor; content advances curRow
	penRow, penCol int  // just past the last emitted cells ("+" target)
	havePen        bool // false after .panel and .at
}

func colorIndex(name string) (byte, error) {
	for i, n := range ColorNames {
		if n == name {
			return byte(i), nil
		}
	}
	return 0, fmt.Errorf("tessera: unknown color %q", name)
}

func (c *compiler) line(raw string) error {
	switch {
	case strings.HasPrefix(raw, "+ "):
		return c.continuation(raw[2:])
	case raw == "+":
		return fmt.Errorf("tessera: empty continuation")
	case len(raw) > 1 && raw[0] == '.' && raw[1] >= 'a' && raw[1] <= 'z':
		return c.command(raw)
	default:
		// ". " and ".." begin ordinary content.
		return c.content(raw)
	}
}

func (c *compiler) command(raw string) error {
	fields := strings.Fields(raw)
	cmd, args := fields[0], fields[1:]
	switch cmd {
	case ".rem":
		return nil
	case ".panel":
		n, err := ints(args, 1)
		if err != nil {
			return err
		}
		if n[0] < 0 || n[0] >= Panels {
			return fmt.Errorf("tessera: panel %d out of range 0..%d", n[0], Panels-1)
		}
		c.panel = n[0]
		c.curRow, c.curCol = 0, 0
		c.havePen = false
		c.pen = ink{}
		return nil
	case ".at":
		n, err := ints(args, 2)
		if err != nil {
			return err
		}
		if n[0] < 0 || n[0] >= Rows || n[1] < 0 || n[1] >= Cols {
			return fmt.Errorf("tessera: .at %d %d outside rows 0..%d, cols 0..%d", n[0], n[1], Rows-1, Cols-1)
		}
		c.curRow, c.curCol = n[0], n[1]
		c.havePen = false
		return nil
	case ".ink":
		return c.setInk(args)
	case ".fill":
		n, err := ints(args, 4)
		if err != nil {
			return err
		}
		return c.fill(n[0], n[1], n[2], n[3])
	}
	return fmt.Errorf("tessera: unknown command %s", cmd)
}

func ints(args []string, want int) ([]int, error) {
	if len(args) != want {
		return nil, fmt.Errorf("tessera: want %d arguments, have %d", want, len(args))
	}
	out := make([]int, want)
	for i, a := range args {
		n, err := strconv.Atoi(a)
		if err != nil {
			return nil, fmt.Errorf("tessera: bad number %q", a)
		}
		out[i] = n
	}
	return out, nil
}

func (c *compiler) setInk(args []string) error {
	var fgName, bgName string
	switch {
	case len(args) == 1:
		fgName = args[0]
	case len(args) == 3 && args[1] == "on":
		fgName, bgName = args[0], args[2]
	default:
		return fmt.Errorf("tessera: .ink wants FG or FG on BG")
	}
	fg, err := colorIndex(fgName)
	if err != nil {
		return err
	}
	c.pen.fg = fg
	if bgName != "" {
		if c.pen.bg, err = colorIndex(bgName); err != nil {
			return err
		}
	}
	return nil
}

// emit places the codes the pen needs at (row, col), then cells after
// them, and moves the pen past the last cell. A cell of content never
// lands on an ink code; a code may replace a code.
func (c *compiler) emit(row, col int, cells []byte) error {
	if c.panel < 0 {
		return fmt.Errorf("tessera: content before .panel")
	}
	r := c.page.Row(c.panel, row)
	pre := codes(stateAt(r, col), c.pen)
	end := col + len(pre) + len(cells)
	if end > Cols {
		return fmt.Errorf("tessera: row %d: %d cells at column %d overflow the row", row, end-col, col)
	}
	for i := range cells {
		if IsInk(r[col+len(pre)+i]) {
			return fmt.Errorf("tessera: row %d column %d: content over an ink code", row, col+len(pre)+i)
		}
	}
	copy(r[col:], pre)
	copy(r[col+len(pre):], cells)
	c.penRow, c.penCol, c.havePen = row, end, true
	return nil
}

func (c *compiler) content(raw string) error {
	// Right-trim: invisible trailing spaces must not clobber
	// neighbours; clearing is .fill's explicit job.
	raw = strings.TrimRight(raw, " \t")
	if raw == "" {
		c.curRow++ // an empty line, or one of only spaces, flows one row
		return nil
	}
	cells, err := Transcode(raw)
	if err != nil {
		return err
	}
	if c.curRow >= Rows {
		return fmt.Errorf("tessera: content below row %d", Rows-1)
	}
	if err := c.emit(c.curRow, c.curCol, cells); err != nil {
		return err
	}
	c.curRow++
	return nil
}

func (c *compiler) continuation(rest string) error {
	if !c.havePen {
		return fmt.Errorf("tessera: + with nothing to continue (.panel and .at reset the pen)")
	}
	rest = strings.TrimRight(rest, " \t")
	if rest == "" {
		return fmt.Errorf("tessera: empty continuation")
	}
	cells, err := Transcode(rest)
	if err != nil {
		return err
	}
	return c.emit(c.penRow, c.penCol, cells)
}

// fill paints a W-by-H region of spaces in the pen's ink: on every row
// the codes the pen needs at the left edge, spaces to the right edge
// (over anything, codes included: clearing is fill's job), and at the
// right edge, when it is inside the row, the codes that restore what
// arrived there before.
func (c *compiler) fill(row, col, w, h int) error {
	if c.panel < 0 {
		return fmt.Errorf("tessera: .fill before .panel")
	}
	if w < 1 || h < 1 || row < 0 || col < 0 || row+h > Rows || col+w > Cols {
		return fmt.Errorf("tessera: .fill %d %d %d %d outside the panel", row, col, w, h)
	}
	for y := row; y < row+h; y++ {
		r := c.page.Row(c.panel, y)
		pre := codes(stateAt(r, col), c.pen)
		if len(pre) > w {
			return fmt.Errorf("tessera: row %d: a fill %d wide cannot hold its %d ink codes", y, w, len(pre))
		}
		var post []byte
		if col+w < Cols {
			post = codes(c.pen, stateAt(r, col+w))
			for i := range post {
				if x := col + w + i; x >= Cols || (r[x] != 0 && !IsInk(r[x])) {
					return fmt.Errorf("tessera: row %d: the fill's closing ink code at column %d lands on content", y, x)
				}
			}
		}
		copy(r[col:], pre)
		for x := col + len(pre); x < col+w; x++ {
			r[x] = ' '
		}
		copy(r[col+w:], post)
	}
	return nil
}
