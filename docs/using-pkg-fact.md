# Using `pkg.fact` files (agent guide)

This is a paste-ready section for the CLAUDE.md (or equivalent agent
instructions) of any project whose packages — or dependencies — carry
generated `pkg.fact` files. Copy everything below the line.

---

## Navigating Go code via pkg.fact

Some package directories contain a generated `pkg.fact` file: a flat,
greppable index of that package's declaration layer — types, fields, method
sets, computed interface satisfactions, signatures, resolved call edges
(functions and methods), package-level consts and vars, and defining files. **Check it before reading source**: navigation questions
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
| Which file defines `Service`? | `grep '^type:Service\.file' pkg.fact` |
| All exported functions | `grep '\.exported: bool = true' pkg.fact` |
| All methods of `Service`, with signatures | `grep '^method:Service_' pkg.fact` |
| Fields of `Service` and their types | `grep '^type:Service\.field' pkg.fact` |
| Package-level consts and vars (error sentinels etc.) | `grep '^const:\|^var:' pkg.fact` |
| Same question, whole module | `grep -r --include=pkg.fact 'implements.*Poster' .` |
| Who calls `Service.Settle`, module-wide? | `grep -r --include=pkg.fact 'calls.*"Service.Settle"' .` |
| Which package declares `Marshal`? | `grep -rl --include=pkg.fact '^func:Marshal\.' .` |
| Which packages import `mymod/ledger`? | `grep -r --include=pkg.fact 'imports.*"mymod/ledger"' .` |
| Exported API of a dependency | `grep '\.sig' facts/<import-path>/pkg.fact` |

In module-wide (`-r`) hits the printed path is the qualifier: the projection
lives in its package directory, so `ledger/pkg.fact:...` means package
`ledger`. Callees in `calls` are source-qualified strings — grep for
`"pkg.Func"` or `"pkg.Type.Method"` exactly as a call site would write it
(interface calls appear as the static callee, e.g. `"ledger.Poster.Post"`).

`file` values name the defining source file — together with the symbol name,
that is the handoff into source (open the file, grep the name). The
projection covers declarations only; for statement-level questions (where is
this field assigned, what does this branch do) follow `.file` into source.

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
- **With the hooks wired (below), regeneration is automatic**: the
  PostToolUse hook refreshes the projection after each `.go` edit —
  running goimports on the edited file and surfacing the diff or compile
  errors in-session — and the pre-commit hook stages pkg.fact into the
  same commit as the source change. When the hook reports a goimports
  rewrite, re-read the file before editing it again.
- **Manual fallback** when hooks are missing or report errors: regenerate
  with `fact project -w <pkg-dir>` and commit the result with the source
  change.
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

Wire it in three places:

1. **Claude Code hook** (agent sessions): a `PostToolUse` hook keeps every
   projection fresh as the agent edits, and feeds the projection diff back
   to the agent as the impact report of each edit. In `.claude/settings.json`:

   ```json
   {
     "hooks": {
       "PostToolUse": [
         {
           "matcher": "Edit|Write",
           "hooks": [{ "type": "command", "command": "fact hook" }]
         }
       ]
     }
   }
   ```

   `fact hook` reads the hook payload on stdin; it acts only when the edited
   file is a `.go` file in a package that carries a `pkg.fact`. It runs
   goimports on the edited file (formatting plus import fixing — test files
   included), regenerates the projection, and stays silent when the edit was
   declaration-neutral. If the package does not compile, the hook surfaces
   the compiler diagnostics in-session instead — the edit→build→read-errors
   loop collapses into the edit itself. It never blocks an edit; the CI
   gate catches any staleness later.
2. **Pre-commit** (or editor-on-save): regenerate the projections of the
   packages you touched, so pkg.fact travels in the same commit as the
   source change it reflects. A ready-made script lives in the flat repo
   at `docs/pre-commit` — copy it to `.git/hooks/pre-commit` (and re-copy
   after any fresh clone; git hooks are per-clone). It regenerates and
   stages pkg.fact for every package with staged `.go` changes, and warns
   without blocking when a package does not compile.
3. **CI**: `fact project -check <pkg-dir>` for each projected package —
   exits 1 if a committed pkg.fact is stale. Staleness is a byte-exact
   comparison, so the gate never false-positives on a clean tree.

The target directory must be inside a Go module (the generator resolves
imports through the `go` toolchain), and each invocation projects exactly
one package.
