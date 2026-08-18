# KALIP Contract

The current frozen contract is the v3.1 surface: `{sh, read_ref, splice}`.

Each tool has a distinct role:

```
read_ref = addressable observation
splice   = structure-preserving mutation + truthful local post-state
sh       = general computation and semantic verification
```

---

## read_ref

**Read source with snapshot-bound line anchors.**

The `@ref R<n>` value identifies the observed file version. Copy it exactly into `splice.ref`.

Each source line begins with an anchor such as `L38:f1|`. Copy the complete `L38:f1` token exactly into `splice.at`, `start`, `end`, or `after`.

Anchors are supplied by the harness; do not construct or modify them. Only lines shown by this `read_ref` call can be addressed using its anchors. If the file changes or you need anchors for other lines, call `read_ref` again.

**Schema** (informal):

```json
{
  "path":       "string (workspace-relative)",
  "start_line": "integer (1-based, default 1)",
  "line_count": "integer (default 2000)"
}
```

**Response shape**:

```
@ref R7
@file <workspace-relative path>

L1:ab|<line content>
L2:cd|<line content>
...
```

---

## splice

**Apply atomic edits using anchors copied from `read_ref`.**

The schema uses `oneOf` to enforce three mutually exclusive edit shapes structurally. No `op` or `kind` field.

For a small replacement within one observed line, use `at + old + new`. `old` must occur exactly once in that anchored line. Surrounding bytes, indentation, comments, and the line boundary are preserved.

For complete-line or multiline replacement, use `start + end + text`. The addressed lines are replaced as complete logical lines and the harness preserves the boundary to untouched following source.

For insertion, use `after + text`.

All edits use the same snapshot and are validated before mutation. Successful edits return bounded local bytes read from the committed file. This post-state shows what was written; it does not establish semantic correctness.

**Schema**:

```json
{
  "ref":   "string (snapshot ID, e.g. R1)",
  "edits": [{
    "oneOf": [
      {"required": ["at", "old", "new"],
       "properties": {"at": "string", "old": "string", "new": "string"},
       "additionalProperties": false},
      {"required": ["start", "end", "text"],
       "properties": {"start": "string", "end": "string", "text": "string"},
       "additionalProperties": false},
      {"required": ["after", "text"],
       "properties": {"after": "string", "text": "string"},
       "additionalProperties": false}
    ]
  }]
}
```

**Response on success**:

```
ok

post-edit <workspace-relative path> lines <X>-<Y>:
<actual source bytes for those lines, verbatim>
```

**Truth about resulting bytes ≠ proof of correctness.**

The model still has to verify with `pytest` or equivalent. The harness does not run the test suite.

---

## sh

**Execute a Bash command in the session workspace.**

Each `sh` call starts a new shell in the workspace working directory. Shell-local state such as `cd`, variables, and shell options does not persist to later `sh` calls unless encoded again in the command.

The command is interpreted by Bash. Nonzero exit status indicates command failure; output may contain diagnostics.

**Schema**:

```json
{"cmd": "string (Bash command)"}
```

`sh` is not classified as inspection / verification / mutation in its semantics. Those are analytical categories for trajectory analysis, not shell semantics. `sed`, `pytest`, and `cat` after splice are overwhelmingly legitimate verification actions and should not be discouraged.

---

## Priority

```
splice > read_ref > sh
```

because most remaining ambiguity is in how edits are expressed.

---

## What the harness is and is not

The harness **is authoritative** about:

```
these are the bytes now present here
```

The harness is **not authoritative** about:

```
these bytes solve the user's problem
```

So the desired trajectory is not necessarily:

```
splice → post-state → final
```

It is often correctly:

```
splice
→ bounded post-state
→ pytest (via sh)
→ final
```

What post-state eliminates is the unnecessary middle loop:

```
splice → read_ref → sed → reconsider what was written → pytest
```

not legitimate semantic verification.
