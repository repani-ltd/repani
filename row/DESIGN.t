ROW DESIGN
.date 2026-11-21
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem Why ROW is the way it is: the reasons behind ROW.t, the alternatives
.rem weighed, and the decision ledger. Not normative: where this and ROW.t
.rem differ, ROW.t holds.

# Afresh

ROW v1 (frozen 2026-10-12) is retired whole, with its spec, decisions and
history, to ~/repos/_attic/row. It had no outside users, and every store and app
that speaks it (ASCON-KV's client, parea, webapp, iou, raid) is ours to move.
This ROW is written from a blank page under the same name; nothing of v1 binds
it, and where it agrees with v1 it is because the reason still holds.

# No compression

v1 compressed strings of 242 bytes and more with lz4s when that made them
shorter, and required the stream to be lz4s.Compress's own, so that a string
kept one encoding: every decoder compressed each such string again to check it,
and JavaScript carried a compressor held byte for byte to Go's.

Measured on 2026-11-21 (synthetic iou expense and event rows, lz4s against LZ4,
LZ4 HC, LZW, deflate, Smaz and byte-pair encoding): a single row compresses by 0
to 13 percent, and rows with random identifiers grow; ten rows together compress
to about 52 percent and a hundred to 32. The gain is in sequences of rows, which
belong to whatever holds them, and there no canonical stream is needed. So no
item has a compressed form, and nothing in ROW recompresses.

# Rows within rows

A value is one row. Some values are one record with a repeating part -- an
invoice and its lines, an expense and its sharers, a game and its moves -- that
merges whole by design. v1's one-level lists held those only as strides (iou's
sharers as id, share, id, share; raid's moves as from, to, from, to), the row
boundaries living in each app's code.

Three structures were weighed:

.term rows and lists
A value is several rows separated by a break item, and a list holds single
values of one type. Appending a row needs no rewrite, and breaks are cheap (one
byte a row). But a record with two repeating groups, or an empty one, needs a
convention in every app (an order, a kind marker, a count), since a group lives
in rows rather than at a position; one record is no longer one row; and it needs
a break item, a rule for where breaks stand, and an answer to zero rows against
an empty row.
.term one level
v1's lists: the strides above.
.term two levels
A list may hold lists, which hold none. Every shape above is a position: a fixed
tuple is a list, a repeating group a list of lists, empty or not, as many as a
record has. One record is one row, one line of text, one structural error.

Two levels won: it says strictly more than rows and lists, with fewer concepts.
Its costs: a repeating group spends a tag and a count per line (eight bytes for
three sharers, against three breaks), and the depth is a judgement -- one level
forces strides, and no use for a third is known. Unbounded nesting (CBOR, JSON)
was never a candidate.

Under two levels a list is what a value is, an ordered sequence of typed items,
positional or not, so it is named row: one concept, not two, and ROW's only
structural rule is how deep rows go (THREE LEVELS, counting the value). A set of
tags is then "a row of strings", the one place the name reads oddly.

Rejected, and recorded because they will be asked again:

.item Special rows -- a dictionary row that later items refer to, or a
compression row. Either gives one value many encodings (inline or by reference),
so ONE ENCODING would need a rule a decoder can only check by rebuilding the
dictionary; rows would depend on each other; and the text notation could not
both show the data and round-trip. Domain lists, headers and kinds are data:
ordinary items an app writes. SELF-CONTAINED is the line: a row means only what
its own bytes say.
.item A tag that starts each row, against a break between rows (under rows and
lists): a byte on every value, one-row values included, and an error for items
before the first row.
.item Typed (homogeneous) lists, and packed numeric lists. Under rows and lists,
homogeneity was what kept a list apart from a row; under two levels a row is
heterogeneous by nature. A packed list drops a tag per element: 50 percent on
small ints, 25 on dates, 11 on floats, nothing on short strings or bytes, whose
tag is their length. A packed array of numbers is a type of its own if a
consumer with bulk numeric data appears, taken from the reserved tags.

# A count on every row

Every row, the value included, is a count and then its items; a row inside a row
adds its tag. v1 had no count on the value, since a store already knows a
value's length and a count would say it again, at a byte and an error. That held
for store values alone. A generic format is also kept in files, logs, exports
and bundles, and with a count a row says where it ends (SELF-DELIMITING): rows
laid end to end need no framing, which is the sequence of rows compression
belongs to. The count also makes the value and a nested row one rule, keeps
empty bytes out of ROW (the empty row is 00; an empty value is the store's or
the application's), and lets a decoder see lost trailing items, which a sealed
store value cannot suffer but a file can. The costs: a byte per value, one or
two percent of a typical row, and one error, bytes after the row where one row
was expected. The count is a varint, not a byte, so a repeating group can have
more than 255 lines. The value carries no row tag: it is always a row, so the
tag would say nothing.

# Short rows

A row inside a row of 0 to 15 items carries its count in its tag (0xE0-0xEF), as
a short string carries its length and a decimal its scale; a long row (0xCB)
stores its count less 16, so a short row has no long encoding. Rows within rows
are how ROW says tuples and repeating groups, so every nested row paid a count
byte: measured 2026-11-21 on iou-style expenses (a sharers row of one to three
two-item rows), short rows take 73.9 bytes to 70.9, 4.1 percent; event rows,
with none nested, do not change. 0 to 15 rather than 0 to 7: invoice lines and
the moves of a short game pass seven, for eight tags more. The value's own count
stays a plain varint, one byte to 127 items.

Small ints in the tag, as CBOR has them (0 to 23 in its head byte), were
measured beside it: with them expenses take 68.9 bytes. Refused: a family of int
widths is int8 and uint16 by another name, and keeping one encoding would need a
long int that excludes the small ones, awkward under zigzag, or a rule and an
error. The count is the only number that gets a short form.

# Against other formats

Measured 2026-11-21, 1000 records each, the same logical values (bytes per
record; the strings alone are 34.9 and 25.7):

.fmt 60 22L 10R 10R
^format | expense | event
ROW, short rows | 70.9 | 39.6
CBOR, deterministic | 72.5 | 42.4
MessagePack | 73.3 | 42.3
protobuf, with a schema | 75.9 | 41.6
.end

Strings are half of every record and no format shrinks them, so the formats fall
within a few percent. CBOR packs small ints and counts into its head byte; ROW
packs decimals (3 bytes to CBOR's tag-4 6), dates (4 to tag 100's 5) and
datetimes (7 to tag 1 with a float's 10). Protobuf's schema saves type bytes but
pays a tag byte a field and a length a nested message. MessagePack has no
decimal or date (ext types, meaning by application) and no canonical form.

CBOR is the serious alternative: standard, libraries everywhere, diagnostic
notation and tools, deterministic profiles (RFC 8949 4.2, dCBOR). Against it:
ROW would still be a profile to define (no maps, no indefinite lengths, no half
floats, chosen tags for decimal, date and time) and to police, since common
libraries accept what a deterministic profile forbids, so the validator stays
ours; v1 judged the same. Bencode is simpler and canonical but has no types
beyond int, string, list and dict, and ints in ASCII. BSON (field names, int32
lengths), Avro, Cap'n Proto and FlatBuffers (schema-bound) and Ion (complex) do
not fit. Speed was not measured: all are single-pass, head-byte formats, CBOR's
fixed-width ints load where ROW's varints loop, and strings dominate.

# Tags

Short strings 0-127, short bytes 0-63, sixty-four typed tags. A short form saves
exactly one byte per item over a tag and a length, so each short length is worth
how often items of that length occur. Row strings cluster low (codes, names,
keys, short notes); above 127, one byte saved is under one percent of the
string, and 127 is what a one-byte varint holds. Bytes cluster at 16 and 32
(ids, Ascon tags, hashes, keys); 64-byte signatures go long at one byte more.
The two high bits of a tag give its class, so a decoder tests bits, not ranges.

The long forms store the length less 128 (strings) or 64 (bytes): a short value
then has no long encoding, and v1's form rule and its error disappear. Against
v1 only strings of 128 to 241 bytes grow, by one byte; bytes of 0 to 63 shrink
by one.

The types are v1's thirteen, the list renamed row; v1's compressed form of a
string is gone. More are added only by appending. The typed tags run
payload-free first (none, false, true), then numbers and times, then the
length-carrying forms (long string, long bytes, long row). Decimal carries its
scale in its tag (0xD0-0xDF), as short rows carry their count (0xE0-0xEF), so a
decoder tests tag & 0xF0 for both: money is why the format has a decimal, and
every amount is a byte shorter than with a scale byte, for fifteen tags of
growth room. Twenty reserved tags (0xCC-0xCF, 0xF0-0xFF) are more growth than a
generic encoder will plausibly use. The payloads keep v1's where its reasons
hold: milliseconds (see Decimals and times), years 0001 to 9999, floats finite
and never -0.

# Varints

Bijective base-128, most significant group first (git's pack offsets), not
LEB128. LEB128 gives a value many encodings (00 and 80 00 are both zero), so it
needs a shortest-form rule, a check in every decoder and an error; bijective has
one encoding by construction, at the cost of a +1 in the loop, and is slightly
denser at each length boundary (two bytes reach 16511, not 16383). SQLite's
varint is LEB128's big-endian cousin, many-to-one too, with a 9-byte form for
the full 64 bits; SQLite4's gives the length in its first byte and sorts under
memcmp, which matters for keys, not values.

Signed integers are zigzag, within ±(2^53-1). Zigzag is already one-to-one, so
with bijective varints an integer has one encoding. Sign tags would hold 0 to
127 in a byte rather than -64 to 63, but cost a tag for each signed type or a
second mechanism beside zigzag. The range is what Go and JavaScript both hold
exactly: a full int64 would make JavaScript decode some ints as BigInt and every
caller handle both.

# Decimals and times in the format

ROW makes decimal, date, datetime, time and duration types of the format, not
conventions over ints, strings or bytes. Reviewed adversarially on 2026-11-21.

The premise that formats lack them holds for JSON, MessagePack and language
cores, not for formats built for records: SQL has DECIMAL, DATE, TIMESTAMP, TIME
and INTERVAL; Amazon Ion has arbitrary-precision decimals and timestamps, made
because JSON could not carry them faithfully; Avro and Parquet have them as
logical types; BSON has Decimal128 and Date; protobuf has well-known Timestamp,
Duration and Date; CBOR has tags 4, 1, 100 and 1004. Languages are moving the
same way (JavaScript's Temporal; its Decimal proposal).

Against: the semantics are contested (precision; whether 90.00 is 90.0;
calendars and ranges; instants, local and zoned times; calendar durations
against exact ones), so a format that chooses excludes someone, and JSON avoided
that by staying out. The conversion moves, it does not vanish: Go has no
standard decimal and JavaScript none at all, so an app still converts ROW's
decimal to its own library. Each type is range checks, round trips and vectors
in every implementation.

For: without them every app invents an encoding (days or an ISO string, ms or
µs, an amount as a float), and the apps disagree; defined once, with exact
ranges and one encoding, a class of fintech bug goes. A date stays a date when a
row is inspected or exported with no schema, which is SELF-CONTAINED. And ROW's
choices are the defensible ones: a decimal keeps its scale, as SQL and Ion do,
which is what money needs (minor units kept, yen at scale 0); a datetime is a
UTC instant, right for events, the zone being presentation; a duration is exact,
calendar periods needing a calendar; zoned times and money are left out, since a
zone needs the tz database and a currency the ISO list, both outside the bytes
-- an app puts date, time and zone, or currency and amount, in adjacent
positions.

The verdict is for, on two conditions. First, limits are refused, never applied
quietly (REFUSE, NEVER TRUNCATE): an encoder given Go's nanosecond time.Time
with sub-millisecond digits, or a decimal past its range, must fail, or the
format corrupts data silently in the very domain it exists for. Second, the gaps
are stated here:

.item Timestamp precision. Datetime is milliseconds; Postgres stores
microseconds and Go's time.Time nanoseconds. Microseconds cannot span 0001 to
9999 within 2^53 (9999 is about 2.5 x 10^17 µs; 2^53 is about 9.0 x 10^15): a µs
datetime would reach about 285 years either side of 1970, 1685 to 2255.
Milliseconds keep the full range, and are what ASCON-KV's own timestamps are.
.item Decimal reach. A mantissa within ±(2^53-1) covers every currency: about 90
trillion units at scale 2, 900 billion at scale 4, and 90 million bitcoin in
satoshis (scale 8) against a supply of 21 million. Tokens of 18 decimals (ether
in wei) do not fit: the scale stops at 15, where the mantissa reaches about 9
units.

Either gap, when a consumer meets it, is a new type from the reserved tags (a
microsecond datetime, a wide decimal), APPEND-ONLY; no existing type changes
meaning.

# Ledger

.fmt 76 11L *L
^date | decision
2026-11-21 | Written afresh; ROW v1 to the attic.
2026-11-21 | No compressed form of anything.
2026-11-21 | Short strings 0-127, short bytes 0-63, 64 typed tags; long lengths counted from the short limit.
2026-11-21 | v1's types stay; new ones by appending.
2026-11-21 | Varints bijective base-128; signed integers zigzag within ±(2^53-1).
2026-11-21 | Decimal scale in the tag.
2026-11-21 | One row per value, lists two deep. Superseded the same day: several rows per value with a break item, zero rows allowed and empty rows refused, typed lists.
2026-11-21 | The list is named row; every row, the value included, is a varint count then its items; the value has no tag.
2026-11-21 | Tag assignment final: 0xC0-0xCB the twelve single-tag types, 0xD0-0xDF decimal, 0xCC-0xCF and 0xE0-0xFF reserved.
2026-11-21 | Short rows: 0xE0-0xEF, 0 to 15 items, the long row's count stored less 16; twenty tags reserved (0xCC-0xCF, 0xF0-0xFF). Small ints in the tag refused.
2026-11-21 | Decimal and time types stay in the format, reviewed; REFUSE, NEVER TRUNCATE; the ms and 2^53 gaps stated, filled by new types if needed.
.end
.width 72
.cols 1
.font sans
