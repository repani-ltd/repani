package row

import _ "embed"

//go:embed row.js
var js string

// JS returns row.js, the JavaScript implementation, embedded at build
// time so a server ships the code its Go side was built beside. It is
// an ES module with no imports: serve it as text/javascript.
func JS() string { return js }
