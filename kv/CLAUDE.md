# kv

ASCON-KV: append-only key-value vault store with client, server, and wire
protocol. Package tree `repani.com/kv/...` in the public module, graduated
from repani-private on 2026-10-12. The documents are pica
(`pica check FILE.t`): PROTOCOL.t the wire, normative and versioned by the
frame's `ver` byte; CLIENT.t the conventions above it; DESIGN.t the
reasons, decisions, known gaps and designs not yet built. History is
git's: no changelog, and no history in comments.

Imports, beyond the standard library: `repani.com/ascon`,
`repani.com/durable`, `repani.com/fact` (the server's vault records,
kvserve's register-key file, the CLI's device file) and
`repani.com/kiosk` (this module), `golang.org/x/term` (the CLI's table
width) and `golang.org/x/sys/windows` (the log lock on Windows);
`repani.com/chrometest` in kv.js's tests.

## Exported surface has consumers outside this module

In repani-private: `webapp` serves and stages `js/kv.js` to every web app
(iou), whose `vaults.js` builds on its Client contract -- after changing
kv.js, run webapp's and iou's browser tests too (see their CLAUDE.md).
The repani server's deploy (private, outside this repo) builds kvserve
and kv by import path. Before removing or changing any exported
identifier, grep every module in `~/repos` -- including `~/repos/_attic`
-- for its users; "unused in this module" is not "dead". The
cautionary case: `store.LogStore.Rewrite` was deleted on 2026-07-21 as
dead code while quietcasting-go's qcpub depended on it, and that tool
silently stopped building for a month. Conversely, nothing in kv is a
general primitive: vault types, client, server and wire are ASCON-KV
protocol surface and stay here; primitives (ascon, golay, lz4s) are
their own top-level directories (`durable` among them). `store` included: see DESIGN.t, "The
store is protocol, not a primitive".

## Testing

From the repo root, with the rest of the module: `go test ./kv/...`.
kv.js's tests drive headless Chrome and skip without it.

The crash-safety chaos test (workers SIGKILLed mid-append) is opt-in
and skipped by a normal `go test`:

```
KV_CHAOS=1 go test ./kv/store -run Chaos -v
```
