// Package kvjs embeds the JavaScript ASCON-KV client (kv.js) so Go
// servers can serve it as a static asset from the module that owns
// it, keeping a single source of truth.
package kvjs

import "embed"

// FS holds kv.js at its root.
//
//go:embed kv.js
var FS embed.FS
