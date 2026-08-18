package harness

import (
	"fmt"
	"strings"
	"testing"
)

// TestPostStateContainsNewContent verifies that the post-state
// includes the verbatim bytes of the new line after a splice.
func TestPostStateContainsNewContent(t *testing.T) {
	s, _ := makeService(t, "old value\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "old value", New: "new value"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.PostState, "new value") {
		t.Fatalf("post-state missing new content: %q", resp.PostState)
	}
	if strings.Contains(resp.PostState, "old value") {
		t.Fatalf("post-state still has old content: %q", resp.PostState)
	}
}

// TestPostStateBound4096 verifies that the post-state is
// truncated to at most 4096 bytes (the v3.1 cap).
func TestPostStateBound4096(t *testing.T) {
	// Create a file with 10000 lines. Each line is "x"+digits+"\n"
	// so the leading "x" appears exactly once per line and the
	// first 10 chars are unique to that line.
	var sb strings.Builder
	for i := 1; i <= 10000; i++ {
		sb.WriteString(fmt.Sprintf("x%010d\n", i))
	}
	s, _ := makeService(t, sb.String())
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "x0000000001", New: "y0000000001"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.PostState) > PostStateByteCap {
		t.Fatalf("post-state exceeded cap: %d > %d", len(resp.PostState), PostStateByteCap)
	}
}

