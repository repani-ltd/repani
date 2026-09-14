// Package raster is rows of colored text cells: forty columns wide, a
// fixed cell repertoire, an ink per cell, and a row record of up to
// 82 bytes as the unit of storage and update. The authoring language,
// with aliases for a page's idioms, compiles to a raster; the
// renderers (plain text, ANSI, HTML) read its cells, which also
// derive the links, bracketed spans. The specification is RASTER.t,
// embedded and returned by Spec, and JS returns the JavaScript reader
// and painter.
package raster
