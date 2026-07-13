# Using `pkg.fact` files (agent guide)

This is a paste-ready section for the CLAUDE.md (or equivalent agent
instructions) of any project whose packages — or dependencies — carry
generated `pkg.fact` files. Copy everything below the line.

---

## Navigating Go code via pkg.fact

Some package directories contain a generated `pkg.fact` file: a flat,
greppable index of that package's declaration layer — types, fields, method
sets, computed interface satisfactions, signatures, resolved call edges, and
source locations. **Check it before reading source**: navigation questions
("what implements this?", "who calls this?", "what shape is this type?") are
one grep against pkg.fact, and some are unanswerable by grepping source at
all (interface satisfaction in Go is structural — implementing types never
name the interface).

Each line is one self-contained fact: `key: type = value`. Keys are fully
qualified, so every grep hit is unambiguous. A segment `kind:Name` marks an
instance — `type:Service`, `func:Submit`, `method:Service_Settle`.

### Query recipes

| Question | Command |
|---|---|
| Everything about type `Service` | `grep '^type:Service\.' pkg.fact` |
| What implements interface `Poster`? | `grep 'implements.*type:Poster' pkg.fact` |
| Who calls `Errorf`? | `grep 'calls.*Errorf' pkg.fact` |
| Signature of `Submit` | `grep '^func:Submit\.sig' pkg.fact` |
| Where is `Service` defined? | `grep '^type:Service\.loc' pkg.fact` |
| All exported functions | `grep '\.exported: bool = true' pkg.fact` |
| Same question, whole module | `grep -r --include=pkg.fact 'implements.*Poster' .` |

`loc` values are `"file.go:line"` — the handoff pointer. The projection
covers declarations only; for statement-level questions (where is this field
assigned, what does this branch do) follow `.loc` into the source file.

### Dependency facts

Projections of third-party packages, if present, live under a mirror tree at
the module root, keyed by import path:

```
facts/github.com/shopspring/decimal/pkg.fact
```

Same format, same queries; `pkg.path` and `pkg.version` inside the file
carry provenance. When a fact in your own packages mentions an external
symbol as a string (e.g. `calls: ... "decimal.Decimal.Add"`), the follow-up
hop is a grep into that dependency's `facts/` entry. A version-bump PR diffs
these files line-by-line — read that diff as the API impact analysis of the
upgrade.

### Rules

- **Never edit pkg.fact.** It is a generated, read-only lens over the Go
  source. To change a fact, edit the source it reflects.
- **Regenerate after changing any Go declaration** in a projected package:
  `fact project -w <pkg-dir>`. Commit the regenerated pkg.fact in the same
  commit as the source change.
- **Read the pkg.fact diff as the impact report** of your edit: renamed
  facts, changed signatures, and updated caller lists are exactly the blast
  radius — no noise.
- If pkg.fact and the source seem to disagree, the projection is stale:
  regenerate (`fact project -check <pkg-dir>` verifies freshness) and trust
  the source in the meantime.

### Generating pkg.fact for this project's own packages

The generator is the `fact` CLI (from the `flat` repo: `go install
<flat-repo>/cmd/fact` or `go build -o fact ./cmd/fact`). Then, per package:

```sh
fact project -w ./path/to/pkg     # writes ./path/to/pkg/pkg.fact, read-only
```

To project every package in a module:

```sh
go list -f '{{.Dir}}' ./... | while read -r dir; do fact project -w "$dir"; done
```

For a dependency (read-only source), project it **by import path** — the
version resolves through this module's `go.mod` and is recorded in the file
as `pkg.version` — and redirect output into the `facts/` mirror:

```sh
fact project -o facts/github.com/shopspring/decimal/pkg.fact github.com/shopspring/decimal
```

Wire it in twice:

1. **Pre-commit** (or editor-on-save): regenerate the projections of the
   packages you touched, so pkg.fact travels in the same commit as the
   source change it reflects.
2. **CI**: `fact project -check <pkg-dir>` for each projected package —
   exits 1 if a committed pkg.fact is stale. Staleness is a byte-exact
   comparison, so the gate never false-positives on a clean tree.

The target directory must be inside a Go module (the generator resolves
imports through the `go` toolchain), and each invocation projects exactly
one package.
