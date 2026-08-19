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
defining files. Check it before reading source: navigation questions
("what implements this?", "who calls this?", "what shape is this type?")
are one grep against pkg.fact, and interface satisfaction is unanswerable
by grepping source at all (Go satisfaction is structural -- implementing
types never name the interface).

Each line is one self-contained fact: `key: type = value`. A segment
`kind:Name` marks an instance -- `type:Doc`, `func:DefaultLayout`,
`method:Doc_Render`. Facts per entity: `.kind`, `.file`, `.fields`,
`.field_F_type`, `.methods`, `.implements`, `.sig`, `.exported`, `.calls`,
`.receiver`.

Query recipes (run at repo root; the root package's index is `./pkg.fact`):

```
grep '^type:Doc\.' pkg.fact                              # everything about a type
grep -r --include=pkg.fact 'implements.*type:Doc' .      # what implements an interface
grep -r --include=pkg.fact 'calls.*DefaultLayout' .      # who calls it
grep '^func:DefaultLayout\.sig' pkg.fact                 # one signature
grep '^type:Doc\.file' pkg.fact                          # defining file
```

`.file` values name the defining source file relative to the package
directory -- together with the symbol name, that is the handoff into source
(open the file, grep the name). Callees in `calls` are source-qualified
strings: grep for `"pkg.Func"` or `"pkg.Type.Method"` exactly as a call
site would write it. The projection covers declarations only; for
statement-level questions (where is this field assigned, what does this
branch do) follow `.file` into the source.

Rules:

- Never edit pkg.fact: it is generated and read-only. Edit the Go source.
- A PostToolUse hook (`.claude/settings.json` → `fact hook`) regenerates
  the projection automatically after each `.go` edit, runs goimports on
  the edited file, and surfaces the projection diff — or the package's
  compile errors — in-session. When it reports a goimports rewrite,
  re-read the file before editing it again.
- Read the pkg.fact diff as the impact report of your edit.
- pkg.fact travels in your commit automatically: the git pre-commit hook
  regenerates and stages it for packages with staged `.go` changes
  (per-clone; reinstall from `../flat/docs/pre-commit` after a fresh
  clone).
- Manual fallback when hooks are missing or report errors:
  `fact project -w <pkg-dir>` regenerates, `fact project -check <pkg-dir>`
  verifies freshness (`fact` is built from the sibling `../flat` repo:
  `go install ./cmd/fact`).
- If pkg.fact and source seem to disagree, the projection is stale: trust
  the source, then regenerate.
