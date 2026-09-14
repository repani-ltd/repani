// Package tessera is a page format: a raster (repani.com/typeset/raster)
// of 34 columns by 28 rows by 4 panels, 7,616 bytes. The specification
// is TESSERA.t, embedded and returned by Spec; what it adds to
// RASTER.t is the geometry. Its tile, the quietcasting slot, was
// withdrawn on 2026-09-14 when raster went to two bytes a cell: the
// quietcasting binding is to be redefined (TASKS.t).
package tessera
