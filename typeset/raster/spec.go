package raster

import _ "embed"

//go:embed RASTER.t
var spec string

// Spec returns the specification, RASTER.t, embedded at build time so
// the text in a binary is the one its compiler was built beside. A
// CLI over a raster format prints it after its own.
func Spec() string { return spec }

//go:embed js/raster.js
var js string

// JS returns the JavaScript decoder, js/raster.js, embedded at build
// time so a server ships the decoder its compiler was built beside.
// It is an ES module: serve it as text/javascript and import it.
func JS() string { return js }
