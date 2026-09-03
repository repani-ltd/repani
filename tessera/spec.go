package tessera

import _ "embed"

//go:embed TESSERA.t
var spec string

// Spec returns the specification, TESSERA.t, embedded at build time.
// It states the page and the tile and takes cells, ink and authoring
// from RASTER.t by reference; the CLI prints both (tessera spec).
func Spec() string { return spec }
