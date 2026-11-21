package row

import (
	"fmt"
	"time"
)

// Reader reads a decoded row's items in order, each typed read taking
// the next item (ROW.t 8.1). Its first error sticks, shared with the
// readers of the rows within it: later reads return zero values, and
// End returns that first error.
type Reader struct {
	row  Row
	path []int
	i    int
	err  *error
}

// Read returns a reader of r's items.
func Read(r Row) *Reader {
	var err error
	return &Reader{row: r, err: &err}
}

func (rd *Reader) at() []int { return append(clonePath(rd.path), rd.i) }

func (rd *Reader) set(code, format string, a ...any) {
	if *rd.err == nil {
		*rd.err = errAt(code, rd.at(), format, a...)
	}
}

// next takes the next item if it is of type want.
func (rd *Reader) next(want string) any {
	if *rd.err != nil {
		return nil
	}
	if rd.i >= len(rd.row) {
		rd.set("S002", "the row ends where a %s was asked for", want)
		return nil
	}
	v := rd.row[rd.i]
	if got := typeName(v); got != want {
		rd.set("S001", "%s where a %s was asked for", orUnknown(got, v), want)
		return nil
	}
	rd.i++
	return v
}

func orUnknown(name string, v any) string {
	if name == "" {
		return fmt.Sprintf("%T", v)
	}
	return name
}

// Str takes a string.
func (rd *Reader) Str() string { s, _ := rd.next("str").(string); return s }

// Bin takes bytes.
func (rd *Reader) Bin() []byte { b, _ := rd.next("bin").([]byte); return b }

// Int takes an int.
func (rd *Reader) Int() int64 { n, _ := rd.next("int").(int64); return n }

// Dec takes a decimal.
func (rd *Reader) Dec() Decimal { d, _ := rd.next("dec").(Decimal); return d }

// F64 takes a float.
func (rd *Reader) F64() float64 { f, _ := rd.next("f64").(float64); return f }

// Bool takes a boolean.
func (rd *Reader) Bool() bool { b, _ := rd.next("bool").(bool); return b }

// Date takes a date.
func (rd *Reader) Date() Day { d, _ := rd.next("date").(Day); return d }

// UTC takes a datetime.
func (rd *Reader) UTC() time.Time { t, _ := rd.next("utc").(time.Time); return t }

// Time takes a time of day.
func (rd *Reader) Time() TimeOfDay { t, _ := rd.next("time").(TimeOfDay); return t }

// Dur takes a duration.
func (rd *Reader) Dur() Duration { d, _ := rd.next("dur").(Duration); return d }

// Row takes a row and returns a reader of its items, sharing this
// reader's error.
func (rd *Reader) Row() *Reader {
	p := rd.at()
	r, _ := rd.next("row").(Row)
	return &Reader{row: r, path: p, err: rd.err}
}

// None reports whether the next item is none, and takes it if so.
func (rd *Reader) None() bool {
	if *rd.err == nil && rd.i < len(rd.row) && rd.row[rd.i] == nil {
		rd.i++
		return true
	}
	return false
}

// Peek names the next item's type ("str", "dec", ...), "" at the row's
// end or after an error.
func (rd *Reader) Peek() string {
	if *rd.err != nil || rd.i >= len(rd.row) {
		return ""
	}
	return typeName(rd.row[rd.i])
}

// More reports whether an item is left (and no error has stuck).
func (rd *Reader) More() bool { return *rd.err == nil && rd.i < len(rd.row) }

// End returns the first error of this reader and of every reader that
// shares it, or S003 if items are left in this row.
func (rd *Reader) End() error {
	if *rd.err == nil && rd.i < len(rd.row) {
		rd.set("S003", "%d items left", len(rd.row)-rd.i)
	}
	return *rd.err
}
