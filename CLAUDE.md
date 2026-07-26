# flat — FACT format toolchain

`fact` is a CLI implementing the FACT format (v0.2). **SPEC.md is the
normative source of truth** — when code and spec disagree, the spec wins;
read it before changing parser/validator/generator behavior.

FACT's primary use case is **code projection**: `fact project` type-checks a
Go package and emits its declaration layer (signatures, fields, method sets,
computed interface satisfactions, call edges, `file` facts) as a flat,
greppable, canonical `.fact` file. Config files are the degenerate case.

## Commands

- Build: `go build ./...`
- Test: `go test ./...`
- Run: `go run ./cmd/fact <validate|fmt|encode|decode> [file]` (stdin if no file)
- Project: `go run ./cmd/fact project <pkg-dir>` (stdout), `-w` (write
  `<pkg-dir>/pkg.fact`, read-only), `-check` (freshness gate)
- Dependencies (SPEC §11.5): project by import path into the `facts/` mirror,
  e.g. `go run ./cmd/fact project -o facts/<import-path>/pkg.fact <import-path>`
  — the version resolves through go.mod and lands in the file as `pkg.version`
- **After changing any Go declaration**, regenerate that package's projection
  (`go run ./cmd/fact project -w ./fact`, likewise for `./project` and
  `./cmd/fact`) and read the pkg.fact diff as the impact report (SPEC §11.1).
  A `PostToolUse` hook (`.claude/settings.json` → `fact hook`) does this
  automatically after each `.go` edit and surfaces the diff in-session — it
  also runs goimports on the edited file and, when the package no longer
  builds, reports the compile errors as in-session context; the manual
  command remains the fallback when the hook reports an error.
- `docs/using-pkg-fact.md` is the paste-ready snippet consuming projects put
  in their own CLAUDE.md; keep it in sync with the §11.2 vocabulary.

## Layout

- `SPEC.md` — FACT format specification v0.2 (normative)
- `fact/` — the format core: line parser, type/value checks, set-level
  validation, canonical serializer, JSON codec
- `project/` — the Go projection generator (SPEC §11): `go/packages` +
  `go/types`, emits fact lines that are re-validated through `fact/`
- `cmd/fact/main.go` — thin CLI wrapper (flag parsing + I/O only; no format logic)

## Hard rules

- **Minimal dependencies.** The format core (`fact/`) is standard library
  only. The generator (`project/`) may additionally use `golang.org/x/tools`
  (`go/packages` for module-aware loading). Nothing else.
- **No trees.** The data model is a flat set of fact lines (SPEC §5). Parsing
  is line-local; validation is line-local plus one set pass. Do not introduce
  nested structures to "help".
- **Case-sensitive segments** (`[a-zA-Z][a-zA-Z0-9_]*`, v0.2): projected Go
  identifiers keep their casing — case is semantic (exportedness). Never
  normalize case.
- The type grammar is finite: exactly 21 legal shapes (7 base types × {plain,
  `?`, `list()`}). Reject everything else with `E004`.
- Errors use the normative codes/messages of SPEC §14, one error per line,
  no cascading.
- Canonical output must be deterministic and byte-identical for equal configs
  (SPEC §8): bytewise-sorted lines, canonical spacing, LF, one trailing
  newline. For projections, the regeneration diff is the impact analysis —
  determinism is the flagship property.

## Implementation decisions (this repo)

- Scalar values are stored as **canonical string tokens** and normalized at
  parse time: floats via `strconv.FormatFloat(v, 'g', -1, 64)`, strings
  re-encoded with `json` (HTML escaping off). This is what makes the JSON
  round-trip guarantee (`decode(encode(F)) == canonical(F)`) hold.
- `float` accepts any JSON number (including plain integers) since float
  canonicalization can itself produce e.g. `5`; the type annotation
  disambiguates.
- Fact-line splitting: the **first `=`** separates value; the **last `:`**
  before it separates key from type (keys hold at most one marker colon,
  types hold none).
- Call edges (`func:F.calls`) use the **all-str choice** of SPEC §6.4: every
  callee is a source-qualified string (`"errors.New"`, `"ledger.Poster.Post"`);
  no `extern:` stub facts. Applied consistently; change only spec-wide.
- The generator emits plain fact *lines* and pipes them back through
  `fact.Parse`/`Validate` — the projection is self-validating by construction.
- Generator scope is one package directory per invocation (non-test files),
  loaded via `go/packages`, so module-local and third-party imports resolve.
  Declarations whose names aren't legal segments (Unicode, leading `_`) are
  skipped.
