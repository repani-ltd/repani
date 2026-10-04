package raster

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/width"
)

// Width is the number of columns in every row.
const Width = 40

// MaxRow is the highest row number a row record can carry; the byte
// above it, FF, begins a row count.
const MaxRow = 254

// countKind is the first byte of a row count record.
const countKind = 0xFF

// MaxTarget is the longest link target, in bytes.
const MaxTarget = 255

// Color is an index into the palette of eight. Default is the
// theme's text colour as a foreground and its page as a background.
type Color uint8

const (
	Default Color = iota
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	White
)

// numColors bounds the palette; values from it to 255 are reserved.
const numColors = 8

var colorNames = [numColors]string{"default", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}

func (c Color) String() string {
	if c < numColors {
		return colorNames[c]
	}
	return fmt.Sprintf("Color(%d)", uint8(c))
}

// Segment is a stretch of a row, columns Start to End (exclusive), in
// one foreground and one background, and a link to Target unless
// Target is "". Text is its End - Start code points. A link is
// always exactly one segment.
type Segment struct {
	Start, End int
	Text       string
	FG, BG     Color
	Target     string
}

// IsLink reports whether s is a link.
func (s Segment) IsLink() bool { return s.Target != "" }

// Role says what a row is, from a closed registry that grows by
// appending. It is advisory: a row's text and colours are complete
// without it, and a renderer treats a value it does not know as
// None.
type Role uint8

const (
	None   Role = iota
	Header      // the labels of the rows below
	Total       // a row that sums the rows above
)

var roleNames = [...]string{"none", "header", "total"}

func (r Role) String() string {
	if int(r) < len(roleNames) {
		return roleNames[r]
	}
	return fmt.Sprintf("Role(%d)", uint8(r))
}

// Record is one record of a raster: a Row or a Count.
type Record interface {
	isRecord()
}

// Row is a row record: row Index of the page, its Role, and its
// segments, which cover its Width columns in order.
type Row struct {
	Index    int
	Role     Role
	Segments []Segment
}

// Count is a row count record: it sets the page's height, removing
// rows from it on and growing a shorter page with blank rows.
type Count int

func (Row) isRecord()   {}
func (Count) isRecord() {}

// blankText is the text of a blank row.
var blankText = strings.Repeat(" ", Width)

// Blank returns the blank row at index: forty spaces in the default
// colours, with no link.
func Blank(index int) Row {
	return Row{Index: index, Segments: []Segment{{End: Width, Text: blankText}}}
}

// IsBlank reports whether r is blank. A row of spaces on another
// background, carrying a link or a role, is content.
func (r Row) IsBlank() bool {
	if r.Role != None || len(r.Segments) != 1 {
		return false
	}
	s := r.Segments[0]
	return s.FG == Default && s.BG == Default && !s.IsLink() && s.Text == blankText
}

// Text returns the row's Width code points.
func (r Row) Text() string {
	var b strings.Builder
	for _, s := range r.Segments {
		b.WriteString(s.Text)
	}
	return b.String()
}

// Check reports whether c is a valid row count.
func (c Count) Check() error {
	if c < 0 || c > 255 {
		return fmt.Errorf("raster: row count %d outside 0 to 255", int(c))
	}
	return nil
}

// Check reports whether r is a valid row in its one canonical form:
// segments cover the row in order; a segment with no visible glyph
// is in the default foreground; two plain segments that meet on one
// background differ in foreground, each holds a glyph, and the later
// starts on one; two links that meet differ in target.
func (r Row) Check() error {
	if r.Index < 0 || r.Index > MaxRow {
		return fmt.Errorf("raster: row %d outside 0 to %d", r.Index, MaxRow)
	}
	if err := r.check(); err != nil {
		return fmt.Errorf("raster: row %d: %w", r.Index, err)
	}
	return nil
}

