package harness

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/tulior/kalip/internal/contract"
)

func TestSessionNew(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if mgr == nil {
		t.Fatal("nil manager")
	}
	defer mgr.Close()
}

func TestSessionCreate(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, err := mgr.CreateSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if sess == nil {
		t.Fatal("nil session")
	}
}

func TestSessionDispatchReadRef(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "f.txt", "hello\n")
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	cfg.Arm = contract.ArmReadRefSpliceObserve
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)

	// Issue a read_ref tool call.
	resp, err := sess.Dispatch(context.Background(), "read_ref", map[string]any{
		"path": "f.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Output, "hello") {
		t.Fatalf("missing content: %q", resp.Output)
	}
}

func TestSessionDispatchSplice(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "f.txt", "x = 1\n")
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	cfg.Arm = contract.ArmReadRefSpliceObserve
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)

	// read_ref
	readResp, err := sess.Dispatch(context.Background(), "read_ref", map[string]any{
		"path": "f.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	// extract L1 token
	tok := extractFirstLineToken(t, readResp.Output)

	// splice
	spliceResp, err := sess.Dispatch(context.Background(), "splice", map[string]any{
		"ref": readResp.Ref,
		"edits": []map[string]any{{
			"at": tok, "old": "1", "new": "2",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !spliceResp.OK {
		t.Fatal("splice returned not OK")
	}
	// Verify file was changed.
	got := mustRead(t, dir+"/f.txt")
	if got != "x = 2\n" {
		t.Fatalf("file not updated: %q", got)
	}
}

func TestSessionDispatchSh(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)
	resp, err := sess.Dispatch(context.Background(), "sh", map[string]any{
		"cmd": "echo hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Output, "hello") {
		t.Fatalf("missing echo output: %q", resp.Output)
	}
}

func TestSessionDispatchUnknownTool(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)
	_, err := sess.Dispatch(context.Background(), "frobnicate", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestSessionHistoryPersists(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "f.txt", "x\n")
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)
	_, err := sess.Dispatch(context.Background(), "read_ref", map[string]any{
		"path": "f.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	hist := sess.History()
	if len(hist) == 0 {
		t.Fatal("expected history entry")
	}
}

func TestSessionProvenanceForKnownTool(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "f.txt", "x\n")
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)
	_, err := sess.Dispatch(context.Background(), "read_ref", map[string]any{
		"path": "f.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, protocol := sess.ProvenanceFor("read_ref")
	if handler == "" || handler == "unknown" {
		t.Fatalf("expected concrete handler, got %q", handler)
	}
	if protocol == "" || protocol == "unknown" {
		t.Fatalf("expected concrete protocol, got %q", protocol)
	}
}

func TestSessionProvenanceForSh(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)
	_, _ = sess.Dispatch(context.Background(), "sh", map[string]any{"cmd": "true"})
	handler, protocol := sess.ProvenanceFor("sh")
	if handler == "unknown" {
		t.Fatalf("expected concrete handler for sh, got %q", handler)
	}
	if protocol == "unknown" {
		t.Fatalf("expected concrete protocol for sh, got %q", protocol)
	}
}

func TestSessionProvenanceFailClosedForUnknown(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)
	handler, protocol := sess.ProvenanceFor("no_such_tool")
	if handler != "unknown" || protocol != "unknown" {
		t.Fatalf("expected unknown/unknown for unrecorded tool, got %s/%s", handler, protocol)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
