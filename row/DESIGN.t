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

# Text

The text form exists so that exported data is never an opaque blob: a store's
rows, or an app's, are exported, inspected, edited by hand where needed, and
imported again. Writing rows from nothing is not its job, so it is shaped for
reading, diff and the one-line edit, not for typing.

One item per line, path: type = value, the shape of a FACT line (FACT supplied
the idea; ROW's text is its own notation, not a FACT file). A line is complete
in itself: a changed amount is one changed line in a diff. The cost is length, a
dozen or more lines for an iou record. Grep is a convenience here, not the aim
it is for FACT; the one that matters is a key's grep returning every line of its
row (see Labels).

Nesting is spelt by the path, not by brackets: a nested item's path is its row's
path, a dot and its position. A nested row has a line of its own with no value,
so an empty row is visible and no count can disagree with the lines under it.

Each line states its type, because the type is what makes lenient input safe.
Where the spelling alone gave the type, an editor typing 9.99 into a float would
make it a decimal without a word; with the type on the line, a float line reads
9.99 as the float, a bytes line reads upper-case hex, and both print back
canonically. Edits need leniency, and leniency stops at the value: input never
adjusts what a value is, and a decimal's digits are its scale.

Strings stay on one line, line breaks escaped. A string syntax spanning lines
(heredoc, triple quotes, continuation lines) would end the property every line
has, being complete in itself: lines could no longer be parsed alone, grep would
return fragments, and canonical form would need rules for indentation and
terminators. With one item per line a long string costs its own line, not the
row's; a reading view that unescapes is a tool's concern.

Rejected: FACT itself as the text (positions are not names, and FACT has no
decimal, date, time, duration, bytes, none as a value or nesting, its first
principle forbidding the last); v1's one row per line with bracketed lists (an
edit means finding a value inside a long line, and the spelling had to carry the
type); strict input, one spelling accepted (right for a format written by
programs, wrong for one edited by hand).

## Paths

Paths count from 0, as Go and JavaScript index the decoded row. Gaps, duplicates
and an item whose row has no line are errors, not repaired: input is lenient
about spelling, never about structure, since a guessed structure is an altered
value. Path order compares positions as numbers.

## Labels

Several rows in one text carry a label on every line. Three layouts were
weighed:

.term two kinds of label
A number for a sequence (its position, sorted as a number) and a string for
named rows (a key, sorted byte by byte), one kind to a file. Canonical order is
then intrinsic, but there are two kinds, and both exist only to give an order.
.term blocks
A label line opens a row and its item lines follow unprefixed: the shortest, but
a line no longer says its row, and a line moved between blocks silently changes
rows.
.term strings in file order
One kind of label, a string; rows keep the order of their labels' first
appearance, and only the lines within a row are sorted. A store exports in key
order and a sequence in its own, so order comes from the file and nothing is
padded ("10" after "9").

The third was taken: one kind of label, every line complete, and a key's grep
returning every line of its row. Its costs: canonical form is not wholly
intrinsic (the same rows in another order are another text; fmt sorts lines
within a row and never moves rows), and labels are unique, a repeated label
being the same row.

## Type names

Type names are two to four characters and sit in the tag registry beside their
tags, so a new type registers both at once and neither is reused. They are on
every line of every export, so short; readable at a glance, so not codes. The
name is the type's, not the tag's: short and long strings are both str, the
sixteen decimals dec, short and long rows row, since which form is used is the
encoding's. Most are the type's own word (none, bool, int, date, time, row) or
its common short form (str, dec, dur, bin). time is a time of day, as SQL's TIME
is; tod was weighed, against Go's time.Time and JavaScript's Date being
instants, but in ROW the instant has its own name and a time line's value
(09:30:00) cannot be read as one. float is f64, naming its width, so a packed
f32 could join it. datetime is utc: the type is a UTC instant, and the zone is
what datetimes are most often got wrong over, so the name says it; a local or
zoned datetime, if one is ever added, would sit beside it plainly. dt, the usual
short form, suggests a date and time that may be local (Python's naive
datetime), and ts (timestamp) leaves the zone to the value's final Z. SQLite was
a reference: it has no datetime type, and utc is a modifier of its date
functions. Input takes no aliases: leniency is about how a value is spelt, not
the vocabulary, so grep and the registry stay exact.

## Spellings

The canonical spellings take v1's where its reasons hold and change where the
type now on the line makes a mark redundant: a scale-0 decimal is 1500, not v1's
1500. with a point that only told it from an int. v1 also gave every float an
exponent (9.99e0), for the same reason; with the type on the line a float is
plain where plain is readable, 10^-6 <= abs(x) < 10^21, and has an exponent only
beyond. The range is where JavaScript's Number toString switches, so row.js gets
the digits and the notation natively, only adding the underscores and dropping
the plus sign from an exponent (1e+21); Go writes strconv's 'f' and 'e' forms
with the shortest digits, the same digits (checked), dropping the exponent's
plus sign and leading zero (1e-07 is 1e-7). A float and a decimal can now look
alike (f64 = 9.99, dec = 9.99): the type name tells them apart, which is what it
is on the line for. Input reads any float literal. A datetime always has three
digits of milliseconds: one shape, readable down a column; input takes it
without them.

