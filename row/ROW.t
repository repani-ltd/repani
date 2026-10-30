ROW — Typed Rows v1
.by Pavlos Christoforou
.date 2026-10-12
.rights All rights reserved © repani.com
.rem Normative. FROZEN 2026-10-12, ratified by Pavlos Christoforou: v1 is
.rem permanent. The type set is closed and no tag is spare; the lz4s compressor
.rem it names is fixed with it. A successor is a new format, never a revision of
.rem this one.

A row is a short sequence of typed values, encoded as bytes. Each value, an
item, starts with one byte that says what it is. There are no field names: what
each position means is the reading program's, as the words of a sentence mean
what the grammar says. What the format adds is that every item carries its type,
so a date is a date and an amount is a decimal on every platform, and no program
parses them by hand.

ROW exists for the values of ASCON-KV entries (format byte 0x01 in the client
conventions), and nothing in it depends on ASCON-KV.

# 1. Principles

.item CORRECTNESS, NOT COMPACTNESS. Each item is typed, every value has exactly
one encoding, and a decoder rejects anything else. Rows come out small, but no
rule here trades correctness for size.
.item POSITIONAL. A row has no field names and no schema inside it. The program
that writes a kind of row says what each position holds, as iou's record grammar
does for its text lines.
.item FLAT. A list holds items, never lists. Records that change independently
belong in separate entries: an ASCON-KV value merges as a whole (last writer
wins), so two records inside one value lose one device's edit, where two entries
keep both.
.item CLOSED. Thirteen types, short strings and compressed strings fill the 256
tags exactly. No type will be added; a need the set does not meet is a new
format.
.item SAME IN EVERY LANGUAGE. Every number decodes exactly to a native number in
Go and in JavaScript.

# 2. Encoding

A row is a sequence of zero or more items, ending where its bytes end: there is
no count and no terminator. An item is one tag byte, then a payload that the tag
determines.

## 2.1 Varints

A varint is an unsigned integer in LEB128: seven bits per byte, least
significant group first, the high bit set on every byte but the last. It must be
the shortest encoding of its value, and no varint in a row exceeds 8 bytes.

A signed integer n is stored as the varint of its zigzag form: 2n for n >= 0,
-2n-1 for n < 0, so 0, -1, 1, -2 become 0, 1, 2, 3.

## 2.2 Integer range

Every integer in a row -- an int, a decimal's mantissa, a date, a datetime, a
time, a duration, a length or count -- lies within -(2^53-1) .. 2^53-1, the
integers both Go and JavaScript hold exactly. Wider values (hashes, 64-bit
identifiers) are bytes.

## 2.3 Tags

.table 76 10L 14L *L
^tag | type | payload
0-241 | short string | that many bytes of UTF-8 (the tag is the length)
242 | compressed string | varint string length, varint stream length, then the lz4s stream of the UTF-8
243 | none | nothing
244 | false | nothing
245 | true | nothing
246 | int | zigzag varint
247 | decimal | scale byte, then zigzag varint mantissa
248 | float | 8 bytes, IEEE 754 binary64, little-endian
249 | date | zigzag varint: days since 1970-01-01
250 | datetime | zigzag varint: milliseconds since 1970-01-01T00:00:00Z
251 | time | varint: milliseconds since midnight
252 | duration | zigzag varint: milliseconds
253 | bytes | varint length, then that many bytes
254 | long string | varint length, then that many bytes of UTF-8
255 | list | varint count, then that many items
.end

# 3. Types

.term string
UTF-8 text, never normalised: the bytes written are the bytes read. A string has
three forms and exactly one of them is right for it:
.item short (tags 0-241) when it has 0 to 241 bytes, its tag its length;
.item otherwise compressed (242) when that item is the shorter: when c, the
length of lz4s.Compress of its bytes, plus the length of c's varint is less than
n, the string's length in bytes;
.item otherwise long (254).

The stream of a compressed string is exactly lz4s.Compress of the string (the
canonical stream of the repani.com/lz4s primitive), never another stream that
decompresses to it, so a decoder checks it by compressing the string again. A
string in any other form, or holding invalid UTF-8, is an error. The form is the
encoder's choice, not the writer's: the text notation is the same for all three.
.term none
No value: an optional position left empty. A row cannot skip a position, so an
absent value is written as none.
.term false, true
The two booleans.
.term int
An integer within the range of 2.2.
.term decimal
The exact number mantissa x 10^-scale. The scale, 0 to 15, is part of the value:
90.00 (scale 2) and 90.0 (scale 1) are different values that compare equal as
numbers, and a writer keeps the scale its data has -- for money, the currency's
minor digits, so yen are scale 0. The mantissa lies within the range of 2.2.
.term float
A finite IEEE 754 binary64 number. NaN, the infinities and -0 are errors; zero
is +0. For measurements; money is decimal.
.term date
A day of the proleptic Gregorian calendar, 0001-01-01 to 9999-12-31: days
-719162 to 2932896 counted from 1970-01-01.
.term datetime
An instant in UTC, with millisecond precision, from 0001-01-01T00:00:00.000Z to
9999-12-31T23:59:59.999Z: milliseconds -62135596800000 to 253402300799999 from
the epoch. No time zone and no leap seconds; local time is the program's, as a
date, a time and a zone name in three positions.
.term time
A time of day with millisecond precision and no zone: 0 to 86399999 milliseconds
after midnight.
.term duration
A signed length of time in milliseconds, within the range of 2.2.
.term bytes
Opaque bytes: identifiers, hashes, keys.
.term list
A count, then that many items of any type but list. An empty list is a count of
0.

