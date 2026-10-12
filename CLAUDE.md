# kv

ASCON-KV: append-only key-value vault store with client, server, and wire
protocol. See PROTOCOL.txt and CLIENT.txt for the protocol and client specs.

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
included (assessed 2026-08-19): its item identity (`Entry.ID`, a
domain-separated XOF), order (canonical `(TsMs, EntryID)`), set hash
and on-disk record are all PROTOCOL.txt definitions, so it is the
protocol's entry set with durability, not a generic log. Extracting a
generic `logset` primitive only makes sense when a second protocol
needs one -- two consumers shape the interface, one does not.

## Testing

The crash-safety chaos test (workers SIGKILLed mid-append) is opt-in
and skipped by a normal `go test`:

```
KV_CHAOS=1 go test ./store -run Chaos -v
```

