# kv

ASCON-KV: append-only key-value vault store with client, server, and wire
protocol. See PROTOCOL.txt and CLIENT.txt for the protocol and client specs.

The Ascon primitives (XOF128, AEAD128) come from the public module
`repani.com` (`repani.com/ascon`, checkout `../../repani`);
the workspace `go.work` at the repo root supplies it, so go.mod
carries no replace or require for it.

## Exported surface has consumers outside this module

`../almanac` imports `kv/vault`, `kv/client`, `kv/server` and `kv/js`
(it is an ASCON-KV application: pages are vault items). Before
removing or changing any exported identifier, grep every module in
`~/repos` -- including `~/repos/_attic` -- for its users; "unused in
this module" is not "dead". The cautionary case: `store.LogStore.Rewrite`
was deleted on 2026-07-21 as dead code while quietcasting-go's qcpub
depended on it, and that tool silently stopped building for a month.
Conversely, nothing in kv is a general primitive: vault types,
client, server and wire are ASCON-KV protocol surface and stay here;
primitives (ascon, golay, lz4s) live in the public module. `store`
included (assessed 2026-08-19): its item identity (`Entry.ID`, a
domain-separated XOF), order (canonical `(TsMs, EntryID)`), set hash
and on-disk record are all PROTOCOL.txt definitions, so it is the
protocol's entry set with durability, not a generic log. Extracting a
generic `logset` primitive only makes sense when a second protocol
needs one -- two consumers shape the interface, one does not.

## Build and test

```
go build ./...
go test ./...
```

The crash-safety chaos test (workers SIGKILLed mid-append) is opt-in
and skipped by a normal `go test`:

```
KV_CHAOS=1 go test ./store -run Chaos -v
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
`kind:Name` marks an instance -- `type:Store`, `func:NewMemStore`,
`method:MemStore_Add`. Facts per entity: `.kind`, `.file`, `.fields`,
`.field_F_type`, `.methods`, `.implements`, `.sig`, `.exported`, `.calls`,
`.receiver`.

Query recipes (run at repo root):

```
grep '^type:Store\.' store/pkg.fact                       # everything about a type
grep -r --include=pkg.fact 'implements.*type:Store' .     # what implements it
grep -r --include=pkg.fact 'calls.*NewMemStore' .         # who calls it
grep '^func:NewMemStore\.sig' store/pkg.fact              # one signature
grep '^type:Store\.file' store/pkg.fact                   # defining file
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
  (per-clone; reinstall at the repo root from
  `../repani/fact/docs/pre-commit` after a fresh clone).
- Manual fallback when hooks are missing or report errors:
  `fact project -w <pkg-dir>` regenerates, `fact project -check <pkg-dir>`
  verifies freshness (`fact` is built from the public repo:
  `cd ../repani && GOBIN=~/bin go install ./fact/cmd/fact`).
- If pkg.fact and source seem to disagree, the projection is stale: trust
  the source, then regenerate.
