package row

import "embed"

// JS holds row.js, the JavaScript implementation, at its root, so a
// server can serve it from the module that owns it.
//
//go:embed row.js
var JS embed.FS
