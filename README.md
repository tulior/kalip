kalip
=====

a small harness for coding models

three tools: read_ref, splice, sh

## Naming

*kalıp* is Turkish for "mold" or "form." A mold is not a tool that
does work — it is a constraint on what work can produce. The harness
does not write code; it constrains the shape of model output before
the output reaches the file.

The name was kept through several naming rounds. The closest runner-up
was `weld`, which was rejected because the name is already taken by
the `weld-cli` PyPI package [1] and the `weld-project/weld` Go runtime
[2]. `kalip` survives because the *kalıp / form* metaphor is the
most precise description of what the harness actually does.

## Architecture

The canonical propositions KALIP is built on. Three categories:

- **[INV]** constitutional invariant — something the harness should
  preserve by construction.
- **[OBS]** experimentally earned conclusion — supported by the runs.
- **[DER]** derived proposition — architecture justified by the
  invariants + evidence, but not itself a universal empirical law.

### 1. Capability and interface

> **[OBS] Semantic reducibility ≠ behavioral redundancy.**

A capability being expressible through another primitive does not
mean the model behaves equivalently when forced to express it that
way. `sh` was semantically emulable with process execution, yet
materially changed trajectories.

> **[OBS] P(T | S) ≠ P(T).**

Tool adoption is relational. Whether a model uses a tool depends on
the rest of the surface presented alongside it. There is no
meaningful context-free question "Does the model like tool T?" —
only "Does the model use T under surface S?"

> **[DER] tool surface = capability surface + planning representation surface.**

A tool is not merely another capability. It is another representation
the model can choose while planning. Adding a semantically redundant
tool can still change behavior.

> **[OBS] representation preservation ⇏ representation coexistence.**

Two individually useful representations do not necessarily belong on
the same tool surface. Offering both can impose choice cost.

> **[OBS] more available tools can create interference without adding capability.**

The broad surfaces produced more calls and more choice behavior even
when the extra tools were not necessary. Minimality is not aesthetic
minimalism. It is a hypothesis about reducing planning entropy.

### 2. Division of labor between model and harness

> **[INV] Do not ask the model to state or derive information the harness can determine exactly.**

If the harness knows the path, changed bytes, exit status, snapshot
identity, or exact anchor, it should provide that fact. Model
cognition should not be spent reconstructing machine-known state.

> **[INV] Recognize exact structure; do not guess intent.**

The harness may validate that a token exists exactly once. It should
not decide that some nearby token is "probably what the model meant."
This is the basis of fail-closed editing.

> **[INV] syntax knowledge is not semantic knowledge.**

Understanding that a command contains a pipe, redirect, filename, or
shell construct does not mean the harness knows the command's effects
or intent. That killed the command-parser / journal architecture.

> **[INV] model authors semantic delta; harness preserves irrelevant surrounding bytes.**

The model should specify what needs to change. The harness should
preserve everything outside that change that it can preserve
mechanically. This is the central reason anchored local substitution
exists.

> **[DER] mutation payload should approach the size of the semantic delta.**

For `return x * 2` becoming `return x * 1`, the model should
preferably author `old = "2", new = "1"` — or another sufficiently
unique local substitution — rather than reconstruct indentation,
comments, neighboring source, and line boundaries unnecessarily.

### 3. Execution

> **[INV] observation must not change execution semantics.**

Capturing output is an observation concern. It must not change which
processes remain alive, when the command completes, or what file
descriptors descendants inherit in a way that changes execution
behavior. This was directly earned by the pipe-capture failure.

> **[INV] execution completion ⊥ observation transport.**

Whether the command is finished must not depend on whether
descendants still hold an observation pipe open. That is why KALIP
moved to bounded tempfile-backed observation.

> **[INV] bytes command creates ≠ bytes harness understands ≠ bytes model sees.**

Those are three distinct layers. A process may emit arbitrary output.
The harness should understand as little of it as necessary. The model
should receive only the bounded observation required for its next
decision.

> **[INV] Bound harness work by decision relevance, not execution magnitude.**

A command may produce gigabytes. That does not mean the harness needs
to ingest gigabytes to tell the model what happened.

> **[INV] Do not compress irrelevant facts. Eliminate them before representation.**

The optimal representation of irrelevant information is generally not
a better summary. It is absence.

> **[DER] O = minimum truthful information sufficient for the next decision.**

Observation should be truthful and bounded around what the agent
needs to decide next. Not a transcript of everything the machine did.

### 4. Interface boundaries

> **[DER] OS-facing interface boundaries need not be model-facing interface boundaries.**

The operating system has processes, pipes, file descriptors, shell
parsing, syscalls, filesystems, signals, and more. The model does
not need one tool for every OS abstraction. Those mechanisms can
live beneath a much smaller model-facing algebra.

> **[INV] A tool contract is the complete coherent loop.**

```
description ≅ observation grammar ≅ input schema ≅ handler semantics
```

Not literal identity — but end-to-end semantic coherence. The
miswired Gate 1 arms proved that testing a schema while another
handler or observation grammar is actually active produces meaningless
behavioral conclusions.

> **[OBS] Anything lexical in model-visible output can become an affordance.**

The accidental `[ref=obsN]` telemetry was not "just telemetry." The
model started using `obs1` as a reference. Therefore model-visible
output is part of the interface whether intended or not.

### 5. Reading and addressing source

> **[DER] Address structure by supplied identity, not reconstructed content.**

The model should copy an anchor supplied by the harness rather than
reproduce arbitrary source text as an identifier.

> **[OBS] inline snapshot-bound anchors can be highly copyable under a coherent contract.**