Large numbers are hard to read without separators, so canonical output groups
the digits of a number in threes with an underscore once it has five or more
(12_345; 1234 stays whole, as four-digit numbers usually are written): the whole
part of an int, a decimal and a plain float. Output groups only that: a
fraction's digits are positions, not a magnitude, and an exponent never reaches
five digits. Grouping is a rule of canonical output, so input is generous: an
underscore between two digits anywhere in a number or a duration, and none.
Dates, times and datetimes take none: their digits are fixed fields, which an
underscore only blurs. The cost is outside ROW: tools that read numbers from
text (sort -n, awk, JavaScript's Number) stop at the first underscore, so an
export is read through ROW, not picked apart by them.

Input accepts what an editor plausibly types for the same value and nothing that
could be another value. A datetime with an offset is read as the instant it
names, converted to UTC, since an offset spells the same instant; a leap second
is refused, the type having none. A duration may be written in whatever parts
are handy (90m, 1500ms, 1.5h), each part's decimals accepted only when the total
is whole milliseconds, so leniency never rounds. Durations have no days: a
calendar day is not always 24 hours, and an exact duration that said d would
invite the mistake. -0 is 0 for an int, but an error for a float, whose -0 is a
value of its own that ROW refuses; the two follow their types. Floats are the
one input that rounds, as every float reader does: decimal digits generally have
no exact binary64 value, and taking the nearest is reading, not altering.

No comments, as in FACT: what an export needs to say about itself goes beside
it.

# Errors

Four families: R for bytes, T for text, V for values, and S for a row's shape
against a program's expectation (see The reader). A value outside its type is
one error wherever it is met -- in bytes, in text, in a program's value being
encoded -- which is REFUSE, NEVER TRUNCATE made concrete: one list of value
errors, not one per reader and writer. A text that reads but lies outside its
type is a V error, not a T one, so 2026-02-30 (no date) and 10000-01-01 (a date
ROW does not hold) are told apart. Precision is a range: a nanosecond time.Time
fails V005 rather than rounding. Ranges and precision share a code per type, the
type being what the reader needs to know.

There is no error for a non-canonical spelling: input is lenient, and a tool may
warn that a text is valid but not canonical (as FACT's W002), without a code
here. The design removed whole classes v1 needed: a varint longer than its
shortest form (bijective varints have one form), a string or row in the wrong
form (long lengths and counts are offset), a scale byte out of range (the scale
is in the tag), an empty row or a break out of place, and a compressed stream
not canonical. Every error carries the item's path, which a positional format
otherwise leaves the reader to count.

# Go and JavaScript

The value types keep v1's where its reasons still hold, from scratch and not for
v1's apps. Day and TimeOfDay, not the registry's date and time: JavaScript's
Date class would be shadowed by a Date of ROW's whenever it is destructured, and
Go's row.Time beside time.Time is a trap in reading. Names that do not collide
are worth more than names that match the text; the registry names the text. utc
is the language's own instant (time.Time, Date), an exact fit in JavaScript,
whose Date is milliseconds since the epoch over the whole of 0001 to 9999, and
finer than ROW in Go, which is why the encoder refuses below a millisecond.
Duration is ROW's own in Go of necessity: time.Duration counts nanoseconds in an
int64, about 292 years, where ROW's durations reach 285,000. A row is []any, as
encoding/json does it; a typed value struct would lengthen every app. Labelled
text has its own pair of functions, because "" is a label and must not mean "no
labels". The JavaScript source is JS() string, as lz4s has it: one convention. A
binary sequence reader (rows laid end to end) waits for a consumer.

## The reader

Every app that reads rows checks positions by hand: iou's record reader is about
eighty lines of type checks, optional fields, exact field counts and a sharers
group whose share is a decimal or an int; raid's event reader checks field
counts. Porting iou is the next consumer, so the reader is designed now, from
those call sites.

A reader, not indexed getters: a record is read in the order it was written, so
walking its items removes the index bookkeeping, and End checks the exact count
that both apps check by hand (S003, a field over; S002, a field short). Typed
reads are named by the text's type names (Str, Dec, UTC ...), one vocabulary
across text and code. Three reads serve the shapes the apps have: None an
optional position, Peek a position of several types (iou's share), More a
repeating group. Go's first error sticks and End returns it, as bufio.Scanner
does, so a record reads as straight-line code checked once; JavaScript throws,
its idiom. Shape errors have codes of their own (S) because the row is valid:
the mismatch is between the row and what a program expects of it, and the same
code in both languages lets one test hold them to the same answers.

Writing needs no counterpart: a row is a literal ([]any{...}, an array).

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
2026-11-21 | Text: for export, inspection and edits; one item per line, path: type = value, nesting by path, a line for each nested row; canonical output, lenient input; strings on one line.
2026-11-21 | Text type names, two to four characters, in the tag registry: none bool int f64 date utc time dur str bin dec row.
2026-11-21 | Text paths from 0, dotted, three deep, contiguous, a line for each nested row, sorted as numbers; several rows by a string label on every line, rows in the order their labels first appear.
2026-11-21 | Text spellings and lines (ROW.t 6.4, 6.5): datetimes always with .000, floats plain between 10^-6 and 10^21, exponent beyond; whole parts of five or more digits grouped in threes with an underscore (ints, decimals, plain floats), underscores accepted on input in numbers and durations, never in dates or times; lenient input as tabled.
2026-11-21 | Errors: R001-R005 bytes, T001-T007 text, V001-V009 values wherever met; each with the item's path; no canonical-form error.
2026-11-21 | Go and JavaScript API (ROW.t 8): v1's value types where their reasons hold, JS() string, labelled text in its own pair; a sequential reader with sticky errors in Go, S001-S003 for shape.
.end
.width 72
.cols 1
.font sans
