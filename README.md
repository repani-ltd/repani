# repani

The public Repani Limited Go module, `repani.com`. Each top-level
directory is one project; its import path is `repani.com/<dir>/...`.

- `prim/`  -- shared primitives: `ascon`, `golay`, `lz4s`
- `pica/`  -- the pica typesetting language and `pica` CLI
             (`go install repani.com/pica/cmd/pica@latest`)
- `fact/`  -- the FACT format, Go projections and `fact` CLI
             (`go install repani.com/fact/cmd/fact@latest`)

Build and test everything: `go build ./... && go test ./...`.
