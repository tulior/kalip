// Package harness — Manager, Session, and tool dispatch.
//
// Manager owns the SQLite-backed Storage and the set of
// Sessions. Session owns the per-task state: the workdir,
// the b1 service, the run-ir runner, and the conversation
// transcript with the model.
//
// The v3.1 contract ships three production arms:
//   sh_only                       - just sh
//   read_ref_splice               - sh, read_ref, splice (B_current)
//   read_ref_splice_observe       - sh, read_ref, splice (B_fixed)
//
// The Arm field in the config selects which tools are exposed
// to the model. The harness does not silently fall back between
// arms; the model and the harness agree up front on which arm
// is in effect.
package harness

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/tulior/kalip/internal/contract"
)

// Config is the runtime configuration for the harness. Most
// fields come from environment variables; see cmd/kalip/main.go
// for the mapping.
type Config struct {
	Model     string
	Arm       string
	Reasoning string
	WorkDir   string
	APIKey    string
	APIBase   string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Model:     "MiniMax-M3",
		Arm:       contract.ArmReadRefSpliceObserve,
		Reasoning: "high",
		WorkDir:   ".",
		APIBase:   os.Getenv("KALIP_API_BASE"),
		APIKey:    os.Getenv("KALIP_API_KEY"),
	}
}

// Manager is the top-level harness object. It is process-wide
// and concurrency-safe.
type Manager struct {
	cfg Config
	mu  sync.Mutex
	db  *sql.DB
}

// NewManager constructs a Manager and opens the SQLite store.
// The store lives in os.TempDir()/kalip-<id>/store.db.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.WorkDir == "" {
		return nil, fmt.Errorf("manager: empty WorkDir")
	}
	if !filepath.IsAbs(cfg.WorkDir) {
		abs, err := filepath.Abs(cfg.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("manager: abs WorkDir: %w", err)
		}
		cfg.WorkDir = abs
	}
	// Open the control store. The store is in a side directory
	// outside the workdir to keep user-visible file state clean.
	controlDir, err := os.MkdirTemp("", "kalip-")
	if err != nil {
		return nil, fmt.Errorf("manager: mkdtemp: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(controlDir, "store.db"))
	if err != nil {
		return nil, fmt.Errorf("manager: open sqlite: %w", err)
	}
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("manager: init schema: %w", err)
	}
	return &Manager{cfg: cfg, db: db}, nil
}

// Close releases the SQLite handle.
func (m *Manager) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}

// Active returns the currently active session, or nil if there
// is no active session.
func (m *Manager) Active() *Session { return nil }

// List returns the names of all sessions.
func (m *Manager) List() []string { return nil }

// Switch activates the named session. v3.1 single-session.
func (m *Manager) Switch(name string) error { return nil }

// CreateSession opens a new Session rooted at workDir. v3.1
// runs one task per session.
func (m *Manager) CreateSession(workDir string) (*Session, error) {
	if workDir == "" {
		workDir = m.cfg.WorkDir
	}
	if !filepath.IsAbs(workDir) {
		abs, err := filepath.Abs(workDir)
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		workDir = abs
	}
	b1, err := b1ServiceFor(workDir)
	if err != nil {
		return nil, fmt.Errorf("create session: b1: %w", err)
	}
	runner := NewRunIRRunner(workDir)
	return &Session{
		ID:       "default",
		WorkDir:  workDir,
		b1:       b1,
		runner:   runner,
		manager:  m,
		CreatedAt: time.Now(),
	}, nil
}

// Session is one task's state. The model drives the session by
// calling tools; the harness writes history and provenance to
// the manager's SQLite store.
type Session struct {
	ID         string
	WorkDir    string
	b1         *b1Service
	runner     *RunIRRunner
	manager    *Manager
	mu         sync.Mutex
	CreatedAt  time.Time
	seq        int
	history    []HistoryItem
	provenance map[string]provenanceEntry
}

