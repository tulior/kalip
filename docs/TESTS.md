# Test Suite

The contract is validated by 78 deterministic unit tests, all
executable without an API call.

## Count by freeze

| freeze | pass | fail | notes |
|---|---:|---:|---|
| v2 | 63 | 0 | pre-inspection |
| v3 (initial) | 68 | 0 | 5 contract tests + 1 helper |
| v3 + inspection | 76 | 0 | +7 insertion/deletion tests |
| v3.1 (current) | 78 | 0 | +2 schema enforcement tests |

## Categories

- `arm_contract_test.go` — 5 tests: arm dispatching and observation
  grammar coherence across the A, B, C, D, E arm families.
- `b1_local_substitution_test.go` — 15 tests: the `{at, old, new}`
  local substitution shape, line boundary preservation, post-edit
  observation cap, plus 2 schema enforcement tests for the `oneOf`
  constraint on splice edit shapes.
- `b1_poststate_test.go` — 17 tests: post-state rendering for
  substitution, first-line / last-line / single-line boundaries,
  non-contiguous changes, the 10k_single_local regression shape,
  workspace-relative path rendering, and 7 tests for insertion /
  deletion including BOF / middle / EOF and multi-line insertion.
- `framed_splice_test.go` — 12 tests: the framed splice format
  (preserved for reference; not part of the current surface).
- `patch_format_test.go` — 8 tests: V4A patch format enforcement.
- `run_ir_test.go` — 15 tests: `sh` execution IR (argv, pipes,
  redirects, max depth, max nodes, cwd, env).

## Running

```bash
cd /workspace/kalip
go test ./...
```

Expected output: `ok ... 0.080s` with 78 PASS / 0 FAIL.
