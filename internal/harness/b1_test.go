package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tulior/kalip/internal/contract"
)

// makeService is a small test helper that creates a fresh
// b1Service rooted at a temp directory with a one-line file.
func makeService(t *testing.T, content string) (*b1Service, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := b1NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestB1ReadRefReturnsAnchorAndContent(t *testing.T) {
	s, _ := makeService(t, "alpha\nbeta\ngamma\n")
	resp, err := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.Text, "@ref R1\n@file x.txt\n\n") {
		t.Fatalf("missing header: %q", resp.Text[:80])
	}
	if !strings.Contains(resp.Text, "|alpha") || !strings.Contains(resp.Text, "|beta") {
		t.Fatalf("missing line content: %q", resp.Text)
	}
}

func TestB1ReadRefRejectsEmptyPath(t *testing.T) {
	s, _ := makeService(t, "x")
	_, err := s.b1ReadRef(b1ReadRefRequest{Path: ""})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if b1FailureCodeOf(err) != contract.ErrPathRequired {
		t.Fatalf("wrong code: %v", err)
	}
}

func TestB1ReadRefRejectsAbsolutePath(t *testing.T) {
	s, _ := makeService(t, "x")
	_, err := s.b1ReadRef(b1ReadRefRequest{Path: "/etc/passwd"})
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
	if b1FailureCodeOf(err) != contract.ErrPathEscapesWorkspace {
		t.Fatalf("wrong code: %v", err)
	}
}

func TestB1ReadRefRejectsTraversal(t *testing.T) {
	s, _ := makeService(t, "x")
	_, err := s.b1ReadRef(b1ReadRefRequest{Path: "../etc/passwd"})
	if err == nil {
		t.Fatal("expected error for traversal")
	}
}

func TestB1ReadRefNotARegularFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, _ := b1NewService(dir)
	_, err := s.b1ReadRef(b1ReadRefRequest{Path: "subdir"})
	if err == nil {
		t.Fatal("expected error for directory")
	}
}

func TestB1SpliceAtomicSubstitutionSucceeds(t *testing.T) {
	s, path := makeService(t, "    return x * 2\n")
	rr, err := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	if err != nil {
		t.Fatal(err)
	}
	// Extract the L1 token from the response.
	tok := extractFirstLineToken(t, rr.Text)
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "x * 2", New: "x * 1"}},
	})
	if err != nil {
		t.Fatalf("splice: %v", err)
	}
	if !resp.OK {
		t.Fatal("splice returned not-OK")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "    return x * 1\n" {
		t.Fatalf("file not updated: %q", got)
	}
	// Post-state must mention the committed line.
	if !strings.Contains(resp.PostState, "x * 1") {
		t.Fatalf("post-state missing new content: %q", resp.PostState)
	}
}

func TestB1SpliceRejectsInvalidShapeWhenEmptyOld(t *testing.T) {
	// Go's struct tags cannot distinguish missing from
	// empty string, so an "old: \"\"" edit is detected
	// as a malformed at+old+new shape (empty_old requires
	// the field to be present-but-empty, which is the
	// same wire shape as missing). The harness reports
	// invalid_editShape, which is the correct contract
	// behaviour.
	s, _ := makeService(t, "x\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "", New: "y"}},
	})
	if b1FailureCodeOf(err) != contract.ErrInvalidEditShape {
		t.Fatalf("wrong code: %v", err)
	}
}

func TestB1SpliceRejectsOldNotInAnchor(t *testing.T) {
	s, _ := makeService(t, "hello\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "missing", New: "y"}},
	})
	if b1FailureCodeOf(err) != contract.ErrOldNotFoundInAnchor {
		t.Fatalf("wrong code: %v", err)
	}
}

func TestB1SpliceRejectsOldMultipleMatches(t *testing.T) {
	s, _ := makeService(t, "ababab\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "ab", New: "cd"}},
	})
	if b1FailureCodeOf(err) != contract.ErrOldMultipleMatches {
		t.Fatalf("wrong code: %v", err)
	}
}

func TestB1SpliceRejectsStaleSnapshot(t *testing.T) {
	s, path := makeService(t, "alpha\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	// Mutate the file from outside the harness to break
	// the snapshot hash.
	if err := os.WriteFile(path, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: extractFirstLineToken(t, rr.Text), Old: "alpha", New: "beta"}},
	})
	if b1FailureCodeOf(err) != contract.ErrStaleSnapshot {
		t.Fatalf("wrong code: %v", err)
	}
}

func TestB1SpliceRejectsUnknownSnapshot(t *testing.T) {
	s, _ := makeService(t, "x\n")
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   "R9999",
		Edits: []b1SpliceEdit{{At: "L1:00", Old: "x", New: "y"}},
	})
	if b1FailureCodeOf(err) != contract.ErrUnknownSnapshot {
		t.Fatalf("wrong code: %v", err)
	}
}

