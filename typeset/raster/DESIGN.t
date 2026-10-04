RASTER -- WHAT IS DECIDED AND WHAT IS NOT
.date 2026-10-04
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem The register of why RASTER.t is what it is, and what is open.
.rem RASTER.t is the format; nothing here is normative.

Raster is rows of forty columns: volatile, glanceable content
painted on any screen and tapped on a phone. This is its second
design. The first -- a glyph and an ink per cell, 1024 rows, its
own authoring language -- was never frozen and never aired, its
TASKS entry asked for a second design rather than fixes, and it is
in `~/repos/_attic/raster` with its history. The second was worked
out in repani-lab/board from 2026-10-02 and replaced it in place
on 2026-10-04: "successors are new protocols" guards frozen specs,
and the first was not one. Its first producer is board's table
language (repani-lab/board), compiled to rows; any producer may
set any content on rows.

# Foundations

.item FORTY COLUMNS, measured on 2026-09-17 over the limasoul
corpus: of 988 rendered content lines in eight production pages,
none exceeded 40. The measurements are in the first board's
DESIGN.t, `~/repos/_attic/board`. The width may later become a
field (What is open).
.item THE ROW IS THE UNIT of storage, update and transmission, as
in the first raster: a file is rows, an update is rows.
.item NO CONTENT TYPE. Tables, the class the samples are, and
paragraphs set by a producer are both rows. What a raster holds is
its producer's choice; what it means to read is its renderer's.

# The record

Settled 2026-10-02 from the nine mock boards of `bill-mock.html`
(Provenance); segments and roles 2026-10-04. What the samples use,
and which part of the record holds it:

.table 72 30L 28L 12L
^pattern | where | holds it
title bar, white on blue | every mock board, row 0 | segment
heading in cyan | TOP STORIES, PROVENANCE | segment
alert band, yellow on red | the weather advisory | segment
status mark and headline | News, Live, Local | segments
colour chosen by value | Departures STATUS, Economy CHG | segments
label and value in two colours | SEA and SUN on Weather | segments
several colours in a row | the key row, Help's swatches | segments
one row rewritten | Departures | row
labels over the rows | Departures, Results | role
a tap that goes somewhere | every bracketed link | link
.end

No sample sets a background on part of a row, and none uses a
style other than colour: no bold, blink, double height or
underline. The marks ● and ○ are glyphs, not styles.

