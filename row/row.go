package row

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Row is a row's items in order. Decode and Parse fill it with the Go
// values of ROW.t section 8: nil, bool, int64, Decimal, float64, Day,
// time.Time (UTC), TimeOfDay, Duration, string, []byte and Row.
type Row []any

// Decimal is the exact number Mantissa x 10^-Scale. The scale is part
// of the value: 90.00 is {9000, 2}, 90.0 is {900, 1}.
type Decimal struct {
	Mantissa int64
	Scale    int
}

// Day is a date: days since 1970-01-01, proleptic Gregorian.
type Day int64

// TimeOfDay is a time of day: milliseconds after midnight, no zone.
type TimeOfDay int64

// Duration is a duration in milliseconds. Not time.Duration: that
// counts nanoseconds in an int64 and so ends at about 292 years, where
// a row's durations reach ±(2^53-1) ms. Encode takes a time.Duration
// of whole milliseconds too.
type Duration int64

// Error is a failure with its ROW.t code (R, T or V), the path of
// the item at fault (nil when there is none), and where it was found:
// Offset in bytes (-1 when not in bytes), Line in text (0 when not in
// text).
type Error struct {
	Code   string
	Path   []int
	Offset int
	Line   int
	Msg    string
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("row: ")
	b.WriteString(e.Code)
	if e.Path != nil {
		b.WriteString(" at ")
		b.WriteString(pathString(e.Path))
	}
	if e.Line > 0 {
		b.WriteString(", line ")
		b.WriteString(strconv.Itoa(e.Line))
	} else if e.Offset >= 0 {
		b.WriteString(", byte ")
		b.WriteString(strconv.Itoa(e.Offset))
	}
	b.WriteString(": ")
	b.WriteString(e.Msg)
	return b.String()
}

func errAt(code string, path []int, format string, a ...any) *Error {
	return &Error{Code: code, Path: clonePath(path), Offset: -1, Msg: fmt.Sprintf(format, a...)}
}

func clonePath(p []int) []int {
	if p == nil {
		return nil
	}
	return append([]int(nil), p...)
}

func pathString(p []int) string {
	var b strings.Builder
	for i, n := range p {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(strconv.Itoa(n))
	}
	return b.String()
}

// The tags (ROW.t sections 3 and 5).
const (
	maxShortString = 127
	tagShortBytes  = 0x80
	maxShortBytes  = 63
	tagNone        = 0xC0
	tagFalse       = 0xC1
	tagTrue        = 0xC2
	tagInt         = 0xC3
	tagFloat       = 0xC4
	tagDate        = 0xC5
	tagDatetime    = 0xC6
	tagTime        = 0xC7
	tagDuration    = 0xC8
	tagLongString  = 0xC9
	tagLongBytes   = 0xCA
	tagLongRow     = 0xCB
	tagDecimal     = 0xD0 // to 0xDF, the scale in the low nibble
	tagShortRow    = 0xE0 // to 0xEF, the count in the low nibble
	maxShortRow    = 15
)

// The ranges (ROW.t sections 4 and 5).
const (
	maxInt      = 1<<53 - 1
	maxScale    = 15
	minDay      = -719162
	maxDay      = 2932896
	minDatetime = -62135596800000
	maxDatetime = 253402300799999
	maxTime     = 86399999
	maxVarint   = 8
	maxDepth    = 4
)

// typeName is a decoded value's name in the tag registry, "" for a
// value that is not one.
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "none"
	case bool:
		return "bool"
	case int64:
		return "int"
	case Decimal:
		return "dec"
	case float64:
		return "f64"
	case Day:
		return "date"
	case time.Time:
		return "utc"
	case TimeOfDay:
		return "time"
	case Duration:
		return "dur"
	case string:
		return "str"
	case []byte:
		return "bin"
	case Row:
		return "row"
	}
	return ""
}

