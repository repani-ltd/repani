// Package raster is the raster format: rows of exactly forty code
// points, each row a row number, a role and segments -- stretches
// in one foreground, one background and one link target or none --
// and a row count. It holds no content type: tables, paragraphs or
// anything else a producer sets on rows.
//
// Append encodes records and Decode decodes them, both checking that
// each is valid and canonical, so one screen has one encoding. A
// renderer reads a Row's Segments. A Page is what a renderer that
// takes updates holds: Apply applies records to it, and Records
// returns it as a whole page. A producer writes a row as a Draft,
// putting text at columns, and Row derives the canonical segments,
// so none computes segments or counts link columns.
//
// The package imports nothing that typesets: no table language, no
// line breaker, no hyphenation patterns, so a renderer stays small.
package raster
