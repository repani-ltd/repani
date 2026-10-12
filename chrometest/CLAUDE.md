# chrometest

Headless Chrome for Go tests: package `repani.com/chrometest` in the
public module, so browser code (web apps, kv.js) is tested from
`go test` with no JavaScript tooling. Imports the standard library
and `golang.org/x/net/websocket` only. No CLI.

CHROMETEST.t is the contract and the decision ledger -- read it
before changing a behaviour it states, and record a new decision
there rather than in a code comment.

## Test

`go test ./chrometest` from the repo root drives a real Chrome and
skips when there is none (`CHROMETEST_CHROME` names the binary).
