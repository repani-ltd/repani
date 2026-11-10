package ascon

import "embed"

// JS holds ascon.js, the JavaScript implementation, at its root, so a
// server can serve it from the module that owns it.
//
//go:embed ascon.js
var JS embed.FS