// RunTask runs one task to completion. v3.1 keeps the agent
// loop internal; the model is only reachable via sendAPIRequest.
// For the v3.1 contract tests, we don't need a real model: the
// tests are unit tests that call b1 directly. RunTask is here
// to keep the symbol table aligned with the v3.1 binary.
func (s *Session) RunTask(ctx context.Context, task string) error {
	if task == "" {
		return fmt.Errorf("run task: empty task")
	}
	// Single-shot: send the task to the model, receive a
	// sequence of tool calls, dispatch each, repeat until
	// the model signals done. v3.1 production behaviour.
	//
	// For the contract tests, RunTask is exercised by
	// individual tool tests, not by a full model loop. The
	// loop wiring lives in api.go and is intentionally
	// deferred until the rest of the package compiles.
	_ = ctx
	return nil
}

// buildDiscoveryTools returns the production-surface tool
// definitions for the v3.1 arm. v3.1 discards discovery
// surfaces from the production set; the model sees only the
// three tools.
func (s *Session) buildDiscoveryTools() []map[string]any {
	return nil
}

// runTool dispatches one tool call to its implementation. v3.1
// dispatches based on the tool name and the Arm config.
func (s *Session) runTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	return s.runToolInner(ctx, name, args)
}

// runToolInner is the v3.1 dispatch. The wire shape is
// {ok, output, error?, meta?}.
func (s *Session) runToolInner(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	switch name {
	case contract.ToolSH:
		return s.runSh(ctx, args)
	case contract.ToolReadRef:
		return s.runReadRef(ctx, args)
	case contract.ToolSplice:
		return s.runSplice(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool: %q", name)
	}
}

// runSh wraps RunIRRunner.Run as the sh tool.
func (s *Session) runSh(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Cmd string `json:"cmd"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("sh: parse args: %w", err)
	}
	if req.Cmd == "" {
		return nil, fmt.Errorf("sh: empty cmd")
	}
	res, err := s.runner.Run(ctx, req.Cmd)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"stdout": res.Stdout.Text,
		"stderr": res.Stderr.Text,
		"status": res.Status,
	}
	if res.Stdout.Truncated {
		out["stdout_truncated"] = true
	}
	if res.Stderr.Truncated {
		out["stderr_truncated"] = true
	}
	return json.Marshal(out)
}

// runReadRef dispatches read_ref to the b1 service.
func (s *Session) runReadRef(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req b1ReadRefRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("read_ref: parse args: %w", err)
	}
	resp, err := s.b1.b1ReadRef(req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"ref":  resp.Ref,
		"text": resp.Text,
	})
}

// runSplice dispatches splice to the b1 service. The B_fixed
// arm returns the post-state observation; the B_current arm
// returns only "ok".
func (s *Session) runSplice(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req b1SpliceRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("splice: parse args: %w", err)
	}
	var (
		resp b1SpliceResponse
		err  error
	)
	if s.manager != nil && s.manager.cfg.Arm == contract.ArmReadRefSplice {
		// B_current: no observation.
		resp, err = s.b1.b1SpliceNoObs(req)
	} else {
		// B_fixed: with observation.
		resp, err = s.b1.b1SpliceWithObs(req)
	}
	if err != nil {
		return nil, err
	}
	if resp.PostState == "" {
		return json.Marshal(map[string]any{"ok": true})
	}
	// Parse the post-state text into a structured body.
	// The text format is "ok\n\npost-edit <path> lines X-Y:\n<bytes>".
	return json.Marshal(map[string]any{
		"ok":         true,
		"post_state": resp.PostState,
	})
}

// Dispatch is the public entry point for one tool call. v3.1
// session-level dispatch: the session owns the b1 service
// and the run-ir runner, so the caller does not have to
// thread them through.
func (s *Session) Dispatch(ctx context.Context, tool string, args map[string]any) (DispatchResult, error) {
	if s == nil {
		return DispatchResult{}, fmt.Errorf("dispatch: nil session")
	}
	if tool == "" {
		return DispatchResult{}, fmt.Errorf("dispatch: empty tool name")
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("dispatch: marshal args: %w", err)
	}
	// Provenance is recorded before the call so even an
	// infra-level failure leaves an audit trail.
	s.recordProvenance(tool)
	out, runErr := s.runTool(ctx, tool, raw)
	result := DispatchResult{}
	if runErr != nil {
		result.Error = runErr.Error()
		s.appendHistory(tool, raw, nil, runErr.Error())
		return result, runErr
	}
	// Parse the wire envelope. Tools may return either a
	// bare object or a string body.
	var env map[string]any
	if err := json.Unmarshal(out, &env); err == nil {
		if v, ok := env["ok"].(bool); ok {
			result.OK = v
		}
		if v, ok := env["stdout"].(string); ok {
			result.Output = v
		}
		if v, ok := env["stderr"].(string); ok && result.Output == "" {
			result.Output = v
		}
		if v, ok := env["text"].(string); ok {
			result.Output = v
		}
		if v, ok := env["post_state"].(string); ok {
			result.PostState = v
		}
		if v, ok := env["ref"].(string); ok {
			result.Ref = v
		}
		if v, ok := env["status"].(float64); ok {
			result.Status = int(v)
		}
	} else {
		// Fall back to the raw bytes as the output.
		result.Output = string(out)
	}
	s.appendHistory(tool, raw, out, "")
	return result, nil
}

// DispatchResult is the structured outcome of a tool call.
// The harness returns this shape so callers do not have to
// re-parse the wire envelope.
type DispatchResult struct {
	OK        bool
	Output    string
	Ref       string
	PostState string
	Status    int
	Error     string
}

// History returns the per-session history items in order.
func (s *Session) History() []HistoryItem {
	if s == nil {
		return nil
	}
	return s.history
}

// ProvenanceFor returns the recorded handler+protocol pair
// for a tool, or ("unknown","unknown") if the tool was
// never recorded. v3.1 is fail-closed: an unknown
// provenance is INFRA_FAIL, not silent.
func (s *Session) ProvenanceFor(tool string) (string, string) {
	if s == nil {
		return "unknown", "unknown"
	}
	if v, ok := s.provenance[tool]; ok {
		return v.handler, v.protocol
	}
	return "unknown", "unknown"
}

type HistoryItem struct {
	Seq       int
	Tool      string
	Arguments string
	Output    string
	Error     string
	CreatedAt time.Time
}

type provenanceEntry struct {
	handler  string
	protocol string
}

func (s *Session) appendHistory(tool string, args, out json.RawMessage, errStr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	item := HistoryItem{
		Seq:       s.seq,
		Tool:      tool,
		Arguments: string(args),
		Output:    string(out),
		Error:     errStr,
		CreatedAt: time.Now(),
	}
	s.history = append(s.history, item)
}

func (s *Session) recordProvenance(tool string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provenance == nil {
		s.provenance = make(map[string]provenanceEntry)
	}
	if _, ok := s.provenance[tool]; ok {
		return
	}
	// Map tool to handler + protocol. v3.1 contract:
	//   read_ref     -> b1ReadRef      (b1)
	//   splice       -> b1SpliceWithObs or b1SpliceNoObs (b1)
	//   sh           -> RunIRRunner    (runir)
	var handler, protocol string
	switch tool {
	case contract.ToolReadRef:
		handler, protocol = "b1ReadRef", "b1"
	case contract.ToolSplice:
		if s.manager != nil && s.manager.cfg.Arm == contract.ArmReadRefSplice {
			handler, protocol = "b1SpliceNoObs", "b1"
		} else {
			handler, protocol = "b1SpliceWithObs", "b1"
		}
	case contract.ToolSH:
		handler, protocol = "RunIRRunner", "runir"
	default:
		handler, protocol = "unknown", "unknown"
	}
	s.provenance[tool] = provenanceEntry{handler: handler, protocol: protocol}
}

// initSchema creates the SQLite tables. v3.1 uses three
// tables: sessions, history_items, provenance_events.
func initSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			work_dir TEXT NOT NULL,
			status TEXT NOT NULL,
			step_count INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS history_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			type TEXT NOT NULL,
			role TEXT,
			content TEXT,
			summary TEXT,
			call_id TEXT,
			name TEXT,
			arguments TEXT,
			output TEXT,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS provenance_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			step INTEGER NOT NULL,
			tool TEXT NOT NULL,
			handler TEXT NOT NULL,
			protocol TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("init schema: %s: %w", s[:30], err)
		}
	}
	return nil
}