.item THE TEXT is forty code points of single width, so counting
code points counts columns. Emoji and CJK are out; Greek, accented
Latin, box drawing, blocks, arrows and symbols such as ° µ € ● are
in. The line and paragraph separators are out though not controls,
since they break a line. Text with no length field: forty code
points end the record.
.item COLOUR is a palette of eight, 0 the theme's default, so a
theme decides every colour and a dark or light screen needs no
second page. The foreground changes within a row on six of the
nine mock boards.
.item A SEGMENT replaced, on 2026-10-04, three separate things --
foreground runs, link spans, and one background per row -- each a
list or a field with its own rule, whose product every renderer
computed: a renderer draws stretches of one style, and a segment is
one. The background came into the segment with them. No sample
sets a background on part of a row, but in a segment it costs one
byte and no rule of its own, it takes the row's own field away,
and a status chip, an inverse label or a highlighted match needs
nothing new. No style sits at row level: a row in one style is one
segment, and a row foreground or background beside the segments
would be a second place for one property. A style added later --
bold, say -- joins the segment.
.item ONE CANONICAL FORM, so one screen has one encoding. A space
shows its background but no foreground, so a change of background
or of target shows exactly where it is, and a change of foreground
alone does not: `abc   def`, red then green on one background,
looks the same with the change at any of columns 3 to 6. The rule
puts the spaces with the glyphs before them and the change on the
glyph that shows it. Segments that must start and end on glyphs,
spaces between being segments of their own, were weighed and
rejected: the shape replaces one clause, but spaces between two
glyphs of one style then need a clause to keep them inside, a link
padded with spaces needs an exemption, and nearly every row gains a
trailing segment. A space is U+0020 alone, since a rule over one
code point needs no table, though a no-break space's foreground is
as invisible. A decoder rejects rather than normalises, so a
producer that encodes wrongly fails loudly.
.item LINKS ARE IN THE ROW, because a link is content -- these
columns of this row lead somewhere -- and the main consumer is a
phone, where a tap is the interaction; a format without them would
have every producer invent a container to carry them. A LINK IS
ONE SEGMENT, so one style: all 85 links on the nine mock boards are
in one colour. A link over several colours would be several
segments with one target, to be merged again by every renderer
that reports a tap. Reading adjacent segments with one target as
one link would later admit only what is refused now, so the door
stays open at no cost. A link covers spaces as it covers glyphs,
so a padded value is tappable across its padding. A target is a
URI reference, so a bare page number such as 401 is one, and so
are news/1, https:, tel: and geo:.
.item A ROLE says what a row is, settled 2026-10-04 when a rule row
under a header proved to be an instruction to draw rather than a
statement of what the row is: a PDF sets a header bold over a
hairline and repeats it after a column split, HTML makes it a
thead, a raster renderer can draw a hairline between rows instead
of spending one, keep the header in view while rows scroll, and
let a screen reader announce it -- none of which can be read back
from colours and a row of ─. A role is advisory and implies no
colour; an unknown value reads as none, so the registry grows
without breaking old readers. Roles are closed, not open tags: a
tag no renderer knows is a side channel with no effect, and what
only a host needs belongs to its container. Pica's note row has no
value: forty fixed columns cannot set a note at half size, and a
value joins the registry when a consumer needs it.
.item THE PAGE IS ROWS AND A ROW COUNT. An update is the rows that
changed -- Departures rewrites one row at a time -- blanking the
bottom rows keeps the height, shrinking says so, and a whole page
is correct against whatever the renderer held. A height derived
from the content was weighed and rejected: it cannot tell "these
rows are blank" from "the page ends here". The row count sets
rather than shrinks or clears: shrink-only would do nothing, or
fail, when N is above H, and a blank tail would still need rows
sent; clearing from N would leave the height where it was, which is
the problem the record exists for. A whole page closes with its
row count, so a blank tail is stated rather than inferred and one
page has one encoding -- and the first raster's stale row, its
canonical bytes correct only against an empty raster, cannot
happen.
.item EVERYTHING ELSE ABOVE THE ROW IS OUTSIDE: which page this
is, freshness and polling, replacing a page atomically. Every
transport already has a container that says these things -- a
publication's manifest and content-named files, a carousel's
slots -- and a consumer in the same process needs none. A page
header in the format would state the same facts a second time,
with a second chance to disagree. Links and the row count tested
this cut: links refer outward from the content, and the row count
is a statement about the rows, as a row's position is.

# The encoding

Byte-aligned, every field a byte, settled 2026-10-02. The counts
let a reader find where a list ends; a terminator byte would cost
the same and reserve a value. 255 rows to a page: a raster is a
glance of some twenty rows, and a listing longer is several pages.

Segments, not a colour per column. The two say the same thing --
segments are the forty columns' styles written as the columns
where they change -- so the choice is encoding alone. A colour per
column is the simpler decoder, but two colours a column is 80
bytes on every row before its text, the first raster's "cells cost
twice text" again, and a column's colour says nothing a producer
meant: a segment is the stretch it wrote in one style.

EVERY INDEX COUNTS FROM ZERO and each position from one origin,
settled 2026-10-04 so that no error mixes two counts. The one
exception is outside the format: a compiler's error in authoring
source counts lines and columns from one, as editors do.

# Rendering

Drawing characters seam and drift in arbitrary fonts: glyphs
shorter than the line height leave gaps between rows, side
bearings leave gaps between columns, and a glyph from a fallback
font with another advance shifts every column after it. The first
raster answered with a fixed font and pre-drawn glyphs. This one
answers with two rules, the column grid and the drawn set, so the
text stays open and the geometry exact. The drawn set is the first
raster's seventeen; half blocks, quadrants or sextant mosaics join
when a page needs them.

