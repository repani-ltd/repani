# row (repani.com/row) — typed rows

ROW v1: a short sequence of typed values as bytes, each item's first
byte its type, no field names. Made for ASCON-KV values (client format
byte 0x01) and independent of it. **ROW.t is the normative source of
truth** -- FROZEN 2026-10-12, permanently: the type set is closed and
every tag is assigned, so a change is a new format, never a revision.
Code may change only to match ROW.t more exactly (fixes, speed);
anything that changes bytes, text or errors is not allowed. Read ROW.t
before touching the encoder, decoder or text notation.

## Layout

- `ROW.t` -- the specification and its decisions (Appendix A).
- `doc.go` -- the reference `row spec` prints (embedded); keep it in
  step with ROW.t.
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