// norm checks a program's value against its type and returns it as
// the Go value Decode would give for it: the accepted variants (any
// integer type, float32, a time.Time in any zone, a time.Duration,
// []any) become the canonical ones. depth is the level of the row the
// value stands in, 1 for the value row.
func norm(v any, path []int, depth int) (any, error) {
	switch x := v.(type) {
	case nil, bool:
		return x, nil
	case int64:
		return checkInt(x, path)
	case int:
		return checkInt(int64(x), path)
	case int8:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case uint8:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint:
		if uint64(x) > maxInt {
			return nil, errAt("V001", path, "int %d outside ±(2^53-1)", x)
		}
		return int64(x), nil
	case uint64:
		if x > maxInt {
			return nil, errAt("V001", path, "int %d outside ±(2^53-1)", x)
		}
		return int64(x), nil
	case Decimal:
		if x.Scale < 0 || x.Scale > maxScale {
			return nil, errAt("V002", path, "decimal scale %d outside 0 to 15", x.Scale)
		}
		if x.Mantissa < -maxInt || x.Mantissa > maxInt {
			return nil, errAt("V002", path, "decimal mantissa %d outside ±(2^53-1)", x.Mantissa)
		}
		return x, nil
	case float64:
		return checkFloat(x, path)
	case float32:
		return checkFloat(float64(x), path)
	case Day:
		if x < minDay || x > maxDay {
			return nil, errAt("V004", path, "date %d days outside 0001-01-01 to 9999-12-31", x)
		}
		return x, nil
	case time.Time:
		if x.Nanosecond()%int(time.Millisecond) != 0 {
			return nil, errAt("V005", path, "datetime finer than a millisecond")
		}
		ms := x.UnixMilli()
		if ms < minDatetime || ms > maxDatetime {
			return nil, errAt("V005", path, "datetime outside 0001 to 9999")
		}
		return time.UnixMilli(ms).UTC(), nil
	case TimeOfDay:
		if x < 0 || x > maxTime {
			return nil, errAt("V006", path, "time %d ms outside one day", x)
		}
		return x, nil
	case Duration:
		if x < -maxInt || x > maxInt {
			return nil, errAt("V007", path, "duration %d ms outside ±(2^53-1)", x)
		}
		return x, nil
	case time.Duration:
		if x%time.Millisecond != 0 {
			return nil, errAt("V007", path, "duration finer than a millisecond")
		}
		return Duration(x / time.Millisecond), nil
	case string:
		if !utf8.ValidString(x) {
			return nil, errAt("V008", path, "string not valid UTF-8")
		}
		return x, nil
	case []byte:
		return x, nil
	case Row:
		return normRow(x, path, depth)
	case []any:
		return normRow(Row(x), path, depth)
	}
	return nil, errAt("V009", path, "%T has no ROW type", v)
}

func checkInt(n int64, path []int) (any, error) {
	if n < -maxInt || n > maxInt {
		return nil, errAt("V001", path, "int %d outside ±(2^53-1)", n)
	}
	return n, nil
}

func checkFloat(f float64, path []int) (any, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || (f == 0 && math.Signbit(f)) {
		return nil, errAt("V003", path, "float %v: NaN, infinite or -0", f)
	}
	return f, nil
}

// normRow normalises a row inside a row at depth (the value row, at
// depth 1, is normalised by normValue).
func normRow(r Row, path []int, depth int) (any, error) {
	if depth >= maxDepth {
		return nil, errAt("R005", path, "rows nested deeper than four levels")
	}
	return normItems(r, path, depth+1)
}

func normItems(r Row, path []int, depth int) (Row, error) {
	out := make(Row, len(r))
	p := append(clonePath(path), 0)
	for i, v := range r {
		p[len(p)-1] = i
		n, err := norm(v, p, depth)
		if err != nil {
			return nil, err
		}
		out[i] = n
	}
	return out, nil
}

// Encode returns the bytes of r (ROW.t sections 2 to 5), checking
// every value first.
func Encode(r Row) ([]byte, error) {
	n, err := normItems(r, nil, 1)
	if err != nil {
		return nil, err
	}
	out := appendVarint(nil, uint64(len(n)))
	for _, v := range n {
		out = appendItem(out, v)
	}
	return out, nil
}

func appendVarint(b []byte, v uint64) []byte {
	var buf [10]byte
	i := len(buf) - 1
	buf[i] = byte(v & 0x7F)
	for v >>= 7; v != 0; v >>= 7 {
		v--
		i--
		buf[i] = 0x80 | byte(v&0x7F)
	}
	return append(b, buf[i:]...)
}

