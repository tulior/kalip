package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tulior/kalip/internal/contract"
)

// TestSpliceB1CurrentReturnsOnlyOK verifies that the B_current
// arm (ArmReadRefSplice) returns just {"ok": true} for splice,
// not a post-state.
func TestSpliceB1CurrentReturnsOnlyOK(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "f.txt", "x = 1\n")
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	cfg.Arm = contract.ArmReadRefSplice
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)

	readResp, err := sess.Dispatch(context.Background(), "read_ref", map[string]any{
		"path": "f.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	tok := extractFirstLineToken(t, readResp.Output)

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
		t.Fatal("splice not OK")
	}
	if spliceResp.PostState != "" {
		t.Fatalf("B_current should not return post-state, got %q", spliceResp.PostState)
	}
}

// TestSpliceB1FixedReturnsPostState verifies that the B_fixed
// arm (ArmReadRefSpliceObserve) returns a post-state.
func TestSpliceB1FixedReturnsPostState(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "f.txt", "x = 1\n")
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	cfg.Arm = contract.ArmReadRefSpliceObserve
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)

	readResp, _ := sess.Dispatch(context.Background(), "read_ref", map[string]any{
		"path": "f.txt",
	})
	tok := extractFirstLineToken(t, readResp.Output)

	spliceResp, _ := sess.Dispatch(context.Background(), "splice", map[string]any{
		"ref": readResp.Ref,
		"edits": []map[string]any{{
			"at": tok, "old": "1", "new": "2",
		}},
	})
	if !spliceResp.OK {
		t.Fatal("splice not OK")
	}
	if !strings.Contains(spliceResp.PostState, "x = 2") {
		t.Fatalf("B_fixed post-state missing new content: %q", spliceResp.PostState)
	}
}

// TestSpliceValidatesAllEditsBeforeWrite verifies that a single
// invalid edit causes the whole splice to be rejected with no
// file modification.
func TestSpliceValidatesAllEditsBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "f.txt", "line1\nline2\nline3\nline4\n")
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	cfg.Arm = contract.ArmReadRefSpliceObserve
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)

	readResp, _ := sess.Dispatch(context.Background(), "read_ref", map[string]any{
		"path": "f.txt",
	})
	tokens := extractAllLineTokens(t, readResp.Output)
	before, _ := os.ReadFile(filepath.Join(dir, "f.txt"))

	// First edit is valid; second is invalid (old_not_in_anchor).
	// Use the b1 service directly.
	_, err := sess.b1.b1SpliceWithObs(b1SpliceRequest{
		Ref: readResp.Ref,
		Edits: []b1SpliceEdit{
			{Start: tokens[0], End: tokens[0], Text: "A\n"},
			{At: tokens[1], Old: "nonexistent", New: "y"},
		},
	})
	if err == nil {
		t.Fatal("expected error from second edit")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(before) != string(after) {
		t.Fatalf("file modified despite validation failure: before=%q after=%q", before, after)
	}
}

// TestSpliceSnapshotHashProtectsAgainstExternalWrite verifies
// that an external write to the file invalidates the snapshot.
func TestSpliceSnapshotHashProtectsAgainstExternalWrite(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "f.txt", "x = 1\n")
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)

	readResp, _ := sess.Dispatch(context.Background(), "read_ref", map[string]any{
		"path": "f.txt",
	})
	tok := extractFirstLineToken(t, readResp.Output)

	// External write corrupts the snapshot.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("y = 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := sess.b1.b1SpliceWithObs(b1SpliceRequest{
		Ref:   readResp.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "1", New: "2"}},
	})
	if b1FailureCodeOf(err) != contract.ErrStaleSnapshot {
		t.Fatalf("expected stale_snapshot, got %v", err)
	}
}

// TestSpliceThenPostStateShowsNewLine verifies that the
// post-state shows the NEW line after a successful splice,
// rendered from disk.
func TestSpliceThenPostStateShowsNewLine(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "f.txt", "old\n")
	cfg := DefaultConfig()
	cfg.WorkDir = dir
	cfg.Arm = contract.ArmReadRefSpliceObserve
	mgr, _ := NewManager(cfg)
	defer mgr.Close()
	sess, _ := mgr.CreateSession(dir)

	readResp, _ := sess.Dispatch(context.Background(), "read_ref", map[string]any{
		"path": "f.txt",
	})
	tok := extractFirstLineToken(t, readResp.Output)

	spliceResp, _ := sess.Dispatch(context.Background(), "splice", map[string]any{
		"ref": readResp.Ref,
		"edits": []map[string]any{{
			"at": tok, "old": "old", "new": "NEW",
		}},
	})
	if !strings.Contains(spliceResp.PostState, "NEW") {
		t.Fatalf("post-state missing NEW: %q", spliceResp.PostState)
	}
	if strings.Contains(spliceResp.PostState, "old") {
		t.Fatalf("post-state still has old text: %q", spliceResp.PostState)
	}
}
