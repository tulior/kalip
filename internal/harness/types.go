// Package harness implements the KALIP tool runtime. The
// runtime serves three tools (sh, read_ref, splice) against
// one model API and stores per-session history and provenance
// in a SQLite database.
//
// The package is split across several files:
//
//   types.go         shared types: request/response, snapshots,
//                    ledger, errors
//   b1.go            the B1 service: read_ref and splice with
//                    content-anchored local substitution
//   post_state.go    the bounded post-state renderer for splice
//   run_ir.go        the sh tool backed by an IR-style runner
//   tools.go         tool dispatch and arm selection
//   session.go       session lifecycle and storage
//   store.go         SQLite-backed storage of history and
//                    provenance
//   main.go          Model API client (sits in package main; see
//                    cmd/kalip)
//
// The b1 package is the only mutation surface. It owns:
//   - parse the splice request and validate edit shapes
//   - resolve the snapshot by ref
//   - resolve each line token Lnn:hh against the snapshot
//   - validate edits (no overlap, anchor present, old is unique)
//   - apply edits in original-order
//   - atomically rewrite the file
//   - build the bounded post-state observation
//
// Failure codes (B errors) are typed and stable.
package harness

// DefaultMaxReadLines is the per-call line cap on read_ref. It is
// the same default as the v2-era splice_b package; the v3.1 contract
// has not changed this number.
const DefaultMaxReadLines = 2000

// DefaultMaxFileBytes is the per-file byte cap. 16 MiB is the
// historical bound; the v3.1 contract has not changed it.
const DefaultMaxFileBytes = 16 << 20

// PostStateByteCap is the v3.1 cap on the post-state observation
// returned to the model after a successful splice. 4096 bytes,
// matching the v3 frozen contract.
const PostStateByteCap = 4096

// b1LineRefRE matches the Lnn:hh anchor format that read_ref
// emits and that splice consumes. hh is exactly two lowercase
// hex digits; nn is a 1-based line number with no leading zeros.
var b1LineRefRE = regexpMustCompile(`^L([1-9][0-9]*):([0-9a-f]{2})$`)

// b1SnapshotRefRE matches the @ref R<n> identifier that
// read_ref emits and splice consumes.
var b1SnapshotRefRE = regexpMustCompile(`^R([1-9][0-9]*)$`)
