// Package raster is a page of colored text cells, one byte per cell:
// panels of rows by columns in a geometry the caller chooses, a fixed
// cell repertoire, and in-band row-scoped ink. A Canvas holds the
// page with its ink out of band, one glyph and one ink per cell; the
// authoring language, with aliases for a page's idioms, compiles to a
// canvas; Encode and Decode move between a canvas and the bytes; the
// renderers (plain text, ANSI, HTML) work on the canvas, which also
// derives the links, bracketed spans. The specification is RASTER.t,
// embedded and returned by Spec, and JS returns the JavaScript decoder
// and painter; tessera (repani.com/tessera) is the first geometry.
package raster
