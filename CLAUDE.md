# kiosk

The public stand a publication is collected from: an HTTP server
that answers GET and HEAD and nothing else. Module
`repani.com/kiosk`, standard library only. KIOSK.t is the contract
and the decision ledger — read it before changing any default it
states, and record a new decision there rather than in a code
comment.

The package was factored out of three daemons (almanac's `web` +
`cmd/almanacd`, the retired `_attic/quietcasting-go`'s `web` +
`cmd/qcweb`, and `kv`'s `cmd/kvserve`) after a production-readiness
review on 2026-09-06. KIOSK.t's "Findings" section is that review;
every default here answers one of its items. Do not reintroduce a
lifecycle in a daemon — a daemon builds routes and calls Serve.

## The lines that must not move

- GET and HEAD only. POST, SSE, WebSockets, sessions, cookies and
  authentication are out of scope, not unimplemented: the method
  gate is the contract made structural. A streaming endpoint is
  what cost the last server its write timeout.
- No TLS, ever. The reverse proxy terminates it, owns the
  certificate, and sets Strict-Transport-Security. kiosk sets only
  the headers an origin can set for itself.
- Timeouts are defaults, not options. A new bound may be added; a
  zero one may not be introduced.
- Conditional requests go through `http.ServeContent`. Hand-rolled
  `If-None-Match` comparison is the bug this package exists to
  stop, and it broke identically in two codebases.
- Content types are required and never sniffed.

## Style

Follows `~/repos/CLAUDE.md` for the module rules, plus almanac's:
ASCII in comments, `any` not `interface{}`, architectural prose in
`doc.go` and one-line file comments, no speculative abstraction,
tests beside the code with a regression test for every fixed bug.

## Build and test

    go build ./... && go test ./...

Lab modules are exempt from the published-CLI rule until something
outside the module consumes them; kiosk ships no CLI at all.