Once the B arm was actually wired correctly, `L<n>:<tag>` lexical
copying was around 98%. That moved the primary problem away from
anchor transcription and toward mutation semantics.

> **[INV] snapshot identity ≠ line identity.**

`@ref R7` says which observed version of the file is being addressed.
`L38:f1` identifies a line within that observation. Those are
distinct pieces of state and should remain distinct.

### 6. Mutation

> **[INV] False preconditions cause failure, not relocation.**

No fuzzy search. No "closest line." No inferred indentation repair.
No silently finding another matching region.

> **[INV] Validate the complete mutation before committing any of it.**

A batched splice either satisfies its preconditions or it does not.
Partial semantic success is worse than explicit failure.

> **[OBS / DER] For a local change, anchored substitution is preferable to line reconstruction when applicable.**

This is one of the strongest editing results from the trajectory
work. `at + old + new` allows the harness to retain indentation,
comments, unrelated text, line terminator, and neighboring lines.
The model authors the change rather than reconstructing its
container.

### 7. Post-edit state

> **[INV] successful mutation should return truthful evidence of resulting local state.**

Not a receipt describing what the harness intended to write. Not an
echo of the request. Actual source bytes read from the committed
file.

> **[INV] post-state is authoritative about bytes, not correctness.**

```
"these bytes now exist" ≠ "these bytes solve the task"
```

KALIP can establish the former. Only appropriate semantic
verification can establish the latter.

> **[INV] MutationSuccess ⊥ ObservationSuccess.**

If the edit committed but rendering the local post-state fails, the
mutation does not retroactively become unsuccessful. The response
must state both facts truthfully.

> **[DER] post-edit observation should close local state, not create a new editing namespace.**

Therefore splice post-state does not emit fresh editable line
references. If the model needs another edit, it obtains a fresh
`read_ref` snapshot.

### 8. Verification

> **[OBS] post-state reduces state reacquisition, not the need for semantic verification.**

The corrected v2 data is important here. `B_fixed` reduced immediate
`read_ref` after splice from roughly 61% to 20%. But the model often
went directly to `sed`, Python introspection, or — most importantly
— `pytest`. That is not failure of post-state. It is the intended
separation of responsibilities.

> **[OBS] shell activity after an edit ≠ repair activity.**

The original README interpretation was wrong. Across the 16
sessions:

```
cat > file                0
open(...).write(...)      0
sed -i                    1
```

Inspection and verification dominated. Therefore "used `sh` after
splice" is not a useful failure metric.

> **[DER] state reacquisition cost ≠ verification cost.**

These must be measured separately. A redundant reread of the exact
bytes splice just returned is potentially removable overhead. Running
the relevant tests is often useful work.

> **[DER] a clean successful trajectory may end with semantic verification.**

```
read_ref
→ splice
→ truthful post-state
→ pytest
→ final
```

That is not a trajectory KALIP should optimize away.

### 9. The three-tool decomposition

This is probably the most compact architectural conclusion from the
whole program.

> **[DER] read_ref = addressable observation.**

> **[DER] splice = structure-preserving mutation + truthful local post-state.**

> **[DER] sh = general computation and semantic verification.**

Together, `{read_ref, splice, sh}` is not "three convenient tools."
It is a decomposition of three fundamentally different
responsibilities:

```
observe → change → compute / verify
```

without multiplying specialized abstractions.

### 10. Experimental laws that shaped the harness

These are not product semantics. They are part of why the
architecture deserves trust.

> **[INV] A behavioral result is inadmissible if the tested contract was not actually the contract shown to the model.**

The original B / B1 wiring failure permanently earned this rule.

> **[OBS] aggregate metrics cannot overrule contradictory raw trajectories.**

When the report said failures were sh-only but the transcripts
showed successful splice calls followed by failure, the report was
wrong. Raw trajectory evidence outranked the derived narrative.

> **[INV] derived classifiers must remain traceable to raw evidence.**

Every label — `repair`, `reacquisition`, `verification`, `malformed
edit` — should be recoverable from preceding observation, raw tool
arguments, raw response, and subsequent action.

> **[INV] Deterministic properties should be tested deterministically.**

If the question is "does insertion at EOF return the correct
committed post-state range?" you do not spend model inference money
answering it. You write a contract test. [3]

> **[INV] experiment isolation requires substrate isolation.**

Prompt isolation is not enough if one model session can find `/` and
discover another experiment's workspace. The environment itself is
part of experimental validity.

### 11. The deepest proposition

If the whole research program were reduced to one derived
proposition, it would be:

> **[DER] Intelligence is upstream. Interface quality determines realized capability.**

And the engineering version is:

> **[DER] remove mechanically avoidable uncertainty before asking the model to reason.**

That explains almost every successful change:

- remove command-effect parsing;
- isolate execution from observation;
- bound observations;
- eliminate overlapping tools;
- supply exact anchors;
- preserve surrounding bytes;
- fail closed rather than guess;
- return committed post-state;
- leave semantic verification to general computation.

And perhaps the most KALIP-specific formulation is:

> **[DER] The harness should constrain mechanics without constraining capability.**

That, to me, is the architectural thesis the experiments converged
on.

## Build

```
go build ./cmd/kalip
```

## Tests

```
go test ./...
```

78 contract tests. The contract is the source of truth; the tests
are the oracle.

## License

Apache-2.0. See `LICENSE`.

---

[1]: https://pypi.org/project/weld-cli/ "weld-cli · PyPI"
[2]: https://github.com/weld-project/weld "weld-project/weld"
[3]: https://go.dev/doc/modules/developing "Developing and publishing modules — The Go Programming Language"
