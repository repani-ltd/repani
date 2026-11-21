ROW — Typed Rows
.by Pavlos Christoforou
.date 2026-11-21
.rights All rights reserved © repani.com
.rem Normative, not yet frozen. Written from a blank page on 2026-11-21; ROW v1
.rem (frozen 2026-10-12) is retired whole to ~/repos/_attic/row and binds
.rem nothing here. The code in this directory implements this document. The
.rem reasons and the decision ledger are DESIGN.t.

A row is a short sequence of typed items, encoded as bytes. Each item starts
with one byte, its tag, that says what it is. There are no field names: what
each position means is the reading program's.

ROW is a generic encoder for values with some structure, the values of key-value
stores first (ASCON-KV): one value is one row. Nothing in it depends on any
store.

# 1. Principles

.item ONE ENCODING. Every value has exactly one encoding, by construction where
the layout allows it; a decoder rejects anything else, and never re-encodes to
check.
.item POSITIONAL. A row has no field names and no schema inside it.
.item SELF-CONTAINED. A row refers to nothing outside its own bytes: no schema,
no dictionary, no shared table. What its values mean to an application is the
application's.
.item SELF-DELIMITING. A row says where it ends, so rows laid end to end need no
other framing.
.item THREE LEVELS. A row may hold rows, and those may hold rows, which hold
none.
.item NO COMPRESSION. No item has a compressed form. Compression, where wanted,
belongs to whatever holds many rows.
.item APPEND-ONLY. A tag, once assigned, means the same forever. New types take
reserved tags; a decoder that predates one rejects its tag.
.item REFUSE, NEVER TRUNCATE. A value outside its type -- a datetime with
sub-millisecond digits, a decimal beyond its range or scale, an integer past
2^53-1 -- is an error in the encoder as in the decoder. No encoder rounds,
truncates or clamps a value to make it fit.

# 2. Rows

A row is a count, then that many items. A value is one row, its count a varint
and no tag before it. A row inside a row is an item: short (0xE0 to 0xEF) when
it has 0 to 15 items, the count being tag & 0x0F, then the items; else long
(0xCB), a varint count less 16, then the items. The empty row is the single byte
00 as a value and 0xE0 inside a row. Empty bytes are not a row: what an empty
value means to a store or an application (a key that exists with nothing in it,
a tombstone) is theirs.

A record's fixed tuple (a currency and an amount) is a row or plain positions; a
repeating group (the lines of an invoice, the sharers of an expense) is a row of
rows, and may be empty. An invoice is then one row: "invoice", a date, "EUR",
and a row of its lines, each a row of a name, a quantity and a price.

# 3. Tags

The two high bits of a tag give its class:

.fmt 76 11L 14L 5L *L
^tags | class | text | payload
0x00-0x7F | short string | str | tag bytes of UTF-8 (0 to 127)
0x80-0xBF | short bytes | bin | tag - 0x80 bytes (0 to 63)
0xC0-0xFF | typed | | the types of section 5, and reserved tags
.end

A string or bytes value of a length the short class holds is always short. The
long forms store the length less the first length the short class cannot hold,
128 for strings and 64 for bytes (a long string of 128 bytes stores 0), so a
short value has no long encoding and needs no rule against one.

# 4. Varints

A varint is bijective base-128, most significant group first, as git writes pack
offsets: every byte but the last has its high bit set, and each continuation
adds one before the next group is shifted in, so every value has exactly one
encoding. A decoder reads

.pre
v = b & 0x7F
while b & 0x80:
    b = next byte
    v = ((v + 1) << 7) | (b & 0x7F)
.end

and an encoder writes the last byte first:

.pre
last byte = v & 0x7F
v = v >> 7
while v != 0:
    v = v - 1
    byte before = 0x80 | (v & 0x7F)
    v = v >> 7
.end

.fmt 40 20R *L
^value | bytes
0 | 00
127 | 7f
128 | 80 00
255 | 80 7f
300 | 81 2c
16511 | ff 7f
16512 | 80 80 00
.end

One byte holds 0 to 127, two bytes to 16511, three to 2113663, eight to
72624976668147839. No varint is longer than 8 bytes.

