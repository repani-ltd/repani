# kv

ASCON-KV: append-only key-value vault store with client, server, and wire
protocol. The documents are pica (`pica check FILE.t`): PROTOCOL.t the
wire, normative and versioned by the frame's `ver` byte; CLIENT.t the
conventions above it; DESIGN.t the reasons, decisions, known gaps and
designs not yet built. History is git's: no changelog, and no history
in comments.

The Ascon primitives (XOF128, AEAD128) come from the public module
`repani.com` (`repani.com/ascon`, checkout `../../repani`);
the workspace `go.work` at the repo root supplies it, so go.mod
carries no replace or require for it.

## Exported surface has consumers outside this module

`../almanac` imports `kv/vault`, `kv/client`, `kv/server` and `kv/js`
(it is an ASCON-KV application: pages are vault items), and
`../webapp` serves and stages `js/kv.js` to every web app (iou), whose
`vaults.js` builds on its Client contract -- after changing kv.js,
run webapp's and iou's browser tests too (see their CLAUDE.md). Before
removing or changing any exported identifier, grep every module in
`~/repos` -- including `~/repos/_attic` -- for its users; "unused in
this module" is not "dead". The cautionary case: `store.LogStore.Rewrite`
was deleted on 2026-07-21 as dead code while quietcasting-go's qcpub
depended on it, and that tool silently stopped building for a month.
Conversely, nothing in kv is a general primitive: vault types,
client, server and wire are ASCON-KV protocol surface and stay here;
primitives (ascon, golay, lz4s) live in the public module. `store`
included: see DESIGN.t, "The store is protocol, not a primitive".

## Testing

The crash-safety chaos test (workers SIGKILLed mid-append) is opt-in
and skipped by a normal `go test`:

```
KV_CHAOS=1 go test ./store -run Chaos -v
```

