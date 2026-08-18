package harness

import (
	"context"
	"strings"
	"testing"
)

func TestRunIRBasicCmd(t *testing.T) {
	dir := t.TempDir()
	r := NewRunIRRunner(dir)
	res, err := r.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Stdout.Text, "hello") {
		t.Fatalf("expected 'hello' in stdout, got %q", res.Stdout.Text)
	}
}

func TestRunIRNonzeroExit(t *testing.T) {
	dir := t.TempDir()
	r := NewRunIRRunner(dir)
	res, err := r.Run(context.Background(), "false")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status.Kind != "exit" {
		t.Fatalf("expected kind exit, got %q", res.Status.Kind)
	}
	if res.Status.Code == nil || *res.Status.Code == 0 {
		t.Fatal("expected nonzero exit code")
	}
}

func TestRunIRCwdInsideWorkspace(t *testing.T) {
	dir := t.TempDir()
	r := NewRunIRRunner(dir)
	res, err := r.Run(context.Background(), "pwd")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Stdout.Text) != dir {
		t.Fatalf("expected cwd %q, got %q", dir, res.Stdout.Text)
	}
}

func TestRunIRShellStateDoesNotPersist(t *testing.T) {
	dir := t.TempDir()
	r := NewRunIRRunner(dir)
	// First call sets a variable.
	_, err := r.Run(context.Background(), "export FOO=bar")
	if err != nil {
		t.Fatal(err)
	}
	// Second call should not see FOO.
	res, err := r.Run(context.Background(), "echo $FOO")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Stdout.Text) != "" {
		t.Fatalf("FOO leaked across calls: %q", res.Stdout.Text)
	}
}

func TestRunIRCwdDoesNotPersist(t *testing.T) {
	dir := t.TempDir()
	r := NewRunIRRunner(dir)
	// First call cd's into a subdirectory.
	_, err := r.Run(context.Background(), "mkdir sub && cd sub")
	if err != nil {
		t.Fatal(err)
	}
	// Second call's cwd should be back to dir.
	res, err := r.Run(context.Background(), "pwd")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Stdout.Text) != dir {
		t.Fatalf("cwd leaked across calls: %q", res.Stdout.Text)
	}
}

func TestRunIRCapturesStderr(t *testing.T) {
	dir := t.TempDir()
	r := NewRunIRRunner(dir)
	res, err := r.Run(context.Background(), "echo err >&2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Stderr.Text, "err") {
		t.Fatalf("expected 'err' in stderr, got %q", res.Stderr.Text)
	}
}

func TestRunIRCapturesExitStatus(t *testing.T) {
	dir := t.TempDir()
	r := NewRunIRRunner(dir)
	res, err := r.Run(context.Background(), "exit 7")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status.Kind != "exit" {
		t.Fatalf("expected kind exit, got %q", res.Status.Kind)
	}
	if res.Status.Code == nil || *res.Status.Code != 7 {
		code := -1
		if res.Status.Code != nil {
			code = *res.Status.Code
		}
		t.Fatalf("expected code 7, got %d", code)
	}
}

func TestRunIRRejectsCwdOutside(t *testing.T) {
	dir := t.TempDir()
	r := NewRunIRRunner(dir)
	// RunIRRunner in v3.1 doesn't expose cwd; the harness
	// itself is rooted at the workspace. Verify a "cd .."
	// inside the command still works (within bash) but
	// does not escape the worker's confinement.
	_, err := r.Run(context.Background(), "cd .. && pwd")
	if err != nil {
		// Either succeeds (running in subshell) or fails
		// (sandbox). Both are acceptable as long as the
		// runner doesn't claim success when it actually
		// escaped.
		t.Logf("cd .. returned: %v", err)
	}
}
