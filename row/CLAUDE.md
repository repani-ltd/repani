# row (repani.com/row) — typed rows

ROW: a row of typed items as bytes, each item's first byte its type,
no field names, rows within rows four levels deep; and a text, one
item per line, for export, inspection and edits. **ROW.t is the
normative source of truth**, DESIGN.t its reasons and ledger. ROW v1
(spec, decisions, history) is retired to `~/repos/_attic/row` and binds
nothing here. Read ROW.t before touching the encoder, decoder, text or
reader.

## Layout

- `ROW.t`, `DESIGN.t` -- the spec, and why it is so.
- `doc.go` -- the reference `row spec` prints (embedded); keep it in
  step with ROW.t (`spec_test.go` checks its sections).
- `row.go` -- types, `norm` (the one check of a program's value,
  shared by Encode and Format), Encode/Decode.
- `text.go` -- Format/Parse and the labelled pair; canonical output,
  lenient input (ROW.t 6.5's table).
- `read.go` -- the reader (ROW.t 8.1).
- `civil.go` -- Hinnant's day algorithms, the same as row.js's.
- `row.js` -- the JavaScript twin, no imports, embedded as `JS()`.
- `testdata/vectors.json` -- THE VECTORS. `valid` and `labeled` come
  from an independent encoder written from ROW.t alone
  (`testdata/gen/gen.py`); `lenient`, `badBinary`, `badText` and
  `badLabeled` were written by hand from ROW.t's tables
  (`testdata/gen/extra.py`, which merges the two). Go and JS must
  both pass them. A changed vector is a changed format: never edit
  one to make a test pass. Regenerate, from `row/`:
  `python3 -I testdata/gen/gen.py /tmp/valid.json && python3 -I
  testdata/gen/extra.py /tmp/valid.json testdata/vectors.json`.
- `gen.py` stays independent by how it is changed: whoever updates it
  (an agent of its own, after a ROW.t change) reads ROW.t and nothing
  else here -- no Go, no row.js, no vectors, not extra.py.
- `cmd/row` -- `check`, `fmt [-w]`, `encode`, `decode`, `spec`.

## Test

`go test ./row/...` runs the vectors in Go, and in headless Chrome for
row.js (`repani.com/chrometest`; skipped without Chrome). Fuzz both
directions after any change to encoding or text:
`go test ./row -run X -fuzz FuzzDecode` and `-fuzz FuzzParse`.

Install the CLI after changing it: `GOBIN=~/bin go install ./row/cmd/row`.
