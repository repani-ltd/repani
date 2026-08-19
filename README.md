# repani

The public Repani Limited Go module, `repani.com`. Each top-level
directory is one project; its import path is `repani.com/<dir>/...`.

- `pica/`  -- the pica typesetting language and `pica` CLI
             (`go install repani.com/pica/cmd/pica@latest`)
- `fact/`  -- the FACT format, Go projections and `fact` CLI
             (`go install repani.com/fact/cmd/fact@latest`)
- `ascon/` -- Ascon-XOF128 and Ascon-AEAD128 (NIST SP 800-232)
- `golay/` -- extended Golay(24,12): corrects 3, detects 4
- `lz4s/`  -- LZ4 sequence format re-tuned for small texts

The last three are primitive packages: stdlib-only, no sibling
imports, no protocol knowledge, append-only (see CLAUDE.md).

Build and test everything: `go build ./... && go test ./...`.

## Releases

Prebuilt `pica` and `fact` binaries for linux, macOS and Windows
(amd64, arm64), with `checksums.txt`, are published on every version
tag: https://github.com/repani-ltd/repani/releases (latest: v0.2.1).
Built by GoReleaser in CI (`.goreleaser.yaml`,
`.github/workflows/release.yaml`); it is not a dependency of any
package here. `go install ...@latest` remains the source route.

## Licence

Apache License 2.0 (LICENSE); third-party notices in NOTICE.