## 4.1 Signed integers and their range

A signed integer n is stored as the varint of its zigzag form: 2n for n >= 0,
-2n-1 for n < 0, so 0, -1, 1, -2 become 0, 1, 2, 3. Every integer in a row,
signed or not, lies within -(2^53-1) to 2^53-1, the integers Go and JavaScript
both hold exactly; wider values are bytes. The zigzag form of the widest,
2^54-2, takes eight varint bytes.

# 5. Types

.fmt 76 10L 12L 5L *L
^tag | type | text | payload
0xC0 | none | none | nothing
0xC1 | false | bool | nothing
0xC2 | true | bool | nothing
0xC3 | int | int | zigzag varint
0xC4 | float | f64 | 8 bytes, IEEE 754 binary64, little-endian
0xC5 | date | date | zigzag varint: days since 1970-01-01
0xC6 | datetime | utc | zigzag varint: milliseconds since 1970-01-01T00:00:00Z
0xC7 | time | time | varint: milliseconds since midnight
0xC8 | duration | dur | zigzag varint: milliseconds
0xC9 | long string | str | varint length less 128, then that many bytes of UTF-8
0xCA | long bytes | bin | varint length less 64, then that many bytes
0xCB | long row | row | varint count less 16, then that many items
0xCC-0xCF | reserved | |
0xD0-0xDF | decimal | dec | zigzag varint mantissa; the scale is tag & 0x0F
0xE0-0xEF | short row | row | tag & 0x0F items (0 to 15)
0xF0-0xFF | reserved | |
.end

Forty-four tags are assigned and twenty reserved. The text column is each type's
name in the text of section 6: two to four lower-case letters or digits, one
name for all of a type's tags, the form (short or long, the scale) being the
encoding's, not the text's. A new type registers its tags and its name together,
and a name, like a tag, is never reused.

.term string
UTF-8 text, never normalised: the bytes written are the bytes read.
.term bytes
Opaque bytes: identifiers, hashes, keys.
.term none
No value: a position left empty. A row cannot skip a position, so an absent
value is written as none.
.term false, true
The two booleans.
.term int
An integer.
.term decimal
The exact number mantissa x 10^-scale. The scale is part of the value: 90.00
(scale 2) and 90.0 (scale 1) are different values that compare equal as numbers,
and a writer keeps the scale its data has -- for money, the currency's minor
digits.
.term float
A finite binary64 number. NaN, the infinities and -0 are errors; zero is +0. For
measurements; money is decimal.
.term date
A day of the proleptic Gregorian calendar, 0001-01-01 to 9999-12-31: days
-719162 to 2932896 from 1970-01-01.
.term datetime
An instant in UTC with millisecond precision, 0001-01-01T00:00:00.000Z to
9999-12-31T23:59:59.999Z: milliseconds -62135596800000 to 253402300799999 from
the epoch. No time zone and no leap seconds; local time is the program's, as a
date, a time and a zone name in three positions.
.term time
A time of day with millisecond precision and no zone: 0 to 86399999 milliseconds
after midnight.
.term duration
A signed length of time in milliseconds.
.term row
Items of any type (section 2), to the depth of THREE LEVELS.

# 6. Text

A row has a text form so that stored rows are never opaque: they are exported,
read, edited by hand and imported again without loss. It is not for writing rows
from nothing.

Text is one item per line: the item's path, its type, and its value. Strings
stay on their line, line breaks escaped.

## 6.1 Paths

A path is the item's position in its row, counted from 0; a nested item's path
is its row's path, a dot, and its position there: 0, 3, 3.0, 3.0.2. A path has
at most three positions (THREE LEVELS), no leading zeros and no spaces. A nested
row has a line of its own, its path and type and no value; its items follow on
lines of their own, and an empty row is that line alone. Every row's items are
its positions 0 to n-1 with none missing and none twice, and every item's row
has its line; anything else is an error.

## 6.2 Labels

A text with no labels is one row. Several rows -- a store's entries, a sequence
of rows -- are one text when every line begins with its row's label, a string
spelt as str values are, before the path. Each row has a line of its own, as a
nested row does: its label, no path, a colon and row. Its items follow, and an
empty row is that line alone:

.pre
"g/X/start": row
"g/X/start" 0: str = "start"
"g/X/start" 1: row
"g/X/start" 1.0: str = "raiders"
"m/0001": row
"m/0001" 0: str = "say"
"empty": row
.end

Lines with the same label are one row; a row without its own line is an error
(T006), as is a path-less line of any type but row (T002). An unlabelled text
has no such line: it is one row, and the empty text is the empty row. Rows keep
the order in which their labels first appear: a store's export writes its keys
in their order, a sequence its rows in theirs, labelled by position ("0", "1",
...) or by ids it already has. What a label means beyond naming its row is the
exporter's.

## 6.3 Canonical output, lenient input

Output is canonical: one spelling per value, each row's lines in path order
(positions compared as numbers, 3.2 before 3.10), and rows in their order, so
export then import gives back the same bytes. Input is lenient: a row's lines
may come in any order and anywhere in the text, a new label's first line placing
its row; and a value may be any spelling the line's type reads as the same
value, printed back canonically. Input never alters a value to make it fit
(REFUSE, NEVER TRUNCATE): a decimal's scale is part of its value, so 9.9 and
9.90 are not variants of each other. A float is the exception every float reader
makes: decimal digits have no exact binary64 value in general (0.1 has none), so
input takes the nearest, which is reading the value, not altering it.

## 6.4 Lines