func zigzag(n int64) uint64   { return uint64(n<<1) ^ uint64(n>>63) }
func unzigzag(u uint64) int64 { return int64(u>>1) ^ -int64(u&1) }

// appendItem writes a normalised value.
func appendItem(b []byte, v any) []byte {
	switch x := v.(type) {
	case nil:
		return append(b, tagNone)
	case bool:
		if x {
			return append(b, tagTrue)
		}
		return append(b, tagFalse)
	case int64:
		return appendVarint(append(b, tagInt), zigzag(x))
	case Decimal:
		return appendVarint(append(b, tagDecimal|byte(x.Scale)), zigzag(x.Mantissa))
	case float64:
		return binary.LittleEndian.AppendUint64(append(b, tagFloat), math.Float64bits(x))
	case Day:
		return appendVarint(append(b, tagDate), zigzag(int64(x)))
	case time.Time:
		return appendVarint(append(b, tagDatetime), zigzag(x.UnixMilli()))
	case TimeOfDay:
		return appendVarint(append(b, tagTime), uint64(x))
	case Duration:
		return appendVarint(append(b, tagDuration), zigzag(int64(x)))
	case string:
		if len(x) <= maxShortString {
			b = append(b, byte(len(x)))
		} else {
			b = appendVarint(append(b, tagLongString), uint64(len(x)-maxShortString-1))
		}
		return append(b, x...)
	case []byte:
		if len(x) <= maxShortBytes {
			b = append(b, tagShortBytes|byte(len(x)))
		} else {
			b = appendVarint(append(b, tagLongBytes), uint64(len(x)-maxShortBytes-1))
		}
		return append(b, x...)
	case Row:
		if len(x) <= maxShortRow {
			b = append(b, tagShortRow|byte(len(x)))
		} else {
			b = appendVarint(append(b, tagLongRow), uint64(len(x)-maxShortRow-1))
		}
		for _, item := range x {
			b = appendItem(b, item)
		}
		return b
	}
	panic(fmt.Sprintf("row: appendItem of unnormalised %T", v))
}

// Decode returns the row b holds; an error if b breaks ROW.t, or holds
// bytes after its row (R002).
func Decode(b []byte) (Row, error) {
	d := decoder{b: b}
	r, err := d.row(nil, 1, -1)
	if err != nil {
		return nil, err
	}
	if d.i != len(b) {
		return nil, &Error{Code: "R002", Offset: d.i, Msg: fmt.Sprintf("%d bytes after the row", len(b)-d.i)}
	}
	return r, nil
}

type decoder struct {
	b []byte
	i int
}

func (d *decoder) fail(code string, path []int, off int, format string, a ...any) *Error {
	return &Error{Code: code, Path: clonePath(path), Offset: off, Msg: fmt.Sprintf(format, a...)}
}

func (d *decoder) varint(path []int, off int) (uint64, error) {
	var v uint64
	for n := 0; ; n++ {
		if d.i >= len(d.b) {
			return 0, d.fail("R001", path, off, "the bytes end inside a varint")
		}
		if n == maxVarint {
			return 0, d.fail("R003", path, off, "a varint longer than 8 bytes")
		}
		c := d.b[d.i]
		d.i++
		if n == 0 {
			v = uint64(c & 0x7F)
		} else {
			v = (v+1)<<7 | uint64(c&0x7F)
		}
		if c&0x80 == 0 {
			return v, nil
		}
	}
}

// take returns the next n bytes.
func (d *decoder) take(n uint64, path []int, off int) ([]byte, error) {
	if n > uint64(len(d.b)-d.i) {
		return nil, d.fail("R001", path, off, "the bytes end inside a payload of %d", n)
	}
	s := d.b[d.i : d.i+int(n)]
	d.i += int(n)
	return s, nil
}

