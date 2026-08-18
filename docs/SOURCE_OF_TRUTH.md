# Source of truth (current state of the world)

The KALIP repository at this point contains:

- `README.md` — the KALIP definition, principles, core interface.
- `LICENSE` — MIT.
- `docs/CONTRACT.md` — the v3.1 contract specification.
- `docs/TESTS.md` — the test suite breakdown.
- `docs/SOURCE_OF_TRUTH.md` — this file.

The Go source code for the harness is **not yet committed to this
repository.** The full source exists as a frozen binary at
`/workspace/toolprobe_gate1/harness_splice_fixed_v3.frozen`
(SHA-256 `491cf877e062fa3583ba15123e712f3362a7b1ce3d207308b5dfbc97f27e310c`),
but the source tree was lost during a sandbox restructure.

The source is being reconstructed from the contract test output
and the saved tool descriptions. See `docs/RECONSTRUCTION.md`
for the current reconstruction plan.

## Reconstructed artifacts

These were extracted from the frozen binary or saved during the
v3.1 contract work:

- `tool_descriptions_v3.json` — the four frozen tool descriptions
  (`sh`, `read_ref_B`, `splice_B_fixed`, `splice_B_current`).
- The full `splice` schema with `oneOf` for three edit shapes.

These are sufficient to verify the contract is honored. They
are not sufficient to rebuild the binary from scratch; for
that, the full Go source tree needs to be restored.
