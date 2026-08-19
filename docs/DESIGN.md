# Design

The architecture in its own words — the rationale for why the
harness is the way it is. The wire contract lives in
[`CONTRACT.md`](CONTRACT.md); this is the "why", not the "what".

## 1. Capability and interface

**Semantic reducibility ≠ behavioral redundancy.**

A capability being expressible through another primitive does not
mean the model behaves equivalently when forced to express it that
way. `sh` was semantically emulable with process execution, yet
materially changed trajectories.

**P(T | S) ≠ P(T).**

Tool adoption is relational. Whether a model uses a tool depends
on the rest of the surface presented alongside it. There is no
meaningful context-free question "Does the model like tool T?" —
only "Does the model use T under surface S?"

**tool surface = capability surface + planning representation surface.**

A tool is not merely another capability. It is another
representation the model can choose while planning. Adding a
semantically redundant tool can still change behavior.

**representation preservation ≠ representation coexistence.**

Two individually useful representations do not necessarily belong
on the same tool surface. Offering both can impose choice cost.

**More available tools can create interference without adding capability.**

The broad surfaces produced more calls and more choice behavior
even when the extra tools were not necessary. Minimality is a
hypothesis about reducing planning entropy, not an aesthetic
preference.

## 2. Division of labor between model and harness

**Do not ask the model to state or derive information the harness can determine exactly.**

If the harness knows the path, changed bytes, exit status, snapshot
identity, or exact anchor, it should provide that fact. Model
cognition should not be spent reconstructing machine-known state.

**Recognize exact structure; do not guess intent.**

The harness may validate that a token exists exactly once. It should
not decide that some nearby token is "probably what the model
meant." This is the basis of fail-closed editing.

**Syntax knowledge is not semantic knowledge.**

Understanding that a command contains a pipe, redirect, filename,
or shell construct does not mean the harness knows the command's
effects or intent. That killed the command-parser / journal
architecture.

**Model authors semantic delta; harness preserves irrelevant surrounding bytes.**

The model specifies what needs to change. The harness preserves
everything outside that change that it can preserve mechanically.
This is the central reason anchored local substitution exists.

**Mutation payload should approach the size of the semantic delta.**

For `return x * 2` becoming `return x * 1`, the model should
preferably author `old = "2", new = "1"` — or another
sufficiently unique local substitution — rather than reconstruct
indentation, comments, neighboring source, and line boundaries
unnecessarily.

## 3. Execution

**Observation must not change execution semantics.**

Capturing output is an observation concern. It must not change
which processes remain alive, when the command completes, or what
file descriptors descendants inherit in a way that changes
execution behavior. This was directly earned by the pipe-capture
failure.

**Execution completion is independent of observation transport.**

Whether the command is finished must not depend on whether
descendants still hold an observation pipe open. That is why KALIP
uses bounded tempfile-backed observation.

**Bytes the command creates ≠ bytes the harness understands ≠ bytes the model sees.**

Three distinct layers. A process may emit arbitrary output. The
harness understands as little of it as necessary. The model receives
only the bounded observation required for its next decision.

**Bound harness work by decision relevance, not execution magnitude.**

A command may produce gigabytes. That does not mean the harness
needs to ingest gigabytes to tell the model what happened.

**Do not compress irrelevant facts. Eliminate them before representation.**

The optimal representation of irrelevant information is generally
not a better summary. It is absence.

**Observation is the minimum truthful information sufficient for the next decision.**

Observation is truthful and bounded around what the agent needs
to decide next. Not a transcript of everything the machine did.

## 4. Interface boundaries

**OS-facing interface boundaries need not be model-facing interface boundaries.**

The operating system has processes, pipes, file descriptors, shell
parsing, syscalls, filesystems, signals, and more. The model does
not need one tool for every OS abstraction. Those mechanisms can
live beneath a much smaller model-facing algebra.

**A tool contract is the complete coherent loop.**

```
description ≅ observation grammar ≅ input schema ≅ handler semantics
```

Not literal identity — but end-to-end semantic coherence. The
miswired Gate 1 arms proved that testing a schema while another
handler or observation grammar is actually active produces
meaningless behavioral conclusions.

**Anything lexical in model-visible output can become an affordance.**

The accidental `[ref=obsN]` telemetry was not "just telemetry."
The model started using `obs1` as a reference. Therefore
model-visible output is part of the interface whether intended or
not.

