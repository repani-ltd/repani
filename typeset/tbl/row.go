package tbl

import "strings"

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

// Cell is one cell of a data row: its content, its colours from a ~
// mark (each half set or unset), and its link target from an @ mark
// ("" for none).
type Cell struct {
	Text   string
	Code   Code
	Target string
}

// Row is a parsed row line.
type Row struct {
	Kind  Kind
	Role  Role
	Cells []Cell
}

// MaxTarget is the longest link target, in bytes.
const MaxTarget = 255

// isSpace reports whether b is a space as SPEC.t defines it.
func isSpace(b byte) bool { return b == ' ' || b == '\t' }

// trim trims spaces and tabs from both ends of s and returns the
// trimmed string and the code points removed from its start.
func trim(s string) (string, int) {
	i := 0
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	j := len(s)
	for j > i && isSpace(s[j-1]) {
		j--
	}
	return s[i:j], i
}

// ParseRow reads a row line, starting at source column col.
func ParseRow(line string, col int) (Row, error) {
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
	var c Cell
	s, lead := trim(s)
	col += lead
	var seenCode, seenLink bool
	for s != "" && (s[0] == '~' || s[0] == '@') {
		end := 0
		for end < len(s) && !isSpace(s[end]) {
			end++
		}
		mark := s[:end]
		switch mark[0] {
		case '~':
			if seenCode {
				return Cell{}, errAt(col, ErrMark, "a second ~ in one cell")
			}
			code, err := ParseCode(mark[1:], col+1)
			if err != nil {
				return Cell{}, err
			}
			c.Code, seenCode = code, true
		case '@':
			if seenLink {
				return Cell{}, errAt(col, ErrMark, "a second @ in one cell")
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
	c.Text = s
	return c, nil
}

// checkTarget reports whether t, starting at source column col, is a
// link target: 1 to MaxTarget bytes of a URI reference as RFC 3986
// writes it, in ASCII from its unreserved and reserved sets, anything
// else percent-encoded.
func checkTarget(t string, col int) error {
	if t == "" {
		return errAt(col, ErrTarget, "@ with no target")
	}
	if len(t) > MaxTarget {
		return errAt(col, ErrTarget, "target of %d bytes, more than %d", len(t), MaxTarget)
	}
	for i := 0; i < len(t); i++ {
		switch b := t[i]; {
		case b == '%':
			if i+2 >= len(t) || !isHex(t[i+1]) || !isHex(t[i+2]) {
				return errAt(col+runes(t[:i]), ErrTarget, "%% not followed by two hex digits")
			}
			i += 2
		case !isURIByte(b):
			return errAt(col+runes(t[:i]), ErrTarget, "%s is not allowed in a URI reference; percent-encode it", quote(t, i))
		}
	}
	return nil
}

func isHex(b byte) bool {
	return '0' <= b && b <= '9' || 'a' <= b && b <= 'f' || 'A' <= b && b <= 'F'
}

// isURIByte reports whether b is in RFC 3986's unreserved or
// reserved sets.
func isURIByte(b byte) bool {
	switch {
	case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9':
		return true
	}
	return strings.IndexByte("-._~:/?#[]@!$&'()*+,;=", b) >= 0
}
