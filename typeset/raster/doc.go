// Package raster is a page of colored text cells: panels of rows by
// columns in a geometry the caller chooses, a fixed cell repertoire,
// and an ink per cell, two bytes a cell in the page's bytes. The
// authoring language, with aliases for a page's idioms, compiles to a
// page; the renderers (plain text, ANSI, HTML) read its cells, which
// also derive the links, bracketed spans. The specification is
// RASTER.t, embedded and returned by Spec, and JS returns the
// JavaScript reader and painter.
package raster
