/*
Package row implements ROW v1: typed rows, a short sequence of typed
values encoded as bytes, each starting with a byte that says what it
is. A row has no field names: what each position means is the program
reading it. This comment is the reference (printed by row spec); the
normative standard, with its decisions, is ROW.t in this directory.

# Items

Every row is a sequence of items to the end of its bytes. An item is a
tag byte and a payload:

	tag      type          payload
	0-241    short string  that many bytes of UTF-8 (the tag is the length)
	242      compressed    varint string length (242 and up), varint stream
	         string        length, then the lz4s.Compress stream of the UTF-8
	243      none          nothing (an optional position left empty)
	244      false         nothing
	245      true          nothing
	246      int           zigzag varint, within ±(2^53-1)
	247      decimal       scale byte 0-15, zigzag varint mantissa: mantissa x 10^-scale
	248      float         8 bytes IEEE 754 binary64 little-endian; finite, never -0
	249      date          zigzag varint: days since 1970-01-01, years 0001-9999
	250      datetime      zigzag varint: UTC ms since the epoch, years 0001-9999
	251      time          varint: ms since midnight, 0-86399999
	252      duration      zigzag varint: ms, within ±(2^53-1)
	253      bytes         varint length, then bytes
	254      long string   varint length (242 and up), then UTF-8
	255      list          varint count, then that many items, never a list

A varint is unsigned LEB128 in its shortest form, at most 8 bytes;
zigzag stores 0, -1, 1, -2 as 0, 1, 2, 3. The type set is closed: no
tag is spare.

A string has one form: short when it has at most 241 bytes; else
compressed when that item is shorter than the long one, that is when
the stream's length varint plus the stream is shorter than the
string; else long. The stream is exactly lz4s.Compress of the string
(repani.com/lz4s), never another stream that decompresses to it. The
encoder chooses; the text notation is the same for all three.

# Text notation

One spelling per row: items separated by single spaces, on one line.

	"EUR"                         string, JSON escapes for " \ and control characters only
	none  false  true
	-7                            int
	90.00  -0.05  1500.           decimal: always a dot, scale digits after it
	9.05e1  1e-3  0e0             float: always an exponent, shortest round-trip digits
	2026-10-08                    date
	2026-10-08T09:30:00.000Z      datetime
	09:30:00.000                  time
	1h30m15.25s  -0.5s  0s        duration
	0x00ff  0x                    bytes, lower-case hex
	["ben", 1]  []                list

Example, an iou expense as a row:

	"x" 2026-10-08 "EUR" 90.00 none "8FVC9G8F+6X" "pavlos" none "ben" ["pavlos", 1, "ben", 1] "taxi"

# Errors

	R001  an item's payload, length or count runs past the end
	R002  a varint longer than its shortest form, or than 8 bytes
	R003  an integer outside its range
	R004  a decimal scale above 15
	R005  a float that is NaN, infinite or -0
	R006  a string that is not valid UTF-8
	R007  a string not in its one form (short, else compressed if shorter, else long)
	R008  a list inside a list
	R009  a compressed string whose stream is not lz4s.Compress of its string
	T001  text that is no row
	T002  a row spelt other than canonically (the message gives the spelling)

Binary errors carry the byte offset of the item at fault, text errors
the column.

# Go and JavaScript

In Go, Encode, Decode, Format and Parse take and return []any of:
nil, bool, string, int64 (int accepted), Decimal, float64, Day,
time.Time (UTC on decode), TimeOfDay, Duration (milliseconds;
time.Duration accepted), []byte and []any. row.js (embedded as JS)
exports encode, decode, format and parse over: null, boolean, string,
number (an int), Decimal, Float, Day, Date (a datetime), TimeOfDay,
Duration, Uint8Array and Array; it imports ./lz4s.js, so serve
lz4s.JS() beside it. Both pass the same frozen vectors.

# CLI

	row check FILE   validate text rows, one per line; exit 0 means valid
	row encode       text rows on stdin to hex, one row per line
	row decode       hex rows on stdin to text, one row per line
	row spec         this reference
*/
package row
