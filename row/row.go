package row

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"time"
	"unicode/utf8"
)

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
// a row's durations reach ±(2^53-1) ms, about 285,000 years. Encode
// takes a time.Duration too.
type Duration int64

// Std returns d as a time.Duration, and false when it is beyond
// time.Duration's range.
func (d Duration) Std() (time.Duration, bool) {
	const limit = int64(math.MaxInt64 / int64(time.Millisecond))
	if int64(d) > limit || int64(d) < -limit {
		return 0, false
	}
	return time.Duration(d) * time.Millisecond, true
}

// Error is a row that breaks ROW.t: Code is its R or T code, Offset
// the byte offset of the item at fault (in the bytes for binary, in
// the line for text).
type Error struct {
	Code   string
	Offset int
	Msg    string
}

func (e *Error) Error() string {
	return "row: " + e.Code + " at " + strconv.Itoa(e.Offset) + ": " + e.Msg
}

func errf(code string, off int, format string, a ...any) *Error {
	return &Error{Code: code, Offset: off, Msg: fmt.Sprintf(format, a...)}
}

// The tags (ROW.t 2.3); 0 to maxShort are short strings, and the
// thirteen types take the rest, 243 to 255.
const maxShort = 242

const (
	tagNone = maxShort + 1 + iota
	tagFalse
	tagTrue
	tagInt
	tagDecimal
	tagFloat
	tagDate
	tagDatetime
	tagTime
	tagDuration
	tagBytes
	tagLongString
	tagList
)

// The ranges (ROW.t 2.2 and 3).
const (
	maxInt      = 1<<53 - 1
	maxScale    = 15
	minDay      = -719162
	maxDay      = 2932896
	minDatetime = -62135596800000
	maxDatetime = 253402300799999
	maxTime     = 86399999
	maxVarint   = 8
)

type kind uint8

const (
	kString kind = iota
	kNone
	kFalse
	kTrue
	kInt
	kDecimal
	kFloat
	kDate
	kDatetime
	kTime
	kDuration
	kBytes
	kList
)

// val is an item that has passed every rule: what Encode writes,
// Format prints, and Decode and Parse produce. n holds the int, the
// decimal's mantissa, and the date, datetime, time and duration.
type val struct {
	k     kind
	s     string
	n     int64
	scale int
	f     float64
	b     []byte
	list  []val
}

// fromGo checks a Go value against the rules and returns it as a val.
// off is the byte offset the item would have, for errors.
func fromGo(v any, inList bool, off int) (val, error) {
	switch x := v.(type) {
	case nil:
		return val{k: kNone}, nil
	case bool:
		if x {
			return val{k: kTrue}, nil
		}
		return val{k: kFalse}, nil
	case string:
		if !utf8.ValidString(x) {
			return val{}, errf("R006", off, "string is not valid UTF-8")
		}
		return val{k: kString, s: x}, nil
	case int:
		return intVal(int64(x), off)
	case int64:
		return intVal(x, off)
	case Decimal:
		if x.Scale < 0 || x.Scale > maxScale {
			return val{}, errf("R004", off, "decimal scale %d is outside 0 to 15", x.Scale)
		}
		if !inRange(x.Mantissa) {
			return val{}, errf("R003", off, "decimal mantissa %d is outside ±(2^53-1)", x.Mantissa)
		}
		return val{k: kDecimal, n: x.Mantissa, scale: x.Scale}, nil
	case float64:
		if err := checkFloat(x, off); err != nil {
			return val{}, err
		}
		return val{k: kFloat, f: x}, nil
	case Day:
		if x < minDay || x > maxDay {
			return val{}, errf("R003", off, "day %d is outside 0001-01-01 to 9999-12-31", x)
		}
		return val{k: kDate, n: int64(x)}, nil
	case time.Time:
		if x.Nanosecond()%int(time.Millisecond) != 0 {
			return val{}, errf("R003", off, "datetime %v has a fraction of a millisecond", x)
		}
		ms := x.UnixMilli()
		if ms < minDatetime || ms > maxDatetime {
			return val{}, errf("R003", off, "datetime %v is outside years 0001 to 9999", x)
		}
		return val{k: kDatetime, n: ms}, nil
	case TimeOfDay:
		if x < 0 || x > maxTime {
			return val{}, errf("R003", off, "time of day %d ms is outside 0 to 86399999", x)
		}
		return val{k: kTime, n: int64(x)}, nil
	case Duration:
		if !inRange(int64(x)) {
			return val{}, errf("R003", off, "duration %d ms is outside ±(2^53-1)", x)
		}
		return val{k: kDuration, n: int64(x)}, nil
	case time.Duration:
		if x%time.Millisecond != 0 {
			return val{}, errf("R003", off, "duration %v has a fraction of a millisecond", x)
		}
		return val{k: kDuration, n: int64(x / time.Millisecond)}, nil // always within ±(2^53-1)
	case []byte:
		return val{k: kBytes, b: x}, nil
	case []any:
		if inList {
			return val{}, errf("R008", off, "a list holds a list")
		}
		l := val{k: kList, list: make([]val, len(x))}
		for i, it := range x {
			var err error
			if l.list[i], err = fromGo(it, true, off); err != nil {
				return val{}, err
			}
		}
		return l, nil
	}
	return val{}, fmt.Errorf("row: %T is not a row type", v)
}

