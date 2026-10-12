package lz4s

import _ "embed"

//go:embed js/lz4s.js
var js string

// JS returns the JavaScript implementation, js/lz4s.js (the decoder
// and Compress), embedded at build time so a server ships the code its
// encoder was built beside.
// It is an ES module: serve it as text/javascript and import it.
func JS() string { return js }
