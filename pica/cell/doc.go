// Package cell sets a pica document on a raster page: the cell
// writer, a sibling of the text and PDF writers, for pages of
// colored text cells (repani.com/typeset/raster). The document's
// blocks are laid out as the mono PDF sets them (justified prose
// with hyphenation on the text writer's geometry; tables, verbatim
// and headings through pica.RenderBlock), flowed
// into columns across the panels of a geometry the caller chooses,
// and written as raster source in a vocabulary of aliases, one per
// construct: .title, .byline, .heading, .subheading, .tablehead,
// .total, .rights for whole rows, .label and .emph for spans. The
// writer never names a color: Vocabulary, the default, is teletext's
// convention (a white-on-blue title bar, yellow headings, cyan
// subheadings and labels), and an app restyles a document by
// supplying its own definitions of the same names.
//
// The page is the printed page on a grid: the masthead centered over
// the columns with a rule beneath it, headings on two-row slots
// wrapped at their role's measure, a hairline down each gutter wide
// enough to hold one, and press's column flow in whole lines, so
// that a document set on a panel the size of its printed column
// breaks where the PDF breaks. The document's .width and .cols are
// consumed, not rendered: the panel's width is the measure, and
// .cols is the default number of columns per panel. The margin
// column of every row is blank, so that a row beginning in ink has
// the cell its code needs; a gutter of blank cells separates columns
// for the same reason. Every rune
// must be in the raster repertoire; one that is not is an error
// naming the line, never a substitution. Brackets in prose become
// links, as on every raster page. A .link block is set as its title
// in brackets and reported in Result.Links, since the page cannot
// carry the URL: what a tap does with the bracketed text is the
// app's.
package cell
