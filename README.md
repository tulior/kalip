kalip
=====

a small harness for coding models

three tools: read_ref, splice, sh

## What it is

A Go binary that drives a model through a tool loop. The model
describes a mutation; the harness validates the description against
the observed file before any write. The post-edit observation is
the bytes actually on disk, not a receipt.

```
read_ref = addressable observation
splice   = structure-preserving mutation + truthful local post-state
sh       = general computation and semantic verification
```

The wire format is the source of truth. See
[`docs/CONTRACT.md`](docs/CONTRACT.md). The architectural rationale
lives in [`docs/DESIGN.md`](docs/DESIGN.md). The test breakdown is
in [`docs/TESTS.md`](docs/TESTS.md).

## Install

```
go install github.com/tulior/kalip@latest
```

## Use

```
kalip <task-prompt>
```

## Tests

78 contract tests. Deterministic, no API required.

```
go test ./...
```

## Build from source

```
go build ./cmd/kalip
```

## License

Apache-2.0. See [`LICENSE`](LICENSE).
