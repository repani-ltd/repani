# Embedded fonts

JuliaMono (Regular, Bold), by cormullion, licensed under the SIL
Open Font License 1.1 (OFL-JuliaMono.txt), subset here to Fira
Mono's coverage plus the raster cell repertoire (1,361 code points;
the full face is 11,934 glyphs). Fira Sans (Regular, Bold, Italic),
by the Mozilla Foundation with Carrois Corporate GbR and
Edenspiekermann AG, SIL OFL 1.1 (OFL.txt). The OFL permits
bundling, embedding, and redistribution; if you redistribute this
repository publicly, include the full licence texts alongside these
files.

Coverage relevant to this package: Latin, Greek, and Cyrillic, and
every glyph of the raster repertoire in the mono faces. JuliaMono
advances are a uniform 0.6 em (true monospace across scripts), the
same face raster's HTML page embeds; Fira Sans is the proportional
face used by ".font sans" documents, its Italic reachable only
through the _..._ emphasis span. JuliaMono replaced Fira Mono on
2026-09-05 (pica/DESIGN.t section 15): Fira Mono lacks eleven
repertoire glyphs and no proportional JuliaMono exists, so the pair
is a mix, tables and code in one face, prose in the other.

The mono files are written with the glyf table last (fontTools
reorderFontTables), which the ttf package's truncation test relies
on: a prefix that holds every other table parses, and only Subset
fails.
