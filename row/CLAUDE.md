# row (repani.com/row) — typed rows

**In transition (2026-11-21).** `ROW.t` is the DRAFT of a new ROW,
written from a blank page and kept clean (decided design only); its
reasons, the alternatives weighed and the decision ledger are
`DESIGN.t`. ROW v1 (its spec, decisions and history) is in
`~/repos/_attic/row`. Everything below describes the v1 code still in
this directory, which stays until the draft replaces it; do not read
v1's rules into the draft.

ROW v1: a short sequence of typed values as bytes, each item's first
byte its type, no field names. Its spec, `~/repos/_attic/row/ROW.t`,
is what the code implements; until the code is replaced it may change
only to match that spec more exactly (fixes, speed).

## Layout

- `ROW.t`, `DESIGN.t` -- the draft and its reasons (above).
- `doc.go` -- the reference `row spec` prints (embedded), v1's.
- `row.go` (types, the one validator `fromGo`, binary Encode/Decode),
  `text.go` (Format/Parse), `civil.go` (Hinnant's day algorithms, the
  same as row.js's).
- `row.js` -- the JavaScript twin, embedded as `row.JS` for serving;
  it imports `./lz4s.js` (serve `lz4s.JS()` beside it). Strings of
  242+ bytes are lz4s-compressed when shorter, and the stream must be
  `lz4s.Compress`'s own: lz4s.js's `compress` is held to Go's by the
  lz4s fixture, and ROW depends on both staying byte-identical.
- `testdata/vectors.json` -- THE FROZEN VECTORS, written by an
  independent encoder; Go and JS must both pass them. A changed vector
  is a changed format: never edit one to make a test pass.
- `cmd/row` -- `check FILE`, `encode`, `decode`, `spec`.

## Test

`go test ./row/...` runs the vectors in Go, and in headless Chrome for
row.js (`repani.com/chrometest`; skipped without Chrome). Fuzz both
directions after any change to encoding or text:
`go test ./row -run X -fuzz FuzzDecode` and `-fuzz FuzzParse`.

Install the CLI after changing it: `GOBIN=~/bin go install ./row/cmd/row`.
