package tbl

import (
	"errors"
	"fmt"
)

// Error is an error at a source line and column, both counted from
// one, the column in code points, as SPEC.t's conventions say: the
// position of a spec token, a cell or a mark, or of the format whose
// columns do not fit. Err is one of the kinds below, wrapped with
// detail, so errors.Is tells a host which kind it is.
type Error struct {
	Line, Col int
	Err       error
}

func (e *Error) Error() string { return fmt.Sprintf("line %d, column %d: %v", e.Line, e.Col, e.Err) }

func (e *Error) Unwrap() error { return e.Err }

// The kinds of error, as SPEC.t lists them.
var (
	ErrCode           = errors.New("tbl: bad colour code")
	ErrToken          = errors.New("tbl: bad or misplaced token")
	ErrMixedWidths    = errors.New("tbl: widths on some columns and not others")
	ErrColumnCount    = errors.New("tbl: relative format with the wrong column count")
	ErrNarrowRelative = errors.New("tbl: narrowing in a relative format")
	ErrNoFull         = errors.New("tbl: relative format with no full format before it")
	ErrSpan           = errors.New("tbl: bad S column")
	ErrAuto           = errors.New("tbl: more than one * column")
	ErrFit            = errors.New("tbl: columns do not fit the width")
	ErrMark           = errors.New("tbl: bad or repeated mark")
	ErrTarget         = errors.New("tbl: bad link target")
	ErrCells          = errors.New("tbl: more cells than groups")
	ErrNote           = errors.New("tbl: note row with no row above")
	ErrNumber         = errors.New("tbl: a number wider than its box")
	ErrText           = errors.New("tbl: cell text a grid cannot show")
)

// errAt returns an Error of kind at line and col, with detail. The
// scanners below the parse functions, which see one line, pass line
// 0; the parse function that called them sets it (onLine).
func errAt(line, col int, kind error, format string, args ...any) *Error {
	return &Error{Line: line, Col: col, Err: fmt.Errorf("%w: "+format, append([]any{kind}, args...)...)}
}

// onLine sets the line of err, an *Error from a scanner, and returns
// it.
func onLine(err error, line int) error {
	if e, ok := err.(*Error); ok {
		e.Line = line
	}
	return err
}