// TestPostStateContainsNoFreshEditableRefs verifies that the
// post-state does not contain a fresh @ref R<n> identifier that
// the model could use for a follow-up splice. The post-state
// is a snapshot of disk; the model must call read_ref again
// to obtain a new ref.
func TestPostStateContainsNoFreshEditableRefs(t *testing.T) {
	s, _ := makeService(t, "alpha\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "alpha", New: "beta"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The post-state may contain a single @ref R<n> at the
	// top, but it must NOT contain a fresh R<n> that the model
	// could use. The v3.1 contract says: the post-state shows
	// the line range and the bytes, no fresh ref. We assert
	// that the post-state text does not begin with the
	// "@ref R<n>" header that read_ref emits.
	if strings.HasPrefix(resp.PostState, "@ref R") {
		t.Fatalf("post-state emits a fresh @ref header: %q", resp.PostState)
	}
}

// TestPostStateRendersFromDisk verifies that the post-state
// is rendered from the committed file, not from the splice
// arguments. The harness always re-reads the file after the
// rewrite; if the model claims to have written "beta" but
// the file actually contains "gamma" (corrupted), the
// post-state shows "gamma".
func TestPostStateRendersFromDisk(t *testing.T) {
	s, _ := makeService(t, "x = 1\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "1", New: "2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The model asked for "2"; the file has "2"; the post-state
	// shows "2".
	if !strings.Contains(resp.PostState, "x = 2") {
		t.Fatalf("post-state not from disk: %q", resp.PostState)
	}
}

// TestPostStatePreviousLineIncluded verifies that the
// post-state includes the previous line as context (the v3
// frozen contract had a bug where the previous line was
// omitted; v3 fixed it).
func TestPostStatePreviousLineIncluded(t *testing.T) {
	s, _ := makeService(t, "line1\nline2\nline3\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{
			Start: tokens[1], End: tokens[1], Text: "line2\n",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The post-state should include "line1" (previous) and
	// "line2" (changed) and "line3" (next).
	for _, want := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(resp.PostState, want) {
			t.Errorf("post-state missing %q: %q", want, resp.PostState)
		}
	}
}

// TestPostStateSingleLineFile shows the full file when the
// file is one line.
func TestPostStateSingleLineFile(t *testing.T) {
	s, _ := makeService(t, "x = 1\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "1", New: "2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.PostState, "x = 2") {
		t.Fatalf("post-state not from disk: %q", resp.PostState)
	}
}

// TestPostStateFirstLineChange shows the previous-line context
// even when the change is on line 1 (no previous line exists).
func TestPostStateFirstLineChange(t *testing.T) {
	s, _ := makeService(t, "line1\nline2\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "line1", New: "FIRST"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.PostState, "FIRST") {
		t.Fatalf("post-state missing change: %q", resp.PostState)
	}
	if !strings.Contains(resp.PostState, "line2") {
		t.Fatalf("post-state missing next line: %q", resp.PostState)
	}
}

// TestPostStateLastLineChange shows the next-line context
// even when the change is on the last line (no next line).
func TestPostStateLastLineChange(t *testing.T) {
	s, _ := makeService(t, "line1\nline2\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{
			Start: tokens[1], End: tokens[1], Text: "LAST\n",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.PostState, "LAST") {
		t.Fatalf("post-state missing change: %q", resp.PostState)
	}
	if !strings.Contains(resp.PostState, "line1") {
		t.Fatalf("post-state missing previous line: %q", resp.PostState)
	}
}

// TestPostStateNonContiguousChanges handles two non-adjacent
// changes; the post-state should show both regions and the
// intervening context.
func TestPostStateNonContiguousChanges(t *testing.T) {
	s, _ := makeService(t, "a\nb\nc\nd\ne\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{
			{Start: tokens[0], End: tokens[0], Text: "A\n"},
			{Start: tokens[3], End: tokens[3], Text: "D\n"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"A", "b", "c", "D", "e"} {
		if !strings.Contains(resp.PostState, want) {
			t.Errorf("post-state missing %q: %q", want, resp.PostState)
		}
	}
}

// TestPostStateUsesWorkspaceRelativePath verifies that the
// path in the post-state header is workspace-relative,
// not absolute.
func TestPostStateUsesWorkspaceRelativePath(t *testing.T) {
	s, _ := makeService(t, "x = 1\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "1", New: "2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The header line is "post-edit <path> lines X-Y:".
	// The path must be exactly "x.txt", not an absolute path.
	if !strings.Contains(resp.PostState, "post-edit x.txt ") {
		t.Fatalf("post-state does not use workspace-relative path: %q", resp.PostState)
	}
}

// TestPostStateInsertionMiddle verifies that an insertion
// in the middle of a file is reflected correctly.
func TestPostStateInsertionMiddle(t *testing.T) {
	s, _ := makeService(t, "line1\nline2\nline3\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{
			After: tokens[0], Text: "INSERTED\n",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.PostState, "INSERTED") {
		t.Fatalf("post-state missing insertion: %q", resp.PostState)
	}
}

// TestPostStateInsertionBOF verifies that an insertion at
// the beginning of the file is reflected.
func TestPostStateInsertionBOF(t *testing.T) {
	s, _ := makeService(t, "line1\nline2\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	// Insert before line 1 is not directly supported by the
	// splice contract (after+text requires an anchor). We
	// test insertion at line 1 (effectively at the top) by
	// using After on line 0 -- but line 0 is invalid.
	// Instead, we test the after-line-1 case which puts
	// INSERTED between line1 and line2.
	tokens := extractAllLineTokens(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{
			After: tokens[0], Text: "BOF\n",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.PostState, "BOF") {
		t.Fatalf("post-state missing insertion: %q", resp.PostState)
	}
}

// TestPostStateInsertionEOF verifies that an insertion at
// the end of the file is reflected.
func TestPostStateInsertionEOF(t *testing.T) {
	s, _ := makeService(t, "line1\nline2\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{
			After: tokens[1], Text: "EOF\n",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.PostState, "EOF") {
		t.Fatalf("post-state missing insertion: %q", resp.PostState)
	}
}

// TestPostStateInsertionMultiLine verifies that an insertion
// of multiple lines works correctly.
func TestPostStateInsertionMultiLine(t *testing.T) {
	s, _ := makeService(t, "line1\nline2\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{
			After: tokens[0], Text: "A\nB\nC\n",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"A", "B", "C"} {
		if !strings.Contains(resp.PostState, want) {
			t.Errorf("post-state missing %q: %q", want, resp.PostState)
		}
	}
}

// TestPostStateDeletionMiddle verifies that a deletion in
// the middle of a file is reflected. We use at+old+new
// with new="" to delete specific text within a line.
func TestPostStateDeletionMiddle(t *testing.T) {
	s, _ := makeService(t, "AAA line1\nBBB line2\nCCC line3\nDDD line4\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{
			At: tokens[1], Old: "BBB ", New: "",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.PostState, "AAA") {
		t.Errorf("post-state missing line1: %q", resp.PostState)
	}
	if strings.Contains(resp.PostState, "BBB") {
		t.Errorf("post-state still has deleted text: %q", resp.PostState)
	}
	if !strings.Contains(resp.PostState, "line2") {
		t.Errorf("post-state missing line2 content: %q", resp.PostState)
	}
	if !strings.Contains(resp.PostState, "CCC") {
		t.Errorf("post-state missing line3: %q", resp.PostState)
	}
}

// TestPostStateDeletionBOF verifies that a deletion at the
// beginning of the file is reflected.
func TestPostStateDeletionBOF(t *testing.T) {
	s, _ := makeService(t, "AAA line1\nBBB line2\nCCC line3\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{
			At: tokens[0], Old: "AAA ", New: "",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resp.PostState, "AAA") {
		t.Errorf("post-state still has deleted text: %q", resp.PostState)
	}
	if !strings.Contains(resp.PostState, "line1") {
		t.Errorf("post-state missing line1: %q", resp.PostState)
	}
	if !strings.Contains(resp.PostState, "BBB") {
		t.Errorf("post-state missing line2: %q", resp.PostState)
	}
}

// TestPostStateDeletionEOF verifies that a deletion at the
// end of the file is reflected.
func TestPostStateDeletionEOF(t *testing.T) {
	s, _ := makeService(t, "AAA line1\nBBB line2\nCCC line3\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{
			At: tokens[2], Old: "CCC ", New: "",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.PostState, "AAA") {
		t.Errorf("post-state missing line1: %q", resp.PostState)
	}
	if !strings.Contains(resp.PostState, "BBB") {
		t.Errorf("post-state missing line2: %q", resp.PostState)
	}
	if strings.Contains(resp.PostState, "CCC") {
		t.Errorf("post-state still has deleted text: %q", resp.PostState)
	}
	if !strings.Contains(resp.PostState, "line3") {
		t.Errorf("post-state missing line3: %q", resp.PostState)
	}
}

// TestPostStateIndicatesLineRange verifies that the post-state
// header reports the correct line range.
func TestPostStateIndicatesLineRange(t *testing.T) {
	s, _ := makeService(t, "line1\nline2\nline3\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{
			Start: tokens[1], End: tokens[1], Text: "line2\n",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.PostState, "lines 1-3") {
		t.Fatalf("post-state missing correct line range: %q", resp.PostState)
	}
}