func intVal(n int64, off int) (val, error) {
	if !inRange(n) {
		return val{}, errf("R003", off, "int %d is outside ±(2^53-1)", n)
	}
	return val{k: kInt, n: n}, nil
}

func inRange(n int64) bool { return n >= -maxInt && n <= maxInt }

func checkFloat(f float64, off int) error {
	switch {
	case math.IsNaN(f):
		return errf("R005", off, "float is NaN")
	case math.IsInf(f, 0):
		return errf("R005", off, "float is infinite")
	case f == 0 && math.Signbit(f):
		return errf("R005", off, "float is -0")
	}
	return nil
}

// toGo is the Go value Decode and Parse return for a val.
func (v val) toGo() any {
	switch v.k {
	case kString:
		return v.s
	case kNone:
		return nil
	case kFalse:
		return false
	case kTrue:
		return true
	case kInt:
		return v.n
	case kDecimal:
		return Decimal{Mantissa: v.n, Scale: v.scale}
	case kFloat:
		return v.f
	case kDate:
		return Day(v.n)
	case kDatetime:
		return time.UnixMilli(v.n).UTC()
	case kTime:
		return TimeOfDay(v.n)
	case kDuration:
		return Duration(v.n)
	case kBytes:
		return v.b
	}
	out := make([]any, len(v.list))
	for i, it := range v.list {
		out[i] = it.toGo()
	}
	return out
}

// Encode returns a row's bytes. Items are Go values of the row types:
// nil (none), bool, string, int or int64, Decimal, float64, Day,
// time.Time (datetime), TimeOfDay, Duration or time.Duration, []byte
// and []any (list). A value outside its type's rules is an *Error with the
// R code Decode would give its bytes.
func Encode(items []any) ([]byte, error) {
	var b []byte
	for _, it := range items {
		v, err := fromGo(it, false, len(b))
		if err != nil {
			return nil, err
		}
		b = v.append(b)
	}
	return b, nil
}

func (v val) append(b []byte) []byte {
	switch v.k {
	case kString:
		if len(v.s) <= maxShort {
			return append(append(b, byte(len(v.s))), v.s...)
		}
		b = binary.AppendUvarint(append(b, tagLongString), uint64(len(v.s)))
		return append(b, v.s...)
	case kNone:
		return append(b, tagNone)
	case kFalse:
		return append(b, tagFalse)
	case kTrue:
		return append(b, tagTrue)
	case kInt:
		return binary.AppendUvarint(append(b, tagInt), zigzag(v.n))
	case kDecimal:
		return binary.AppendUvarint(append(b, tagDecimal, byte(v.scale)), zigzag(v.n))
	case kFloat:
		return binary.LittleEndian.AppendUint64(append(b, tagFloat), math.Float64bits(v.f))
	case kDate:
		return binary.AppendUvarint(append(b, tagDate), zigzag(v.n))
	case kDatetime:
		return binary.AppendUvarint(append(b, tagDatetime), zigzag(v.n))
	case kTime:
		return binary.AppendUvarint(append(b, tagTime), uint64(v.n))
	case kDuration:
		return binary.AppendUvarint(append(b, tagDuration), zigzag(v.n))
	case kBytes:
		b = binary.AppendUvarint(append(b, tagBytes), uint64(len(v.b)))
		return append(b, v.b...)
	}
	b = binary.AppendUvarint(append(b, tagList), uint64(len(v.list)))
	for _, it := range v.list {
		b = it.append(b)
	}
	return b
}

func zigzag(n int64) uint64   { return uint64(n<<1) ^ uint64(n>>63) }
func unzigzag(u uint64) int64 { return int64(u>>1) ^ -int64(u&1) }

// Decode returns a row's items as the Go values Encode takes (int as
// int64, a datetime in UTC). A row that breaks a rule of ROW.t
// section 4 is an *Error with its R code and the offset of the item
// at fault.
func Decode(b []byte) ([]any, error) {
	vals, err := decodeVals(b)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v.toGo()
	}
	return out, nil
}

