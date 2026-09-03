package raster

import _ "embed"

//go:embed RASTER.t
var spec string

// Spec returns the specification, RASTER.t, embedded at build time so
// the text in a binary is the one its compiler was built beside. A
// CLI over a raster format prints it after its own (tessera spec
// does).
func Spec() string { return spec }
