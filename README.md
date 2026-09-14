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
- `typeset/` -- the typesetting family, one package per member:
             `typeset/tab`, tab stops (fixed columns on a monospace
             grid, cells aligned left, right, centred or on the
             decimal point); `typeset/format`, values as the house writes them
             (a number rounded or to places, a string cut or padded
             in runes, a time as HH:MM, a date as "Mon 02", a
             duration in its largest unit)
             `typeset/raster`, rows of colored text cells, forty
             wide: cell table, an ink per cell, a row record of up
             to 82 bytes as the unit of storage and update, an
             authoring language with aliases, links as bracketed
             spans, text/ANSI/HTML renderers, a JavaScript reader
             and the `raster` CLI (`typeset/raster/cmd/raster`)
- `trudge/` -- trudge1, a simple memory-hard KDF on Ascon-XOF128
             (256 MiB pool, 2^24-step walk; spec in `trudge/SPEC.t`)

ascon, golay, lz4s and every member of typeset are primitive packages: stdlib and other
primitives only, no protocol knowledge, append-only (see CLAUDE.md).

Build and test everything: `go build ./... && go test ./...`.

## Releases

Prebuilt `pica` and `fact` binaries for linux, macOS and Windows
(amd64, arm64), with `checksums.txt`, are published on every version
tag: https://github.com/repani-ltd/repani/releases.
Built by GoReleaser in CI (`.goreleaser.yaml`,
`.github/workflows/release.yaml`); it is not a dependency of any
package here. `go install ...@latest` remains the source route.

## Licence

Apache License 2.0 (LICENSE); third-party notices in NOTICE.
