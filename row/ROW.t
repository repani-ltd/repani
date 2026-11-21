ROW — Typed Rows
.by Pavlos Christoforou
.date 2026-11-21
.rights All rights reserved © repani.com
.rem DRAFT, written from a blank page on 2026-11-21. ROW v1 (frozen 2026-10-12)
.rem is retired whole to ~/repos/_attic/row and binds nothing here. The code in
.rem this directory is still v1 until this draft replaces it. The reasons and
.rem the decision ledger are DESIGN.t; section 6 lists what is open.

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
rows, and may be empty:

.pre
"invoice" 2026-11-21 "EUR" [["widget", 2, 9.99], ["gadget", 1, 5.00]]
.end

.rem The example's spelling is illustrative: the text notation is open.

# 3. Tags

The two high bits of a tag give its class:

.fmt 76 11L 22L *L
^tags | class | payload
0x00-0x7F | short string | tag bytes of UTF-8 (0 to 127)
0x80-0xBF | short bytes | tag - 0x80 bytes (0 to 63)
0xC0-0xFF | typed | the types of section 5, and reserved tags
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

.fmt 76 10L 12L *L
^tag | type | payload
0xC0 | none | nothing
0xC1 | false | nothing
0xC2 | true | nothing
0xC3 | int | zigzag varint
0xC4 | float | 8 bytes, IEEE 754 binary64, little-endian
0xC5 | date | zigzag varint: days since 1970-01-01
0xC6 | datetime | zigzag varint: milliseconds since 1970-01-01T00:00:00Z
0xC7 | time | varint: milliseconds since midnight
0xC8 | duration | zigzag varint: milliseconds
0xC9 | long string | varint length less 128, then that many bytes of UTF-8
0xCA | long bytes | varint length less 64, then that many bytes
0xCB | long row | varint count less 16, then that many items
0xCC-0xCF | reserved |
0xD0-0xDF | decimal | zigzag varint mantissa; the scale is tag & 0x0F
0xE0-0xEF | short row | tag & 0x0F items (0 to 15)
0xF0-0xFF | reserved |
.end

Forty-four tags are assigned and twenty reserved.

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

# 6. Open

.item Text notation.
.item Errors and their codes.
.item The Go and JavaScript API.
.width 72
.cols 1
.font sans