## 5. Reading and addressing source

**Address structure by supplied identity, not reconstructed content.**

The model copies an anchor supplied by the harness rather than
reproducing arbitrary source text as an identifier.

**Inline snapshot-bound anchors can be highly copyable under a coherent contract.**

Once the B arm was actually wired correctly, `L<n>:<tag>` lexical
copying was around 98%. That moved the primary problem away from
anchor transcription and toward mutation semantics.

**Snapshot identity is not line identity.**

`@ref R7` says which observed version of the file is being
addressed. `L38:f1` identifies a line within that observation.
Those are distinct pieces of state and should remain distinct.

## 6. Mutation

**False preconditions cause failure, not relocation.**

No fuzzy search. No "closest line." No inferred indentation
repair. No silently finding another matching region.

**Validate the complete mutation before committing any of it.**

A batched splice either satisfies its preconditions or it does
not. Partial semantic success is worse than explicit failure.

**For a local change, anchored substitution is preferable to line reconstruction when applicable.**

`at + old + new` allows the harness to retain indentation,
comments, unrelated text, line terminator, and neighboring lines.
The model authors the change rather than reconstructing its
container.

## 7. Post-edit state

**Successful mutation should return truthful evidence of resulting local state.**

Not a receipt describing what the harness intended to write. Not
an echo of the request. Actual source bytes read from the
committed file.

**Post-state is authoritative about bytes, not correctness.**

```
"these bytes now exist" ≠ "these bytes solve the task"
```

KALIP can establish the former. Only appropriate semantic
verification can establish the latter.

**Mutation success is independent of observation success.**

If the edit committed but rendering the local post-state fails,
the mutation does not retroactively become unsuccessful. The
response must state both facts truthfully.

**Post-edit observation should close local state, not create a new editing namespace.**

Splice post-state does not emit fresh editable line references.
If the model needs another edit, it obtains a fresh `read_ref`
snapshot.

## 8. Verification

**Post-state reduces state reacquisition, not the need for semantic verification.**

`B_fixed` reduced immediate `read_ref` after splice from roughly
61% to 20%. But the model often went directly to `sed`, Python
introspection, or — most importantly — `pytest`. That is the
intended separation of responsibilities.

**Shell activity after an edit is not repair activity.**

Across 16 sessions:

```
cat > file                0
open(...).write(...)      0
sed -i                    1
```

Inspection and verification dominated. "Used `sh` after splice"
is not a useful failure metric.

**State reacquisition cost is not verification cost.**

These must be measured separately. A redundant reread of the
exact bytes splice just returned is potentially removable
overhead. Running the relevant tests is often useful work.

**A clean successful trajectory may end with semantic verification.**

```
read_ref
→ splice
→ truthful post-state
→ pytest
→ final
```

That is not a trajectory KALIP should optimize away.

## 9. The three-tool decomposition

```
read_ref = addressable observation
splice   = structure-preserving mutation + truthful local post-state
sh       = general computation and semantic verification
```

A decomposition of three fundamentally different responsibilities:

```
observe → change → compute / verify
```

without multiplying specialized abstractions.

## 10. Experimental laws

These are part of why the architecture deserves trust.

**A behavioral result is inadmissible if the tested contract was not actually the contract shown to the model.**

The original B / B1 wiring failure permanently earned this rule.

**Aggregate metrics cannot overrule contradictory raw trajectories.**

When the report said failures were sh-only but the transcripts
showed successful splice calls followed by failure, the report was
wrong. Raw trajectory evidence outranked the derived narrative.

**Derived classifiers must remain traceable to raw evidence.**

Every label — `repair`, `reacquisition`, `verification`,
`malformed edit` — should be recoverable from preceding
observation, raw tool arguments, raw response, and subsequent
action.

**Deterministic properties should be tested deterministically.**

If the question is "does insertion at EOF return the correct
committed post-state range?" you do not spend model inference
money answering it. You write a contract test.

**Experiment isolation requires substrate isolation.**

Prompt isolation is not enough if one model session can find `/`
and discover another experiment's workspace. The environment
itself is part of experimental validity.

## 11. The deepest proposition

**Intelligence is upstream. Interface quality determines realized capability.**

The engineering version:

**Remove mechanically avoidable uncertainty before asking the model to reason.**

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

And the KALIP-specific formulation:

**The harness should constrain mechanics without constraining capability.**

That is the architectural thesis the experiments converged on.
