package tbl

import (
	"errors"
	"fmt"
)

// Error is a parse error at a source column, counted from one in code
// points as SPEC.t's conventions say; the host adds the line. Err is
// one of the kinds below, wrapped with detail, so errors.Is tells a
// host which kind it is.
type Error struct {
	Col int
	Err error
}

func (e *Error) Error() string { return fmt.Sprintf("column %d: %v", e.Col, e.Err) }

func (e *Error) Unwrap() error { return e.Err }

// The kinds of error, as SPEC.t lists them.
var (
	ErrCode           = errors.New("tbl: bad colour code")
	ErrToken          = errors.New("tbl: bad or misplaced token")
	ErrMixedWidths    = errors.New("tbl: widths on some columns and not others")
	ErrColumnCount    = errors.New("tbl: relative format with the wrong column count")
	ErrNarrowRelative = errors.New("tbl: narrowing in a relative format")
	ErrNoFull         = errors.New("tbl: relative format with no full format before it")
	ErrEmptyFull      = errors.New("tbl: full format with no columns")
	ErrSpan           = errors.New("tbl: bad S column")
	ErrAuto           = errors.New("tbl: more than one * column")
	ErrFit            = errors.New("tbl: columns do not fit the width")
	ErrMark           = errors.New("tbl: bad or repeated mark")
	ErrTarget         = errors.New("tbl: bad link target")
	ErrCells          = errors.New("tbl: more cells than groups")
	ErrNote           = errors.New("tbl: note row with no row above")
)

// errAt returns an Error of kind at col, with detail.
func errAt(col int, kind error, format string, args ...any) *Error {
	return &Error{Col: col, Err: fmt.Errorf("%w: "+format, append([]any{kind}, args...)...)}
}
