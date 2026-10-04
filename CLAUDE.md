# repani (module repani.com)

One Go module. Top-level directories are either products with
their own CLAUDE.md (`pica/`, `fact/`, `kiosk/`) or primitive
packages (`ascon/`, `golay/`, `lz4s/`) or the typesetting family
(`typeset/`: one directory, one package per member -- `typeset/tab`,
`typeset/format`, `typeset/wrap`, `typeset/tbl`, `typeset/raster`
-- every member under the primitive rule, so a
product never lives there); this file holds what is common.

Primitive packages import only the standard library and other
primitive packages (never a product package: pica, fact,
kiosk), carry no protocol constants or types (a primitive may
not know what a frame, slot, page or vault is), and are
append-only: a changed algorithm is a new package, not a
revision, which is what makes one primitive safe to build on
another. Each ships known-answer or round-trip tests; the tests
are the contract.

- Import paths are `repani.com/<dir>/...`; the module is the repo
  root, so never add a nested go.mod. Experiments and scratch work
  live outside this repo (`~/repos/research`, `~/repos/tmp`), never
  in it; retired work goes to `~/repos/_attic` with history.
- READMEs and CLAUDE.md stay Markdown (consumed by GitHub / Claude
  Code); all other documentation is pica `.t`.
- `TASKS.t` at the repo root is the task ledger: parked work and
  candidates with their triggers, one `.term` per task, removed
  when done (the resulting decision goes to the project's
  DESIGN.t). Add tasks there, not to memory or chat.
- CLIs live in `<project>/cmd/<tool>`: `pica/cmd/pica`,
  `fact/cmd/fact`.
- Install the post-commit hook once per clone with
  `cp fact/docs/post-commit .git/hooks/`; it rebuilds `~/bin/fact`
  and `~/bin/pica` after commits touching Go source.
- Tools explain themselves: every published CLI ships `TOOL spec`
  (the reference, embedded in the binary) and `TOOL check|validate`
  (errors on a file). The full rule: "Tools explain themselves" in
  `~/repos/CLAUDE.md`.
- Documentation in pica: `pica spec`, `pica check FILE.t` (see
  `~/repos/CLAUDE.md`).
- Build and test the whole module: `go build ./... && go test ./...`.
- Versions are annotated git tags `vMAJOR.MINOR.PATCH` on `main`,
  one series for the whole module; v0.x while the API moves (MINOR
  = API change, PATCH = fix/docs), v1.0.0 = frozen surface, and a
  breaking successor is a new module path, never `/v2`. Procedure
  and history: `repani-private/ops/runbooks.t`, "Publish a module
  version".
