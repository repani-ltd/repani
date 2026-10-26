# fact (repani.com/fact) — FACT format toolchain

`fact` is a CLI implementing the FACT format (v0.3). **SPEC.t is the
normative source of truth** — when code and spec disagree, the spec wins;
read it before changing parser or validator behaviour.

FACT is a format for configuration and data files: one typed fact per
line, references that must resolve, a canonical form. Its uses in these
repos are station configuration, subject files, event logs and ledgers.
The Go projection it began as (`pkg.fact`) was retired on 2026-09-06;
SPEC.t §1.1 and §11 record why.

## Commands

- Run: `go run ./cmd/fact <validate|fmt|encode|decode> [file]` (stdin if no
  file); `fmt -w FILE` rewrites in place. Exit codes: 0 ok, 1 invalid input
  or failure, 2 usage.
- `fact spec` prints the embedded reference (the package comment of
  `doc.go`); a test asserts its sections exist and that no source
  furniture leaks in.
- `docs/post-commit` (this repo only, install with `cp fact/docs/post-commit
  .git/hooks/`) rebuilds `~/bin/fact` and `~/bin/pica` after commits
  touching Go source, so the installed binaries match the committed
  toolchain.

## Layout

- `SPEC.t` — FACT format specification v0.3 (normative)
- `*.go` at this level (package `fact`, import `repani.com/fact`) — the
  format core: line parser, type/value checks, set-level validation
  (`Load` = `Parse` + `Validate`), canonical serializer, JSON codec,
  `Bind` (nested-map view)
- `cmd/fact/main.go` — thin CLI wrapper (flag parsing + I/O only; no format
  logic); `main_test.go` drives the commands in-process through `run`
- `docs/stream-profile.t` — the stream profile

## Hard rules

- **Standard library only.** The format core imports nothing outside
  the standard library. The CLI adds one primitive of this module,
  `repani.com/durable` (itself standard library only), so `fmt -w`
  replaces the author's file whole or not at all.
- **No trees.** The data model is a flat set of fact lines (SPEC §5). Parsing
  is line-local; validation is line-local plus one set pass. Do not introduce
  nested structures to "help".
- **Case-sensitive segments** (`[a-zA-Z][a-zA-Z0-9_]*`, v0.2). Never
  normalize case.
- The type grammar is finite: exactly 21 legal shapes (7 base types × {plain,
  `?`, `list()`}). Reject everything else with `E004`.
- Errors use the normative codes/messages of SPEC §14, one error per line,
  no cascading.
- Canonical output must be deterministic and byte-identical for equal inputs
  (SPEC §8): bytewise-sorted lines, canonical spacing, LF, one trailing
  newline.

## Implementation decisions (this repo)

- Scalar values are stored as **canonical string tokens** and normalized at
  parse time: floats via `strconv.FormatFloat(v, 'g', -1, 64)`, `-0` → `0`,
  strings re-encoded with `json` (HTML escaping off, `fact.Quote`). This is
  what makes the JSON round-trip guarantee (`decode(encode(F)) ==
  canonical(F)`) hold. `checkValue` is the only canonicaliser.
- **No reflection, no struct binding.** A program writes its file as
  fact lines and runs them through `Load` + `Canonical` (the checker
  stays the one authority on spelling); it reads with `Load` + `Bind`.
  `kv/server/record.go` is the pattern. `Marshal`/`Unmarshal` were
  removed on 2026-10-26: two five-field users did not earn a
  reflection binder in the format core. `git log -- fact/marshal.go`
  finds the removal commit; revert it if a consumer appears that
  hand-written lines cannot serve.
- `float` accepts any JSON number (including plain integers) since float
  canonicalization can itself produce e.g. `5`; the type annotation
  disambiguates.
- Fact-line splitting: the **first `=`** separates value; the **last `:`**
  before it separates key from type (keys hold at most one marker colon,
  types hold none).
