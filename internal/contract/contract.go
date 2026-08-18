// Package contract defines the v3.1 tool surface and the wire types
// the model exchanges with the harness. The contract is the source
// of truth for what tools exist, their names, descriptions, and
// JSON schemas. It is intentionally denotational: a tool description
// says what the tool does, not when to use it.
package contract

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed splice_schema.json
var spliceSchemaJSON []byte

//go:embed tool_descriptions_v3.json
var descriptionsJSON []byte

// Tool names exposed to the model. These are the only tool names
// the harness will ever serve. Any other name is INFRA_FAIL.
const (
	ToolSH        = "sh"
	ToolReadRef   = "read_ref"
	ToolSplice    = "splice"
	ToolSpliceB1  = "splice_b1"        // pre-v3.1 alias kept for legacy sessions
	ToolReadRefB1 = "read_ref_b1"      // pre-v3.1 alias kept for legacy sessions
)

// Arm names used to select surface variants via KALIP_ARM.
// v3.1 ships with three production arms: sh_only, read_ref_splice,
// and read_ref_splice_observe. v3.1 also ships one diagnostic
// arm: framed_splice (kept for the patch_format tests but not
// exposed to the model in production).
const (
	ArmShOnly         = "sh_only"
	ArmReadRefSplice  = "read_ref_splice"
	ArmReadRefSpliceObserve = "read_ref_splice_observe" // B_fixed
	ArmFramedSplice   = "framed_splice"
	ArmEditExact      = "edit_exact"
	ArmSpliceA1       = "splice_a1"
)

// Descriptions returns the per-tool descriptions as loaded from
// the embedded JSON. The descriptions are the denotational contract
// the model sees. The harness never edits them at runtime.
type Descriptions struct {
	ReadRef         string `json:"read_ref_B_description"`
	Sh              string `json:"sh_description"`
	SpliceBCurrent  string `json:"splice_B_current_description"`
	SpliceBFixed    string `json:"splice_B_fixed_description"`
}

func LoadDescriptions() (Descriptions, error) {
	var d Descriptions
	if err := json.Unmarshal(descriptionsJSON, &d); err != nil {
		return d, fmt.Errorf("parse tool descriptions: %w", err)
	}
	return d, nil
}

// SpliceSchema returns the JSON Schema for the splice tool with the
// oneOf enforcement on the three edit shapes: at+old+new, start+end+text,
// after+text. The model cannot produce a mixed-shape edit; the
// harness refuses it with INVALID_EDIT_SHAPE before any validation.
func SpliceSchema() (json.RawMessage, error) {
	out := make(json.RawMessage, len(spliceSchemaJSON))
	copy(out, spliceSchemaJSON)
	return out, nil
}

// ToolCall is the model-emitted request to invoke a tool.
type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	CallID    string          `json:"call_id,omitempty"`
}

// ToolResult is the harness-emitted response to a model tool call.
type ToolResult struct {
	CallID string          `json:"call_id,omitempty"`
	Name   string          `json:"name"`
	OK     bool            `json:"ok"`
	Output string          `json:"output,omitempty"`
	Error  *ToolError      `json:"error,omitempty"`
	Meta   json.RawMessage `json:"meta,omitempty"`
}

// ToolError is the structured failure shape. B errors (typed
// failure codes) are surfaced through this. The codes are part
// of the contract: the model is allowed to switch behavior based
// on them.
type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// B error codes. These are stable and part of the v3.1 contract.
// Adding a new code is a contract change.
const (
	ErrInvalidSnapshotRef    = "invalid_snapshot_ref"
	ErrUnknownSnapshot       = "unknown_snapshot"
	ErrStaleSnapshot         = "stale_snapshot"
	ErrInvalidLineRef        = "invalid_line_ref"
	ErrUnknownLineRef        = "unknown_line_ref"
	ErrHashMismatch          = "hash_mismatch"
	ErrOverlap               = "overlap"
	ErrInvalidEditShape      = "invalid_editShape"
	ErrOldNotFoundInAnchor   = "old_not_found_in_anchor"
	ErrOldMultipleMatches    = "old_multiple_matches_in_anchor"
	ErrEmptyOld              = "empty_old"
	ErrPathRequired          = "path_required"
	ErrPathEscapesWorkspace  = "path_escapes_workspace"
	ErrNotARegularFile       = "not_a_regular_file"
	ErrFileTooLarge          = "file_too_large"
	ErrNotUTF8               = "not_utf8"
	ErrEmptyEdits            = "empty_edits"
	ErrNoEditsApplied        = "no_edits_applied" // any failure that rolled back
	ErrSplceWriteFailed      = "splice_write_failed"
	ErrUnknownArm            = "unknown_arm"
	ErrUnknownProtocol       = "unknown_protocol"
	ErrInternal              = "internal_error"
)

// Dump returns the contract artifacts as raw JSON. Used by the
// docs tooling; not on the hot path.
func Dump() (schema, descriptions string, err error) {
	// Touch spliceSchemaJSON so go vet does not flag the embed as unused.
	if len(spliceSchemaJSON) == 0 {
		return "", "", fmt.Errorf("splice schema missing from embed")
	}
	return string(spliceSchemaJSON), string(descriptionsJSON), nil
}