func TestB1SpliceRejectsInvalidSnapshotRef(t *testing.T) {
	s, _ := makeService(t, "x\n")
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   "notaref",
		Edits: []b1SpliceEdit{{At: "L1:00", Old: "x", New: "y"}},
	})
	if b1FailureCodeOf(err) != contract.ErrInvalidSnapshotRef {
		t.Fatalf("wrong code: %v", err)
	}
}

func TestB1SpliceRejectsEmptyEdits(t *testing.T) {
	s, _ := makeService(t, "x\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	_, err := s.b1SpliceWithObs(b1SpliceRequest{Ref: rr.Ref, Edits: nil})
	if b1FailureCodeOf(err) != contract.ErrEmptyEdits {
		t.Fatalf("wrong code: %v", err)
	}
}

func TestB1SpliceRejectsInvalidEditShape(t *testing.T) {
	s, _ := makeService(t, "x\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	// Mixed: at + start + end + text
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Start: tok, End: tok, Text: "y"}},
	})
	if b1FailureCodeOf(err) != contract.ErrInvalidEditShape {
		t.Fatalf("wrong code: %v", err)
	}
}

func TestB1SpliceRangeReplacementPreservesTrailingLine(t *testing.T) {
	s, path := makeService(t, "line1\nline2\nline3\nline4\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	// Replace line2..line3 with single line "REPLACED"
	resp, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{{
			Start: tokens[1], End: tokens[2], Text: "REPLACED\n",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatal("not OK")
	}
	got, _ := os.ReadFile(path)
	want := "line1\nREPLACED\nline4\n"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestB1SpliceInsertion(t *testing.T) {
	s, path := makeService(t, "line1\nline2\n")
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
	if !resp.OK {
		t.Fatal("not OK")
	}
	got, _ := os.ReadFile(path)
	want := "line1\nINSERTED\nline2\n"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestB1SpliceRejectsOverlap(t *testing.T) {
	s, _ := makeService(t, "line1\nline2\nline3\nline4\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	// Two edits targeting the same line range.
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{
			{Start: tokens[0], End: tokens[1], Text: "A\n"},
			{Start: tokens[0], End: tokens[1], Text: "B\n"},
		},
	})
	if b1FailureCodeOf(err) != contract.ErrOverlap {
		t.Fatalf("wrong code: %v", err)
	}
}

func TestB1SpliceNoObsReturnsEmptyPostState(t *testing.T) {
	s, path := makeService(t, "x = 1\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	resp, err := s.b1SpliceNoObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "1", New: "2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatal("not OK")
	}
	if resp.PostState != "" {
		t.Fatalf("NoObs should not return post-state: %q", resp.PostState)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "x = 2\n" {
		t.Fatalf("file not updated: %q", got)
	}
}

func TestB1SplicePreservesIndentation(t *testing.T) {
	s, path := makeService(t, "\tx = 1\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "1", New: "2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), "\t") {
		t.Fatalf("indentation not preserved: %q", got)
	}
}

func TestB1SplicePreservesComment(t *testing.T) {
	s, path := makeService(t, "x = 1  # comment\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "1", New: "2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "# comment") {
		t.Fatalf("comment not preserved: %q", got)
	}
}

func TestB1SplicePreservesCRLF(t *testing.T) {
	s, path := makeService(t, "x = 1\r\ny = 2\r\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tok := extractFirstLineToken(t, rr.Text)
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref:   rr.Ref,
		Edits: []b1SpliceEdit{{At: tok, Old: "1", New: "3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "\r\n") {
		t.Fatalf("CRLF not preserved: %q", got)
	}
}

func TestB1SpliceAllOrNothingOnInvalid(t *testing.T) {
	s, path := makeService(t, "line1\nline2\nline3\n")
	rr, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	tokens := extractAllLineTokens(t, rr.Text)
	before, _ := os.ReadFile(path)
	// First edit is valid; second is invalid (overlap with first).
	_, err := s.b1SpliceWithObs(b1SpliceRequest{
		Ref: rr.Ref,
		Edits: []b1SpliceEdit{
			{Start: tokens[0], End: tokens[1], Text: "A\n"},
			{Start: tokens[0], End: tokens[1], Text: "B\n"},
		},
	})
	if err == nil {
		t.Fatal("expected overlap error")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("file was modified despite rejection: before=%q after=%q", before, after)
	}
}

// extractFirstLineToken parses the first Lnn:hh token from
// a read_ref response.
func extractFirstLineToken(t *testing.T, text string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, ":"); i > 0 && line[0] == 'L' {
			return line[:i+3]
		}
	}
	t.Fatalf("no Lnn:hh token in: %q", text)
	return ""
}

// extractAllLineTokens returns all Lnn:hh tokens in order.
func extractAllLineTokens(t *testing.T, text string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if len(line) > 4 && line[0] == 'L' {
			// find first '|'
			i := strings.Index(line, "|")
			if i < 0 {
				continue
			}
			if j := strings.LastIndex(line[:i], ":"); j > 0 {
				out = append(out, line[:j+3])
			}
		}
	}
	return out
}