# 4. Decoding

A decoder reads items until the bytes end and rejects the row, with the offset
of the item at fault, when:

.table 76 6L *L
^code | error
R001 | an item's payload, length or count runs past the end
R002 | a varint is longer than its shortest form, or than 8 bytes
R003 | an integer is outside its range (2.2, or its type's)
R004 | a decimal's scale is above 15
R005 | a float is NaN, an infinity or -0
R006 | a string is not valid UTF-8
R007 | a string is not in its one form (section 3)
R008 | a list holds a list
R009 | a compressed string's stream is not lz4s.Compress of its string
.end

A compressed string's stream yields at most 255 bytes for each of its own, so a
decoder refuses (R009) a string length above 255 times the stream length before
it allocates anything.

So one row has one encoding: an encoder that writes these rules produces the
bytes, and two equal rows are equal bytes.

# 5. Text notation

Rows have one text spelling, for people and agents: what row decode prints, row
encode reads and row check validates. A row is its items separated by single
spaces, on one line.

.table 76 12L *L
^type | spelling
string | "..." with JSON escaping: \" \\ and control characters only, as \b \f \n \r \t or \u00xx (lower-case hex); every other character as itself
none | none
false, true | false, true
int | -?digits, no leading zero, no -0: 0, -7, 300
decimal | always a dot, scale digits after it, at least one digit and no leading zero before it, no minus on zero: 90.00, -0.05, 0.00, and 1500. for scale 0
float | always an exponent, shortest round-trip digits: 9.05e1, 1e-3, 0e0
date | YYYY-MM-DD
datetime | YYYY-MM-DDTHH:MM:SS.sssZ, always three digits of milliseconds
time | HH:MM:SS.sss
duration | -?, then h, m and s parts, each only when non-zero, seconds with up to three decimals and no trailing zeros: 1h30m15.25s, -0.5s, 0s
bytes | 0x then lower-case hex, two digits a byte: 0x00ff, 0x
list | [ items separated by ", " ]: ["ben", 1], []
.end

A float's spelling: an optional minus, one digit, then a dot and more digits
only when needed, then e and the exponent with no plus and no leading zero
(9.05e1, -1.5e-7, 1e21). Every value has exactly one spelling; anything else is
an error (T001 for text that is no item, T002 for an item spelt other than
canonically, the message giving the canonical spelling when there is one), and
the R codes of section 4 apply to values out of range.

# 6. Examples

.pre
"EUR"                       03 45 55 52
""                          00
none                        f3
false                       f4
true                        f5
-7                          f6 0d
300                         f6 d8 04
90.00                       f7 02 d0 8c 01
1500.                       f7 00 b8 17
-0.05                       f7 02 09
9.05e1                      f8 00 00 00 00 00 a0 56 40
1e-3                        f8 fc a9 f1 d2 4d 62 50 3f
2026-10-08                  f9 fc c3 02
1969-12-31                  f9 01
2026-10-08T09:30:00.000Z    fa 80 87 c9 ad a3 68
09:30:00.000                fb c0 b3 a7 10
1h30m15.25s                 fc a4 85 95 05
-0.5s                       fc e7 07
0s                          fc 00
0x00ff                      fd 02 00 ff
0x                          fd 00
["ben", 1]                  ff 02 03 62 65 6e f6 02
[]                          ff 00
.end

A string of 300 bytes that does not compress begins fe ac 02; 300 a's are the
compressed string f2 ac 02 05 1f 61 00 ff 1b (one literal a, then a 299-byte
match at distance 1). The iou record "x 2026-10-08 EUR 90.00 at 8FVC9G8F+6X by
pavlos ben > pavlos:1 ben:1 | taxi" as a row, 62 bytes against the 75 of its
text:

.pre
"x" 2026-10-08 "EUR" 90.00 none "8FVC9G8F+6X" "pavlos" none "ben" ["pavlos", 1, "ben", 1] "taxi"
.end

.pre
01 78 f9 fc c3 02 03 45 55 52 f7 02 d0 8c 01 f3 0b 38 46 56 43 39
47 38 46 2b 36 58 06 70 61 76 6c 6f 73 f3 03 62 65 6e ff 04 06 70
61 76 6c 6f 73 f6 02 03 62 65 6e f6 02 04 74 61 78 69
.end

# 7. The row command

.term row spec
Prints the reference: this format, from the package comment embedded in the
binary.
.term row check FILE
Reads rows in text notation, one per line (blank lines skipped), and reports
each error with its line and column; exit 0 means every row is valid.
.term row encode
Reads text rows on stdin and writes each row's bytes as lower-case hex, one row
per line.
.term row decode
Reads rows as hex, one per line, on stdin, and prints each in text notation; a
bad row is reported with its R code and byte offset.

# 8. Implementations

One in Go (repani.com/row) and one in JavaScript (row.js, embedded in the
package for serving), each with encode, decode, the text notation and every
error of sections 4 and 5. Both use lz4s in their own language (repani.com/lz4s,
and its lz4s.js, whose compress is held byte for byte to Go's by the lz4s
fixture). Both are tested against one set of frozen vectors, written by an
independent encoder with its own lz4s compressor: every example of section 6,
each error code, and the edges -- the first and last date and datetime, 0 and
86399999 for time, the integers -(2^53-1) and 2^53-1, scale 15, the longest
short string (241 bytes), the shortest long and compressed strings (242 bytes),
and a pair at the compression boundary: a string that compression would shorten
by nothing (long) and one it shortens by one byte (compressed). A changed vector
is a changed format.

# Appendix A. Decisions

.item 2026-10-12. Positional, without field names. A named field costs its name
in every row: the iou record above grows from 62 to about 110 bytes with names,
larger than its own text, and a name tells the program reading it nothing it
does not already know.
.item 2026-10-12. No item count: a row ends where its bytes end. Its end is
always known -- an ASCON-KV value has its own length and is sealed by the AEAD,
a text row is one line -- so a count would say a second time what the bytes
already settle, cost a byte, and need an error of its own for disagreeing with
them. Lists do carry a count, since a list's end is not the row's. Rows stored
back to back in one stream are framed by that stream (a length, a line), not by
the row.
.item 2026-10-12. Thirteen types, closed. Considered and left out: maps and
named fields (positional by design); nested lists and records (they merge badly
inside one ASCON-KV value, see section 1); UUIDs, hashes, references (bytes);
enumerations (a string or an int at a position); zoned local date-times (date,
time and a zone name, since zones are a changing database, not a type);
geographic points (two decimals, or a Plus Code); 64-bit and big integers
(bytes); characters, percentages and rationals (string, decimal). Float and
duration were added to the first eleven: measurements would otherwise hide in
bytes, and durations in ints whose unit lives in someone's head.
.item 2026-10-12. Integers within -(2^53-1) .. 2^53-1, so Go and JavaScript
decode the same exact number with no BigInt; the cost, fixed forever, is that
wider integers are bytes.
.item 2026-10-12. Millisecond precision for datetime, time and duration.
Microseconds would carry year-9999 datetimes past 2^53, and ASCON-KV's own
timestamps are milliseconds.
.item 2026-10-12. Short strings take every tag below 242. With the type set
closed, a spare range would never be used; given to strings, it saves two bytes
on every string of 128 to 241 bytes (one tag byte instead of a tag and a
two-byte length).
.item 2026-10-12. Strings of 242 bytes and more are compressed with lz4s
whenever that makes them shorter; tag 242 was taken from the short strings for
it. Long text (notes, descriptions, pasted messages) is where a row's bytes go,
and ASCON-KV does not compress values. Compression is a form of string, not a
type: the value and its text spelling are the same either way, so no program
meets two kinds of string. The form is fixed by rule, not chosen by the writer,
and the stream must be lz4s.Compress's own: a writer's choice, or any stream
that decompresses correctly, would give one string many encodings. The cost is
that the lz4s compressor (greedy, one parse rule, documented as canonical) is
part of this format for good, which its primitive package's append-only rule
already promises; that row.js carries a compressor; and that a decoder
compresses every long or compressed string once more to check its form. lz4s
rather than a bulk compressor because it is ours, small, already in Go and
JavaScript, and tuned for texts of a few bytes to a few KiB.
.item 2026-10-12. Not CBOR. CBOR's first byte also says the type, and it has a
deterministic profile, but ROW would have to define and police a subset of it
(no maps, no floats of other widths, no indefinite lengths), its dates and
decimals cost two to three tag bytes more, and nothing outside our own programs
reads these values: in ASCON-KV they are encrypted.
.item 2026-10-12. Frozen. Ratified with the compressed string in place; from
here ROW v1 does not change.

# Appendix B. Naming

A row is what each value is: one row of typed fields, as in a table whose
columns the program knows. Considered: rst (the radio report, compact and
positional, but opaque without the reference), tup, bead, strand.
.width 48
.cols 2
.font sans