func (r Row) check() error {
	if len(r.Segments) == 0 {
		return fmt.Errorf("no segments")
	}
	col := 0
	for i, s := range r.Segments {
		if s.Start != col || s.End <= s.Start || s.End > Width {
			return fmt.Errorf("segment %d: columns %d to %d do not continue from %d within the row", i, s.Start, s.End, col)
		}
		n, err := checkText(s.Text, s.Start)
		if err != nil {
			return fmt.Errorf("segment %d: text: %w", i, err)
		}
		if n != s.End-s.Start {
			return fmt.Errorf("segment %d: %d code points of text for %d columns", i, n, s.End-s.Start)
		}
		if err := checkColor(s.FG); err != nil {
			return fmt.Errorf("segment %d: foreground: %w", i, err)
		}
		if err := checkColor(s.BG); err != nil {
			return fmt.Errorf("segment %d: background: %w", i, err)
		}
		if s.IsLink() {
			if _, err := CheckTarget(s.Target); err != nil {
				return fmt.Errorf("segment %d: target: %w", i, err)
			}
		}
		if !hasGlyph(s.Text) && s.FG != Default {
			return fmt.Errorf("segment %d: no glyph, so its foreground must be default, not %s", i, s.FG)
		}
		if i > 0 {
			if err := checkMeet(r.Segments[i-1], s); err != nil {
				return fmt.Errorf("segment %d: %w", i, err)
			}
		}
		col = s.End
	}
	if col != Width {
		return fmt.Errorf("segments end at column %d, not %d", col, Width)
	}
	if text := r.Text(); !norm.NFC.IsNormalString(text) {
		return fmt.Errorf("text not in NFC")
	}
	return nil
}

// checkMeet checks the boundary between adjacent segments a and b. A
// boundary where the target or the background changes shows at its
// exact column. One where only the foreground changes does not, as a
// space shows no foreground, so it is pinned to the glyph that
// starts b, with a glyph in a.
func checkMeet(a, b Segment) error {
	switch {
	case a.IsLink() && b.IsLink():
		if a.Target == b.Target {
			return fmt.Errorf("adjoins a link to the same target")
		}
	case a.IsLink() || b.IsLink() || a.BG != b.BG:
	case a.FG == b.FG:
		return fmt.Errorf("same colours as the segment before it")
	case !hasGlyph(a.Text):
		return fmt.Errorf("follows a segment of spaces on the same background")
	case b.Text[0] == ' ':
		return fmt.Errorf("changes only the foreground but starts on a space")
	}
	return nil
}

// hasGlyph reports whether s holds anything but U+0020.
func hasGlyph(s string) bool {
	return strings.Trim(s, " ") != ""
}

func checkColor(c Color) error {
	if c >= numColors {
		return fmt.Errorf("colour %d is reserved", uint8(c))
	}
	return nil
}

// checkText returns the number of code points in s, text starting at
// column start, if each is valid UTF-8 taking one column. Errors name
// the row's column.
func checkText(s string, start int) (int, error) {
	n := 0
	for i, c := range s {
		if c == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(s[i:]); size == 1 {
				return 0, fmt.Errorf("column %d: invalid UTF-8", start+n)
			}
		}
		if err := checkColumn(c); err != nil {
			return 0, fmt.Errorf("column %d: %w", start+n, err)
		}
		n++
	}
	return n, nil
}

// checkColumn reports whether c takes exactly one column: no controls,
// no combining or zero-width code points, no line or paragraph
// separators, nothing East Asian wide or fullwidth.
func checkColumn(c rune) error {
	switch {
	case unicode.Is(unicode.Cc, c):
		return fmt.Errorf("control character %U", c)
	case unicode.In(c, unicode.Mn, unicode.Me, unicode.Cf):
		return fmt.Errorf("combining or zero-width code point %U", c)
	case unicode.In(c, unicode.Zl, unicode.Zp):
		return fmt.Errorf("line or paragraph separator %U", c)
	}
	switch width.LookupRune(c).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return fmt.Errorf("wide code point %U", c)
	}
	return nil
}

// CheckTarget reports whether t is a link target: 1 to MaxTarget
// bytes of a URI reference as RFC 3986 writes it, ASCII from its
// unreserved and reserved sets, anything else percent-encoded (the
// grammar's structure is the application's). On an error, at is the
// byte of t where it goes wrong, for a producer that reports a
// position.
func CheckTarget(t string) (at int, err error) {
	if len(t) == 0 || len(t) > MaxTarget {
		return 0, fmt.Errorf("%d bytes, not 1 to %d", len(t), MaxTarget)
	}
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case c == '%':
			if i+2 >= len(t) || !isHex(t[i+1]) || !isHex(t[i+2]) {
				return i, fmt.Errorf("%% not followed by two hex digits")
			}
			i += 2
		case !isURIChar(c):
			r, _ := utf8.DecodeRuneInString(t[i:])
			return i, fmt.Errorf("%q is not allowed in a URI reference; percent-encode it", r)
		}
	}
	return 0, nil
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// isURIChar reports whether c is in RFC 3986's unreserved or
// reserved sets.
func isURIChar(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return true
	}
	switch c {
	case '-', '.', '_', '~', // unreserved
		':', '/', '?', '#', '[', ']', '@', // gen-delims
		'!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=': // sub-delims
		return true
	}
	return false
}