A line is its label (in a text of several rows), its path (none for a labelled
row's own line), a colon, its type name, and, unless the type is none or row, an
equals sign and its value. Canonical lines have single spaces: "k": row, "k"
1.0: str = "x", or 1.0: str = "x". Input accepts any run of spaces or tabs
around the label, the colon and the equals sign, and after the value. Blank
lines are ignored and never written. There are no comments.

## 6.5 Values

A number's whole part of five digits or more is written in groups of three with
an underscore, from the right: in an int, a decimal and a float in plain
notation (12_345, 12_345.50, but 1234 and 1234.50). Nothing else is grouped in
output: not a fraction, not an exponent, not a duration. Input accepts a single
underscore between two digits anywhere in an int, a decimal, a float or a
duration, and none; never in a date, a time or a datetime, whose digits are
fixed fields.

.fmt 76 6L 34P *P
^name | canonical | input also accepts
none | no value | --
row | no value | --
bool | true, false | --
int | -7, 0, 12_345: no plus sign, no leading zero | a plus sign, leading zeros, -0 (as 0)
dec | digits, a point, then exactly scale digits: 9.99, -0.05, 0.00, 12_345.50; at scale 0 no point: 1500; zero unsigned | a plus sign, leading zeros, .5 (as 0.5), -0.00 (as 0.00); never an exponent
f64 | the shortest digits that read back as the same float; plain when 10^-6 <= abs(x) < 10^21 or x is 0 (9.99, 0.001, 12_345.6, 3, 0), else with an exponent, no plus sign and no leading zero in it (1e21, -1.5e-7) | any decimal float literal: 9.99e0, 1E3, +1.5, .5, 5.
date | 2026-11-21 | --
utc | 2026-11-21T09:30:00.000Z, always three digits of milliseconds | no fraction, or one to three fraction digits, more only when zeros; t and z in lower case; a space for T; an offset +HH:MM or -HH:MM, converted to UTC. A leap second (:60) is an error
time | 09:30:00.000 | 09:30, 09:30:00, one to three fraction digits; 24:00 is an error
dur | an optional minus, then hours h, minutes m and seconds s, each only when not zero, seconds to three decimals without trailing zeros: 1h30m15.25s, -0.5s, 0s | parts beyond their range (90m, 1h90m), a milliseconds part (1500ms), decimals on any part when the total is whole milliseconds (1.5h); parts in that order, each once. No days: a day is not always 24 hours
str | "..." escaping only the quote, the backslash and control characters below U+0020, as \b \f \n \r \t or \u00xx in lower-case hex; every other character as itself | any JSON escape: \/, \uXXXX for any character in either case, surrogate pairs
bin | 0x, then two lower-case hex digits a byte: 0x00ff; empty is 0x | upper-case hex digits, 0X
.end

# 7. Errors

Errors fall in four families: R, the structure of the bytes; T, the structure of
the text; V, a value outside its type, found the same wherever it is met --
decoding bytes, parsing text, or encoding a program's value; and S, a valid row
not of the shape a program reading it expects (section 8). Every error carries
the path of the item at fault when there is one, and the byte offset (bytes) or
the line (text). Go and JavaScript report the same codes.

.fmt 76 6L *L
^code | error
R001 | the bytes end inside a count, a length, a payload or a varint (empty bytes included, which hold no count)
R002 | bytes after the row, where one row was expected
R003 | a varint longer than 8 bytes
R004 | a reserved tag
R005 | rows nested deeper than THREE LEVELS (decoding, encoding and parsing)
T001 | a line that is not one: no colon, a broken label, stray text
T002 | a path that is not one: a leading zero, more than three positions, anything but digits and dots
T003 | an unknown type name
T004 | a value its type cannot read (abc as an int, 2026-02-30, an underscore in a date), a value on a none or row line, or none where one is due
T005 | a row whose positions have a gap or a duplicate
T006 | an item whose row has no line, or whose row's line is of another type
T007 | labelled and unlabelled lines in one text
V001 | an int outside -(2^53-1) to 2^53-1
V002 | a decimal with a scale above 15, or a mantissa outside -(2^53-1) to 2^53-1
V003 | a float that is NaN, infinite or -0
V004 | a date outside 0001-01-01 to 9999-12-31
V005 | a datetime outside its range, or finer than a millisecond
V006 | a time outside one day, or finer than a millisecond
V007 | a duration outside -(2^53-1) to 2^53-1 milliseconds, or finer than a millisecond
V008 | a string that is not valid UTF-8
V009 | a program's value that has no ROW type (encoding only)
S001 | an item of another type than the reader asked for
S002 | the row ends where the reader asked for an item
S003 | items left where the reader expected the row's end
.end

Text that reads is not therefore in range: 2026-02-30 is no date (T004), while
10000-01-01 is a date outside the type (V004), as is a datetime whose offset
carries it past 9999.

# 8. Go and JavaScript

Both implementations offer the same operations under their language's names.

.fmt 76 6L 40L *L
^type | Go | JavaScript
none | nil | null
bool | bool | boolean
int | int64 (any integer type accepted) | number
dec | Decimal{Mantissa, Scale} | Decimal(mantissa, scale)
f64 | float64 (float32 accepted) | Float(value)
date | Day (days since 1970-01-01) | Day(days)
utc | time.Time, UTC when decoded | Date
time | TimeOfDay (milliseconds) | TimeOfDay(ms)
dur | Duration (milliseconds; whole-millisecond time.Duration accepted) | Duration(ms)
str | string | string
bin | []byte | Uint8Array
row | Row, a []any ([]any accepted) | Array
.end

.fmt 76 30L *L
^Go (JavaScript) | does
Encode (encode) | a row to bytes
Decode (decode) | bytes to a row; R002 if bytes follow it
Format (format) | a row to the text of one row, no labels
Parse (parse) | the text of one row to the row
FormatLabeled (formatLabeled) | labelled rows, in order, to one text
ParseLabeled (parseLabeled) | a labelled text to its rows, in order
Read (reader) | a reader of a row's items, in order
Spec, JS | the reference, and row.js's source
.end

Encode and Format check every value (V codes), so both can fail. Errors (section
7) are *Error in Go and RowError in JavaScript.

## 8.1 The reader

A program reads a row the way it was written: position by position. A reader
walks the row's items in order; each typed read takes the next item and fails
(S001) if it is of another type, or (S002) if the row has ended.

.fmt 76 22L *L
^read | takes the next item
Str Bin Int Dec F64 | as that type
Bool Date UTC Time Dur | as that type
Row | as a row, returning a reader of its items
None | if it is none, reporting so; else leaves it
Peek | nothing: names its type (str, dec, ...)
More | nothing: reports whether an item is left
End | nothing: S003 if an item is left
.end

In Go a reader's first error sticks: later reads return zero values, and End, of
the row or of any row within it, returns that first error. In JavaScript every
read throws.
.width 72
.cols 1
.font sans
