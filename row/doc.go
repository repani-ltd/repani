/*
Package row implements ROW: typed rows, a short sequence of typed
items encoded as bytes, each starting with a byte that says what it
is, and a line-per-item text for export, inspection and edits. This
comment is the reference (printed by row spec); the normative
standard is ROW.t in this directory, its reasons DESIGN.t.

# Rows

A value is one row: a varint count, then that many items. A row may
hold rows, three levels deep counting the value: the value's rows may
hold rows, and those may not. Empty bytes are not a row; the empty
row is 00. A row ends where its count says, so rows laid end to end
need no framing.

# Tags

	0x00-0x7F  short string   the tag is the length (0-127), then UTF-8
	0x80-0xBF  short bytes    tag-0x80 is the length (0-63), then bytes
	0xC0       none           an empty position
	0xC1 0xC2  false, true
	0xC3       int            zigzag varint
	0xC4       float          8 bytes binary64 little-endian; finite, never -0
	0xC5       date           zigzag varint: days since 1970-01-01
	0xC6       datetime       zigzag varint: UTC ms since the epoch
	0xC7       time           varint: ms since midnight
	0xC8       duration       zigzag varint: ms
	0xC9       long string    varint length-128, then UTF-8
	0xCA       long bytes     varint length-64, then bytes
	0xCB       long row       varint count-16, then the items
	0xD0-0xDF  decimal        zigzag varint mantissa; scale = tag & 0x0F
	0xE0-0xEF  short row      tag & 0x0F items (0-15), then the items
	0xCC-0xCF, 0xF0-0xFF      reserved

A value of a length or count a short form holds is always short. A
varint is bijective base-128, most significant group first, at most
8 bytes: every byte but the last has its high bit set, and each
continuation adds one before the next group shifts in (0 is 00, 127
is 7f, 128 is 80 00, 16511 is ff 7f, 16512 is 80 80 00). Zigzag stores
0, -1, 1, -2 as 0, 1, 2, 3. Every integer lies within ±(2^53-1).
Dates and datetimes run 0001 to 9999, proleptic Gregorian.

# Text

One item per line, path: type = value; none and row lines have no
value. A path counts positions from 0, a nested item's path being its
row's, a dot, and its position: 0, 3, 3.0, 3.0.2. Every nested row has
its own line, and its items follow:

	0: str = "invoice"
	1: date = 2026-11-21
	2: str = "EUR"
	3: row
	3.0: row
	3.0.0: str = "widget"
	3.0.1: int = 2
	3.0.2: dec = 9.99

Several rows are one text when every line begins with its row's
label, a string. Each row has a line of its own, the label with no
path, before its items; an empty row is that line alone:

	"g/X/start": row
	"g/X/start" 0: str = "start"
	"empty": row

Rows keep the order their labels first appear in. An unlabelled text
is one row, and the empty text is the empty row.

Output is canonical: single spaces, each row's lines in path order
(positions compared as numbers), one spelling per value. Input is
lenient: lines in any order, any spaces or tabs, blank lines, and any
spelling its type reads as the same value; never a changed value.

	type  canonical                         input also accepts
	none  (no value)
	row   (no value)
	bool  true false
	int   -7  0  1234  12_345               +7, leading zeros, -0
	dec   9.99  -0.05  0.00  12_345.50      +, leading zeros, .5, -0.00;
	      scale 0: 1500                     the digits after the point
	                                        are the scale; no exponent
	f64   9.99  0.001  12_345.6  3  0       any float literal (9.99e0,
	      1e21  -1.5e-7: shortest digits,   1E3, +1.5, .5, 5.); input
	      plain from 1e-6 up to 1e21        rounds to the nearest float
	date  2026-11-21
	utc   2026-11-21T09:30:00.000Z          no fraction or 1-3 digits,
	                                        t z lower case, a space for
	                                        T, an offset +02:00 (to UTC)
	time  09:30:00.000                      09:30, 09:30:00, 1-3 digits
	dur   1h30m15.25s  -0.5s  0s            90m, 1500ms, 1.5h when whole
	                                        ms; parts h m s ms in order
	str   "..." escaping " \ and controls   any JSON escape
	      below U+0020 (\n, \u001b)
	bin   0x00ff  0x (empty)                upper-case hex, 0X

Whole parts of five digits or more group in threes with an underscore
(int, dec, plain f64); input accepts an underscore between any two
digits of a number or a duration, never in dates and times. No
comments. Strings stay on one line.

# Errors

	R001  the bytes end inside a count, length, payload or varint
	R002  bytes after the row, where one row was expected
	R003  a varint longer than 8 bytes
	R004  a reserved tag
	R005  rows nested deeper than three levels
	T001  a line that is not one: no colon, a broken label, stray text
	T002  a path that is not one: leading zero, over three positions
	T003  an unknown type name
	T004  a value its type cannot read, a value on a none or row line,
	      or none where one is due
	T005  a row whose positions have a gap or a duplicate
	T006  an item whose row has no line, or whose row is another type
	T007  labelled and unlabelled lines in one text
	V001  an int outside ±(2^53-1)
	V002  a decimal scale above 15 or mantissa outside ±(2^53-1)
	V003  a float that is NaN, infinite or -0
	V004  a date outside 0001-01-01 to 9999-12-31
	V005  a datetime outside its range or finer than a millisecond
	V006  a time outside one day or finer than a millisecond
	V007  a duration outside ±(2^53-1) ms or finer than a millisecond
	V008  a string that is not valid UTF-8
	V009  a program's value with no ROW type
	S001  an item of another type than the reader asked for
	S002  the row ends where the reader asked for an item
	S003  items left where the reader expected the row's end

Every error carries the item's path when there is one, and the byte
offset (bytes) or line (text). V errors are the same wherever met:
decoding, parsing, or encoding a program's value, which is never
rounded or truncated to fit.

# Go and JavaScript

Encode, Decode, Format, Parse, FormatLabeled and ParseLabeled, and
Read for a reader; row.js (embedded as JS) exports encode, decode,
format, parse, formatLabeled, parseLabeled and reader, with no
imports. Values, Go then JavaScript:

	none  nil                       null
	bool  bool                      boolean
	int   int64 (any int type in)   number
	dec   Decimal{Mantissa, Scale}  Decimal(mantissa, scale)
	f64   float64 (float32 in)      Float(value)
	date  Day                       Day(days)
	utc   time.Time, UTC            Date
	time  TimeOfDay (ms)            TimeOfDay(ms)
	dur   Duration (ms)             Duration(ms)
	str   string                    string
	bin   []byte                    Uint8Array
	row   Row ([]any in)            Array

A reader takes a decoded row's items in order: Str Bin Int Dec F64
Bool Date UTC Time Dur, Row (a reader of the nested row), None (takes
a none and reports it), Peek (the next item's type name), More, End
(S003 if items are left). In Go the first error sticks and End
returns it; in JavaScript each read throws.

# CLI

	row check FILE      validate a text, labelled or not; warn if not canonical
	row fmt [-w] FILE   print the canonical text (-w: rewrite FILE)
	row encode          a one-row text on stdin to hex
	row decode          hex on stdin to the canonical text
	row spec            this reference
*/
package row