// row reads a row of count items. count < 0 means the value row, whose
// count is a varint at d.i.
func (d *decoder) row(path []int, depth int, count int64) (Row, error) {
	if count < 0 {
		c, err := d.varint(path, d.i)
		if err != nil {
			return nil, err
		}
		if c > maxInt {
			return nil, d.fail("V001", path, 0, "a count of %d outside 2^53-1", c)
		}
		count = int64(c)
	}
	// Every item takes a byte at least, so the bytes left bound what a
	// count may allocate; a count past them fails at its first missing
	// item.
	r := make(Row, 0, min(count, int64(len(d.b)-d.i)))
	p := append(clonePath(path), 0)
	for k := int64(0); k < count; k++ {
		p[len(p)-1] = int(k)
		v, err := d.item(p, depth)
		if err != nil {
			return nil, err
		}
		r = append(r, v)
	}
	return r, nil
}

func (d *decoder) zz(path []int, off int) (int64, error) {
	u, err := d.varint(path, off)
	if err != nil {
		return 0, err
	}
	return unzigzag(u), nil
}

// checked is a decoded value through norm, the one check of a value
// against its type, its error placed at the item's offset.
func (d *decoder) checked(v any, path []int, off int) (any, error) {
	n, err := norm(v, path, 1)
	if err != nil {
		e := err.(*Error)
		e.Offset = off
		return nil, e
	}
	return n, nil
}

func (d *decoder) item(path []int, depth int) (any, error) {
	off := d.i
	if d.i >= len(d.b) {
		return nil, d.fail("R001", path, off, "the bytes end before the item")
	}
	t := d.b[d.i]
	d.i++
	switch {
	case t <= maxShortString:
		return d.str(uint64(t), path, off)
	case t < tagNone:
		s, err := d.take(uint64(t-tagShortBytes), path, off)
		return append([]byte{}, s...), err
	case t >= tagDecimal && t < tagShortRow:
		m, err := d.zz(path, off)
		if err != nil {
			return nil, err
		}
		return d.checked(Decimal{Mantissa: m, Scale: int(t & 0x0F)}, path, off)
	case t >= tagShortRow && t <= tagShortRow+maxShortRow:
		return d.nested(uint64(t&0x0F), path, depth, off)
	}
	switch t {
	case tagNone:
		return nil, nil
	case tagFalse:
		return false, nil
	case tagTrue:
		return true, nil
	case tagInt, tagDate, tagDatetime, tagDuration:
		n, err := d.zz(path, off)
		if err != nil {
			return nil, err
		}
		var v any = n
		switch t {
		case tagDate:
			v = Day(n)
		case tagDatetime:
			v = time.UnixMilli(n)
		case tagDuration:
			v = Duration(n)
		}
		return d.checked(v, path, off)
	case tagFloat:
		s, err := d.take(8, path, off)
		if err != nil {
			return nil, err
		}
		return d.checked(math.Float64frombits(binary.LittleEndian.Uint64(s)), path, off)
	case tagTime:
		u, err := d.varint(path, off)
		if err != nil {
			return nil, err
		}
		return d.checked(TimeOfDay(u), path, off) // 8 varint bytes fit an int64
	case tagLongString:
		u, err := d.varint(path, off)
		if err != nil {
			return nil, err
		}
		return d.str(u+maxShortString+1, path, off)
	case tagLongBytes:
		u, err := d.varint(path, off)
		if err != nil {
			return nil, err
		}
		s, err := d.take(u+maxShortBytes+1, path, off)
		return append([]byte{}, s...), err
	case tagLongRow:
		u, err := d.varint(path, off)
		if err != nil {
			return nil, err
		}
		return d.nested(u+maxShortRow+1, path, depth, off)
	}
	return nil, d.fail("R004", path, off, "reserved tag 0x%02X", t)
}

func (d *decoder) str(n uint64, path []int, off int) (any, error) {
	s, err := d.take(n, path, off)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(s) {
		return nil, d.fail("V008", path, off, "string not valid UTF-8")
	}
	return string(s), nil
}

// nested reads a row of count items inside the row at depth.
func (d *decoder) nested(count uint64, path []int, depth int, off int) (any, error) {
	if depth >= maxDepth {
		return nil, d.fail("R005", path, off, "rows nested deeper than four levels")
	}
	if count > maxInt {
		return nil, d.fail("V001", path, off, "a count of %d outside 2^53-1", count)
	}
	return d.row(path, depth+1, int64(count))
}
