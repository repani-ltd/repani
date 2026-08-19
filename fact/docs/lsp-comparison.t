FACT vs LSP for agentic Go development

Design rationale, not normative material — the spec (SPEC.t) defines the
format; this note records why a generated projection exists in a world
that already has gopls. The short version: LSP staleness under agent
editing load is a structural property of the protocol, not a tuning
problem, and FACT sidesteps the staleness class instead of mitigating it.

# The staleness mechanism

gopls is built around a human editing model: one file open, changes
arriving keystroke-by-keystroke through didChange, hundreds of
milliseconds between actions. Its pipeline is deliberately asynchronous —
edits invalidate an in-memory snapshot, re-typechecking of the changed
package and its reverse dependencies happens lazily, and diagnostics are
debounced and pushed whenever ready, not when asked.

An agent breaks every one of those assumptions:

.item It writes whole files to disk, so the server learns of changes via
file-watcher events (extra latency, debouncing) rather than in-band
buffer edits.
.item It edits several files back-to-back, then immediately asks a global
question.
.item The protocol has no barrier primitive — no standard request meaning
"apply everything pending and answer only from a fully reconciled
state."

A query landing mid-invalidation is answered from the previous snapshot:
stale references, phantom errors on code already fixed, or a clean bill
of health on code already broken. The killer is not that answers are
always wrong but that the agent cannot tell when they are — "no
diagnostics" is indistinguishable from "server hasn't caught up."

Some of this is harness plumbing rather than protocol destiny (eager
sync, version-correlated diagnostics shrink the window), but nothing
closes it, and no mainstream harness does it rigorously.

# Why agents converge on go build

The compiler is synchronous and hermetic: it reads what is on disk now,
and when it returns, its answer is complete and true. It is the only
oracle in the Go toolchain whose freshness is guaranteed by its calling
convention. Field observation: agents editing at machine speed end up
trusting go build/go vet and treating LSP answers as hints — but
build only validates; it answers no navigation question at all.

# FACT's position

Don't run a stale-prone query server beside the compiler — run the
compiler as the indexer, synchronously, at the edit event. The
PostToolUse hook triggers fact project after each .go edit: a full
go/types typecheck of the package, materialized as a greppable index
plus an impact diff.

Measured (kv/store, warm cache): go build 0.13s, fact project ~0.2s —
the same order. The typecheck the agent was going to pay for anyway now
also yields compile validation, a refreshed navigation index, and the
regeneration diff as the impact report. And the projection is never
silently stale: it is either regenerated (hook succeeded) or loudly
behind (hook warned; fact project -check fails) — "loud when wrong" is
precisely the property LSP under agent load lacks.

.table 24L 26L *L
Property | LSP (gopls) | pkg.fact
Freshness after an edit burst | Eventual, unsignaled | Synchronous (hook) or loudly stale (-check)
"No errors" trustworthy? | No — may mean "not caught up" | Yes — regeneration is a typecheck
Navigation cost per question | One RPC round-trip to a live server | One grep, no server
Works toolchain-free (fresh clone, sandbox, review bot) | No — needs server + workspace load | Yes — committed file
Impact of an edit | Not offered | Regeneration diff (SPEC §8, §11.1)
Setup cost | Workspace load: seconds–minutes, high RSS | None (committed); ~0.2s/package to regenerate
.end

# Honest limits

.item pkg.fact answers the declaration layer only. Position-precise
queries — references to a local variable, hover at a point, rename
across statements — remain LSP territory by design (SPEC §11.3: bodies
are computation, file is the handoff).
.item On very large modules, per-edit regeneration of a heavily-depended-on
package costs more than 0.2s — though still per-package, never
workspace-wide.
.item During read-only exploration (no edits in flight), LSP staleness does
not bite; the failure mode is specifically the edit-heavy loop.

The two are complements, not rivals: FACT for the between-edits questions
agents actually ask (what exists, what shape, who calls, what
implements — at grep cost, with guaranteed freshness), LSP for
statement-level precision inside a file the agent has already opened.