Tested 2026-10-02 across nine fonts, one proportional, in the
first version of `board-align.html`. The plain <pre> seamed,
drifted and, with the proportional font, broke; the two rules held
in all nine. A proportional font stays aligned but clips its wide
letters, so a renderer still wants a monospace font: the rules
guarantee alignment, not looks.

The browser renderer that came out of the test is a canvas under
one transparent text layer. Each segment of a row is placed at its
first column and stretched to exactly its columns, and links are
<a> elements around their own characters. That gives tapping,
keyboard focus, screen readers, selection, copy and find-in-page
over an exact canvas, at one element per segment; a <br> per row
keeps line breaks in a copy. Columns drawn as DOM boxes work as
well, but cost an element per column and three pixel traps: boxes
in whole CSS pixels, shapes as plain boxes rather than masks or
SVG, and no sub-pixel transform.

Colour is optional because an e-ink panel will paint every row in
its defaults and still be a raster renderer. Roles are optional for
the same reason, and a plain text dump does nothing with them, as
it cannot add rows.

# The Go package

A row in Go is its segments, each with its columns, text, colours
and target, so a renderer reads a row's Segments and has everything
it draws and reports.

A producer writes a row as a DRAFT: it puts text at a column, in a
foreground and a background, linked or not, in any order and
overwriting as it likes, and Row derives the canonical segments
and refuses a link in two colours. Put returns the column after
the text, so a producer counts only where text starts. No producer
computes segments or counts link columns: the renderer test's own
hand-counted link clipped https://repani.com to https://repani.c,
and no check can catch that, since a shorter link is valid.

THE PACKAGE STAYS LIGHT. It is all a renderer or a program
producer imports, so it imports nothing that typesets: a test fails
if its dependencies reach typeset/tbl, typeset/wrap, typeset/tab,
pica or board. Compilers that set text on rows -- board's language
-- import those and raster, never the reverse. Its link-target
check is its own for the same reason, duplicated on purpose from
tbl's. `golang.org/x/text` is its one dependency outside the
standard library, for NFC and width.

# What is open

.term reference renderers
A JavaScript renderer (canvas and layer) and a Go one (PDF, PNG),
held together by a fixture as lz4s's two are. Both draw a row's
segments.

.term PUBLISH for raster pages
PUBLISH (repani-lab/publish) names raster pages, carries a
geometry line and name lines that resolve links, and takes its
packet form from qam, which is in the attic. For this raster its
packet form must be stated in its own right, geometry goes or
keeps only an optional count of screen rows, and name lines lose
their link role, which the row now carries; search and
autocomplete may keep them. A page file is a whole page, correct
against any page a client holds.

.term bold
Not now: no sample uses it. Its case is emphasis that survives a
renderer without colour -- e-ink, a monochrome panel, a reader
blind to colour. Worked out on 2026-10-04 for when one asks: bit 3
of fg, so 8 to 15 are the eight colours in bold; bold on a space is
as invisible as colour, so segments and the canonical form hold
unchanged. Its cost is a bold face in every font a renderer ships.
Open with it: whether a renderer meeting a colour it does not know
draws the default -- which lets a registry append harmlessly -- or
rejects the row, as "invalid" now says.

.term variable width
Fixed at forty until real use asks for more; a general row format
may want any width. Worked out on 2026-10-02 for when it does: a
page record `FE W`, W from 1 to 255, that also empties the page;
rows then 0 to 253; every forty becomes W; a whole page starting
`FE W`. Fitting W columns to a screen stays the renderer's.

# Provenance

.item The first raster, retired 2026-10-04: `~/repos/_attic/raster`;
its only consumer, pica's cell writer: `~/repos/_attic/pica-cell`.
.item The design's working history: repani-lab/board, its DESIGN.t
and commits from 2026-10-02.
.item The first board, with the corpus measurements behind forty
columns: `~/repos/_attic/board`.
.item The nine mock boards the record was settled from:
`~/repos/tmp/geometry/out/bill-mock.html`.
.item The renderer test: `~/repos/tmp/geometry/out/board-align.html`
and `board-align-data.js`. Serve the directory to view it; the
webfonts do not load from file://.

.width 72
.cols 1
.font sans