func decodeVals(b []byte) ([]val, error) {
	d := decoder{b: b}
	var out []val
	for d.pos < len(b) {
		v, err := d.item(false)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

type decoder struct {
	b   []byte
	pos int
}

func (d *decoder) left() int { return len(d.b) - d.pos }

// uvarint reads a varint of the item starting at start.
func (d *decoder) uvarint(start int) (uint64, error) {
	var u uint64
	for i := 0; ; i++ {
		if i == maxVarint {
			return 0, errf("R002", start, "a varint is longer than 8 bytes")
		}
		if d.pos >= len(d.b) {
			return 0, errf("R001", start, "a varint runs past the end")
		}
		c := d.b[d.pos]
		d.pos++
		u |= uint64(c&0x7f) << (7 * i)
		if c < 0x80 {
			if c == 0 && i > 0 {
				return 0, errf("R002", start, "a varint is longer than its shortest form")
			}
			return u, nil
		}
	}
}

// signed reads a zigzag varint within ±(2^53-1).
func (d *decoder) signed(start int, what string) (int64, error) {
	u, err := d.uvarint(start)
	if err != nil {
		return 0, err
	}
	if u > 2*maxInt {
		return 0, errf("R003", start, "%s is outside ±(2^53-1)", what)
	}
	return unzigzag(u), nil
}

// length reads a length or count and checks it fits what is left.
func (d *decoder) length(start int, what string) (int, error) {
	u, err := d.uvarint(start)
	if err != nil {
		return 0, err
	}
	if u > maxInt {
		return 0, errf("R003", start, "%s %d is outside 2^53-1", what, u)
	}
	if u > uint64(d.left()) {
		return 0, errf("R001", start, "%s %d runs past the end", what, u)
	}
	return int(u), nil
}

func (d *decoder) str(start, n int) (val, error) {
	s := d.b[d.pos : d.pos+n]
	d.pos += n
	if !utf8.Valid(s) {
		return val{}, errf("R006", start, "string is not valid UTF-8")
	}
	return val{k: kString, s: string(s)}, nil
}

func (d *decoder) item(inList bool) (val, error) {
	start := d.pos
	tag := d.b[d.pos]
	d.pos++
	switch {
	case tag <= maxShort:
		if int(tag) > d.left() {
			return val{}, errf("R001", start, "string of %d bytes runs past the end", tag)
		}
		return d.str(start, int(tag))
	case tag == tagNone:
		return val{k: kNone}, nil
	case tag == tagFalse:
		return val{k: kFalse}, nil
	case tag == tagTrue:
		return val{k: kTrue}, nil
	case tag == tagInt:
		n, err := d.signed(start, "int")
		return val{k: kInt, n: n}, err
	case tag == tagDecimal:
		if d.left() < 1 {
			return val{}, errf("R001", start, "decimal runs past the end")
		}
		scale := int(d.b[d.pos])
		d.pos++
		if scale > maxScale {
			return val{}, errf("R004", start, "decimal scale %d is above 15", scale)
		}
		n, err := d.signed(start, "decimal mantissa")
		return val{k: kDecimal, n: n, scale: scale}, err
	case tag == tagFloat:
		if d.left() < 8 {
			return val{}, errf("R001", start, "float runs past the end")
		}
		f := math.Float64frombits(binary.LittleEndian.Uint64(d.b[d.pos:]))
		d.pos += 8
		if err := checkFloat(f, start); err != nil {
			return val{}, err
		}
		return val{k: kFloat, f: f}, nil
	case tag == tagDate:
		n, err := d.signed(start, "date")
		if err == nil && (n < minDay || n > maxDay) {
			err = errf("R003", start, "day %d is outside 0001-01-01 to 9999-12-31", n)
		}
		return val{k: kDate, n: n}, err
	case tag == tagDatetime:
		n, err := d.signed(start, "datetime")
		if err == nil && (n < minDatetime || n > maxDatetime) {
			err = errf("R003", start, "datetime %d ms is outside years 0001 to 9999", n)
		}
		return val{k: kDatetime, n: n}, err
	case tag == tagTime:
		u, err := d.uvarint(start)
		if err == nil && u > maxTime {
			err = errf("R003", start, "time of day %d ms is outside 0 to 86399999", u)
		}
		return val{k: kTime, n: int64(u)}, err
	case tag == tagDuration:
		n, err := d.signed(start, "duration")
		return val{k: kDuration, n: n}, err
	case tag == tagBytes:
		n, err := d.length(start, "bytes length")
		if err != nil {
			return val{}, err
		}
		b := append([]byte{}, d.b[d.pos:d.pos+n]...)
		d.pos += n
		return val{k: kBytes, b: b}, nil
	case tag == tagLongString:
		n, err := d.length(start, "string length")
		if err != nil {
			return val{}, err
		}
		if n <= maxShort {
			return val{}, errf("R007", start, "a long string of %d bytes could be a short one", n)
		}
		return d.str(start, n)
	}
	// tagList
	if inList {
		return val{}, errf("R008", start, "a list holds a list")
	}
	n, err := d.length(start, "list count")
	if err != nil {
		return val{}, err
	}
	l := val{k: kList, list: make([]val, n)}
	for i := range n {
		if d.pos >= len(d.b) {
			return val{}, errf("R001", start, "list of %d items runs past the end", n)
		}
		if l.list[i], err = d.item(true); err != nil {
			return val{}, err
		}
	}
	return l, nil
}
