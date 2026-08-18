package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tulior/kalip/internal/contract"
)

func mustWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func TestScanEmpty(t *testing.T) {
	dir := t.TempDir()
	s, _ := b1NewService(dir)
	mustWrite(t, dir, "x.txt", "")
	resp, err := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text == "" {
		t.Fatal("expected header even for empty file")
	}
}

func TestScanMissingFileRejected(t *testing.T) {
	dir := t.TempDir()
	s, _ := b1NewService(dir)
	_, err := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	if b1FailureCodeOf(err) != contract.ErrNotARegularFile {
		t.Fatalf("expected not_a_regular_file error, got: %v", err)
	}
}

func TestScanSingleLineNoNewline(t *testing.T) {
	dir := t.TempDir()
	s, _ := b1NewService(dir)
	mustWrite(t, dir, "x.txt", "single line, no newline")
	resp, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	if !contains(resp.Text, "single line, no newline") {
		t.Fatal("missing content")
	}
}

func TestScanMultipleLines(t *testing.T) {
	dir := t.TempDir()
	s, _ := b1NewService(dir)
	mustWrite(t, dir, "x.txt", "a\nb\nc\n")
	resp, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	for _, want := range []string{"|a", "|b", "|c"} {
		if !contains(resp.Text, want) {
			t.Errorf("missing %q in %q", want, resp.Text)
		}
	}
}

func TestScanCRLF(t *testing.T) {
	dir := t.TempDir()
	s, _ := b1NewService(dir)
	mustWrite(t, dir, "x.txt", "a\r\nb\r\n")
	resp, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	if !contains(resp.Text, "|a") {
		t.Fatalf("missing a in %q", resp.Text)
	}
}

func TestScanLargeFile(t *testing.T) {
	dir := t.TempDir()
	s, _ := b1NewService(dir)
	var data []byte
	for i := 1; i <= 1000; i++ {
		data = append(data, []byte("line\n")...)
	}
	mustWrite(t, dir, "x.txt", string(data))
	resp, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	for i := 1; i <= 1000; i++ {
		want := fmt.Sprintf("L%d", i)
		if !contains(resp.Text, want) {
			t.Fatalf("missing %s in resp", want)
		}
	}
}

func TestScanUtf8(t *testing.T) {
	dir := t.TempDir()
	s, _ := b1NewService(dir)
	mustWrite(t, dir, "x.txt", "héllo wörld\n")
	resp, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	if !contains(resp.Text, "héllo") {
		t.Fatalf("missing utf8: %q", resp.Text)
	}
}

func TestScanInvalidUtf8Rejected(t *testing.T) {
	dir := t.TempDir()
	s, _ := b1NewService(dir)
	// Write bytes that are not valid UTF-8.
	mustWrite(t, dir, "x.txt", "hello\xffworld\n")
	_, err := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	if b1FailureCodeOf(err) != contract.ErrNotUTF8 {
		t.Fatalf("expected not_utf8 error, got: %v", err)
	}
}

func TestScanLineTokenFormat(t *testing.T) {
	dir := t.TempDir()
	s, _ := b1NewService(dir)
	mustWrite(t, dir, "x.txt", "alpha\n")
	resp, _ := s.b1ReadRef(b1ReadRefRequest{Path: "x.txt"})
	// First line token must be L1:hh where hh is 2 hex chars.
	if !contains(resp.Text, "L1:") {
		t.Fatalf("missing L1: token: %q", resp.Text)
	}
}
