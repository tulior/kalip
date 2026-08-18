// Command kalip is the KALIP harness entry point.
//
// KALIP is a minimal, rigorous interface between capable models
// and the work they act on. The harness exposes three tools:
//
//	read_ref = addressable observation
//	splice   = structure-preserving mutation + truthful local post-state
//	sh       = general computation and semantic verification
//
// The harness is authoritative about "these are the bytes now
// present here" and NOT authoritative about "these bytes solve
// the user's problem". The model is responsible for verification.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/tulior/kalip/internal/contract"
	"github.com/tulior/kalip/internal/harness"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("kalip: %v", err)
	}
}

func run() error {
	cfg := harness.DefaultConfig()

	if v := os.Getenv("KALIP_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("KALIP_ARM"); v != "" {
		cfg.Arm = v
	}
	if v := os.Getenv("KALIP_REASONING"); v != "" {
		cfg.Reasoning = v
	}
	if v := os.Getenv("KALIP_WORKDIR"); v != "" {
		cfg.WorkDir = v
	}

	mgr, err := harness.NewManager(cfg)
	if err != nil {
		return fmt.Errorf("new manager: %w", err)
	}
	defer mgr.Close()

	// The harness talks to one model API and runs one task
	// per session. We accept a single task via argv or stdin.
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: kalip <task-prompt>")
	}
	task := os.Args[1]

	ctx := context.Background()
	sess, err := mgr.CreateSession(cfg.WorkDir)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	if err := sess.RunTask(ctx, task); err != nil {
		return fmt.Errorf("run task: %w", err)
	}
	return nil
}

// ContractDumps returns the contract JSON for inspection.
// Exposed for tooling; not part of the runtime hot path.
func ContractDumps() (schemaJSON, descriptionsJSON string, err error) {
	return contract.Dump()
}

// ToolCall is the wire shape for one tool invocation from the model.
type ToolCall = contract.ToolCall

// ToolResult is the wire shape for one tool result to the model.
type ToolResult = contract.ToolResult

// JSONSchema is a placeholder type used by tooling that introspects
// the tool schema. Kept in main to avoid an import cycle.
type JSONSchema = json.RawMessage
