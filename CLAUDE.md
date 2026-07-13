# typeset

Text typesetting library: wrapping, hyphenation, tables, with a PDF backend
(`pdf/`, `pdf/ttf/`) and the `pica` renderer CLI (`cmd/pica`).

## Build and test

```
go build ./...
go test ./...
```

## Navigating Go code via pkg.fact

Every package directory contains a generated `pkg.fact` file: a flat,
greppable index of the package's declaration layer -- types, fields, method
sets, computed interface satisfactions, signatures, resolved call edges, and
source locations. Check it before reading source: navigation questions
("what implements this?", "who calls this?", "what shape is this type?")
are one grep against pkg.fact, and interface satisfaction is unanswerable
by grepping source at all (Go satisfaction is structural -- implementing
types never name the interface).

Each line is one self-contained fact: `key: type = value`. A segment
`kind:Name` marks an instance -- `type:Doc`, `func:DefaultLayout`,
`method:Doc_Render`. Facts per entity: `.kind`, `.loc`, `.fields`,
`.field_F_type`, `.methods`, `.implements`, `.sig`, `.exported`, `.calls`,
`.receiver`.

Query recipes (run at repo root; the root package's index is `./pkg.fact`):

```
grep '^type:Doc\.' pkg.fact                              # everything about a type
grep -r --include=pkg.fact 'implements.*type:Doc' .      # what implements an interface
grep -r --include=pkg.fact 'calls.*DefaultLayout' .      # who calls it
grep '^func:DefaultLayout\.sig' pkg.fact                 # one signature
grep '^type:Doc\.loc' pkg.fact                           # "file.go:line" handoff
```

`.loc` values are `"file.go:line"` relative to the package directory. The
projection covers declarations only; for statement-level questions (where
is this field assigned, what does this branch do) follow `.loc` into the
source file.

Rules:

- Never edit pkg.fact: it is generated and read-only. Edit the Go source.
- After changing any Go declaration, regenerate that package's projection
  and commit it in the same change: `fact project -w <pkg-dir>`
  (`fact` is built from the sibling `../flat` repo: `go install ./cmd/fact`).
- Read the pkg.fact diff as the impact report of your edit.
- If pkg.fact and source seem to disagree, the projection is stale: trust
  the source, then regenerate (`fact project -check <pkg-dir>` verifies
  freshness).
