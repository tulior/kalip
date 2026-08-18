# kalip

A Go-based agent harness with a suckless-style architecture.

The v3.1 contract is the public, frozen interface. It exposes
three tools to the model: `sh`, `read_ref`, `splice`. The harness
owns all mutation: the model only describes what to change, and
the harness validates the description before any file is
rewritten.

## What kalip is

- A small Go binary that drives a model through a tool loop.
- The contract is the source of truth. See
  [`docs/CONTRACT.md`](docs/CONTRACT.md) for the wire format and
  [`docs/SOURCE_OF_TRUTH.md`](docs/SOURCE_OF_TRUTH.md) for the
  design rationale.
- 78 contract tests. Run them:

```
go test ./...
```

## Three tools

- `sh` — one Bash call, one process. cwd, variables, and shell
  options do not persist across calls.
- `read_ref` — read a file with snapshot-bound line anchors.
  Returns `@ref R<n>` (snapshot) and `Lnn:hh` (line anchor).
- `splice` — atomic content-anchored edit. The harness validates
  all edits against the snapshot before writing. A successful
  splice returns a bounded local view of the committed file.

## Three arms

- `sh_only` — just `sh`.
- `read_ref_splice` (B_current) — `sh`, `read_ref`, `splice`.
  The model does not see the post-state.
- `read_ref_splice_observe` (B_fixed) — same tools, but
  splice returns the post-state.

## Build

```
go build ./cmd/kalip
```

## License

Apache-2.0. See `LICENSE`.
