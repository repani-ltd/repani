// Package integration holds ASCON-KV's end-to-end tests: Go clients
// against an in-process server (e2e_test.go), and the JavaScript
// client kv.js checked against the Go implementation and a live
// server, run in headless Chrome when it is installed (js_test.go,
// through repani.com/chrometest). It has no code of its own.
package integration
