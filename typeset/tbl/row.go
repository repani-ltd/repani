package tbl

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"repani.com/typeset/raster"
	"repani.com/typeset/wrap"
)

// Kind is what a row line is.
type Kind uint8

const (
	Data  Kind = iota // cells
	Blank             // an empty line
	Rule              // ---
)

// Role is what a data row is, from its prefix.
type Role uint8

const (
	None   Role = iota // no prefix
	Header             // ^
	Total              // =
	Note               // ..
)

// Cell is one cell of a data row: its content, its colours from a :
// mark (each half set or unset), and its link target from an @ mark
// ("" for none). Col is the source column the cell starts at, after
// its leading spaces, for errors a host reports later.
type Cell struct {
	Text   string
	Code   Code
	Target string
	Col    int
}

// Row is a parsed row line, and the source line it was read from.
type Row struct {
	Kind  Kind
	Role  Role
	Cells []Cell
	Line  int
}

// trim trims spaces -- the breaker's breaking spaces, so a cell's
// words are the words the breaker sets -- from both ends of s and
// returns the trimmed string and the code points removed from its
// start.
func trim(s string) (string, int) {
	t := strings.TrimLeftFunc(s, wrap.IsBreakingSpace)
	lead := utf8.RuneCountInString(s[:len(s)-len(t)])
	return strings.TrimRightFunc(t, wrap.IsBreakingSpace), lead
}

// ParseRow reads a row's text, from source line line and column col.
func ParseRow(text string, line, col int) (Row, error) {
	r, err := parseRow(text, col)
	r.Line = line
	return r, onLine(err, line)
}

func parseRow(line string, col int) (Row, error) {
	t, lead := trim(line)
	switch t {
	case "":
		return Row{Kind: Blank}, nil
	case "---":
		return Row{Kind: Rule}, nil
	}
	col += lead
	r := Row{Kind: Data}
	switch {
	case strings.HasPrefix(t, ".."):
		r.Role, t, col = Note, t[2:], col+2
	case strings.HasPrefix(t, "="):
		r.Role, t, col = Total, t[1:], col+1
	case strings.HasPrefix(t, "^"):
		r.Role, t, col = Header, t[1:], col+1
	}
	for {
		i := strings.IndexByte(t, '|')
		part := t
		if i >= 0 {
			part = t[:i]
		}
		c, err := parseCell(part, col)
		if err != nil {
			return Row{}, err
		}
		r.Cells = append(r.Cells, c)
		if i < 0 {
			return r, nil
		}
		col += runes(t[:i+1])
		t = t[i+1:]
	}
}

// parseCell reads one cell's marks and content, s starting at source
// column col.
func parseCell(s string, col int) (Cell, error) {
	s, lead := trim(s)
	col += lead
	c := Cell{Col: col}
	var seenCode, seenLink bool
	for s != "" && (s[0] == ':' || s[0] == '@') {
		end := strings.IndexFunc(s, wrap.IsBreakingSpace)
		if end < 0 {
			end = len(s)
		}
		mark := s[:end]
		switch mark[0] {
		case ':':
			if seenCode {
				return Cell{}, errAt(0, col, ErrMark, "a second : in one cell")
			}
			code, err := parseCode(mark[1:], col+1)
			if err != nil {
				return Cell{}, err
			}
			c.Code, seenCode = code, true
		case '@':
			if seenLink {
				return Cell{}, errAt(0, col, ErrMark, "a second @ in one cell")
			}
			if err := checkTarget(mark[1:], col+1); err != nil {
				return Cell{}, err
			}
			c.Target, seenLink = mark[1:], true
		}
		col += runes(mark)
		s = s[end:]
		var skip int
		s, skip = trim(s)
		col += skip
	}
	// The content in NFC, its spaces collapsed as the breaker splits
	// words, so a clipped, a numeric and a wrapped cell show the same
	// text; and every code point one column, raster's rule, so a box
	// is as wide on a board as in a document.
	s = strings.Join(wrap.Fields(norm.NFC.String(s)), " ")
	if _, err := raster.Columns(s); err != nil {
		return Cell{}, errAt(0, col, ErrText, "%v", err)
	}
	c.Text = s
	return c, nil
}

// checkTarget reports whether t, starting at source column col, is a
// link target as raster defines one (raster.CheckTarget), at the
// column where it goes wrong.
func checkTarget(t string, col int) error {
	if t == "" {
		return errAt(0, col, ErrTarget, "@ with no target")
	}
	if at, err := raster.CheckTarget(t); err != nil {
		return errAt(0, col+runes(t[:at]), ErrTarget, "%v", err)
	}
	return nil
}
