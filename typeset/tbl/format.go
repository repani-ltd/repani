package tbl

import (
	"strconv"
	"strings"
)

// Column is one column of a resolved format.
type Column struct {
	Width int  // in code points; 0 for the auto column until fitted
	Auto  bool // the * column, which takes what the others leave
	Align byte // L, R, C, N, P or S
	Clip  bool // "!": clip instead of wrapping
	Code  Code // the column's colours, each half set or unset
}

// Format is a resolved format: what every row under it is laid out
// by. A relative format resolves to its full format with its changes
// applied, so a Format never refers to another.
type Format struct {
	Row    Code // the row code
	Narrow int  // the narrowing width, 0 for none
	Cols   []Column
}

// clone returns f with its own Cols.
func (f Format) clone() Format {
	f.Cols = append([]Column(nil), f.Cols...)
	return f
}

// Formats keeps the last full format and resolves each format line
// against it. The zero Formats has none.
type Formats struct {
	full *Format
}

// token is one space-separated token of a spec and the source column
// of its first code point.
type token struct {
	s   string
	col int
}

// tokens splits s, starting at source column col, at spaces and tabs.
func tokens(s string, col int) []token {
	var out []token
	start, startCol := -1, 0
	c := col
	for i, r := range s {
		if r == ' ' || r == '\t' {
			if start >= 0 {
				out = append(out, token{s[start:i], startCol})
				start = -1
			}
		} else if start < 0 {
			start, startCol = i, c
		}
		c++
	}
	if start >= 0 {
		out = append(out, token{s[start:], startCol})
	}
	return out
}

// colToken is a column token as written.
type colToken struct {
	hasWidth bool
	width    int
	auto     bool
	align    byte
	code     Code
	clip     bool
	col      int
}

// Apply reads a format line's spec, starting at source column col,
// and returns the format it resolves to and whether it is full. A
// full format becomes the one later relative formats resolve
// against.
func (f *Formats) Apply(spec string, col int) (Format, bool, error) {
	var (
		row               Code
		narrow, narrowCol int
		cols              []colToken
	)
	toks := tokens(spec, col)
	for i, t := range toks {
		b := t.s[0]
		switch {
		case b == '/' || (b >= 'a' && b <= 'z'):
			if i != 0 {
				return Format{}, false, errAt(t.col, ErrToken, "row code %q must be the first token", t.s)
			}
			c, err := ParseCode(t.s, t.col)
			if err != nil {
				return Format{}, false, err
			}
			row = c
		case isDigits(t.s):
			if narrow != 0 || len(cols) > 0 {
				return Format{}, false, errAt(t.col, ErrToken, "narrowing %s must come once, before the columns", t.s)
			}
			n, err := strconv.Atoi(t.s)
			if err != nil || n < 1 {
				return Format{}, false, errAt(t.col, ErrToken, "narrowing %s is not a positive width", t.s)
			}
			narrow, narrowCol = n, t.col
		case b == '*' || (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z'):
			ct, err := parseColumn(t)
			if err != nil {
				return Format{}, false, err
			}
			cols = append(cols, ct)
		default:
			return Format{}, false, errAt(t.col, ErrToken, "%s does not begin a token", quote(t.s, 0))
		}
	}

	widths := 0
	for _, c := range cols {
		if c.hasWidth {
			widths++
		}
	}
	if widths > 0 && widths < len(cols) {
		for _, c := range cols {
			if c.hasWidth != cols[0].hasWidth {
				return Format{}, false, errAt(c.col, ErrMixedWidths, "a full format gives every column a width, a relative one none")
			}
		}
	}
	for i, c := range cols {
		if c.align != 'S' {
			continue
		}
		switch {
		case i == 0:
			return Format{}, false, errAt(c.col, ErrSpan, "S joins the column on its left, and the first has none")
		case c.code.HasFG || c.code.HasBG || c.clip:
			return Format{}, false, errAt(c.col, ErrSpan, "S takes the style of the column it joins, so it carries no code or !")
		}
	}

	if len(cols) > 0 && widths == len(cols) {
		fm := Format{Row: row, Narrow: narrow}
		auto := false
		for _, c := range cols {
			if c.auto {
				if auto {
					return Format{}, false, errAt(c.col, ErrAuto, "one column takes the rest")
				}
				auto = true
			}
			fm.Cols = append(fm.Cols, Column{Width: c.width, Auto: c.auto, Align: c.align, Clip: c.clip, Code: c.code})
		}
		full := fm.clone()
		f.full = &full
		return fm, true, nil
	}

	switch {
	case f.full == nil:
		return Format{}, false, errAt(col, ErrNoFull, "a format without widths is relative to a full format, and none came before")
	case narrow != 0:
		return Format{}, false, errAt(narrowCol, ErrNarrowRelative, "the narrowing belongs to the full format")
	case len(cols) != 0 && len(cols) != len(f.full.Cols):
		return Format{}, false, errAt(cols[0].col, ErrColumnCount, "%d columns where the full format has %d; list every column or none", len(cols), len(f.full.Cols))
	}
	fm := f.full.clone()
	fm.Row = row.Over(f.full.Row)
	for i, c := range cols {
		fc := &fm.Cols[i]
		fc.Align, fc.Clip = c.align, c.clip
		fc.Code = c.code.Over(fc.Code)
	}
	return fm, false, nil
}

// parseColumn reads a column token: [width] align [code] ["!"].
func parseColumn(t token) (colToken, error) {
	ct := colToken{col: t.col}
	s := t.s
	i := 0
	switch {
	case s[0] == '*':
		ct.hasWidth, ct.auto = true, true
		i = 1
	case s[0] >= '0' && s[0] <= '9':
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		n, err := strconv.Atoi(s[:i])
		if err != nil || n < 1 {
			return ct, errAt(t.col, ErrToken, "width %s is not a positive width", s[:i])
		}
		ct.hasWidth, ct.width = true, n
	}
	if i == len(s) || !isAlign(s[i]) {
		at := t.col + runes(s[:i])
		if i == len(s) {
			return ct, errAt(at, ErrToken, "%q has no alignment (L R C N P S)", s)
		}
		return ct, errAt(at, ErrToken, "%s is not an alignment (L R C N P S)", quote(s, i))
	}
	ct.align = s[i]
	i++
	rest := s[i:]
	if n := len(rest); n > 0 && rest[n-1] == '!' {
		ct.clip = true
		rest = rest[:n-1]
	}
	if rest != "" {
		if j := strings.IndexByte(rest, '!'); j >= 0 {
			return ct, errAt(t.col+runes(s[:i+j]), ErrToken, "! must end the token")
		}
		c, err := ParseCode(rest, t.col+runes(s[:i]))
		if err != nil {
			return ct, err
		}
		ct.code = c
	}
	return ct, nil
}

func isAlign(b byte) bool {
	switch b {
	case 'L', 'R', 'C', 'N', 'P', 'S':
		return true
	}
	return false
}

func isDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
