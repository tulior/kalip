package harness

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newSh(t *testing.T) *Sh {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sh, err := NewSh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sh.Close() })
	return sh
}

func TestShBasicCommand(t *testing.T) {
	sh := newSh(t)
	r, err := sh.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode != 0 {
		t.Errorf("exit code: got %d, want 0", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "hello") {
		t.Errorf("stdout: got %q, want 'hello'", r.Stdout)
	}
}

func TestShExitCodePropagates(t *testing.T) {
	sh := newSh(t)
	r, err := sh.Run(context.Background(), "exit 7")
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode != 7 {
		t.Errorf("exit code: got %d, want 7", r.ExitCode)
	}
}

func TestShCwdPersists(t *testing.T) {
	sh := newSh(t)
	dir := t.TempDir()
	// First call: cd into the dir.
	if _, err := sh.Run(context.Background(), "cd "+dir); err != nil {
		t.Fatal(err)
	}
	// Second call: pwd should still be in the dir.
	r, err := sh.Run(context.Background(), "pwd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Stdout, dir) {
		t.Errorf("cwd: got %q, want it to contain %q", r.Stdout, dir)
	}
}

func TestShEnvPersists(t *testing.T) {
	sh := newSh(t)
	if _, err := sh.Run(context.Background(), "export FOO=bar"); err != nil {
		t.Fatal(err)
	}
	r, err := sh.Run(context.Background(), "echo $FOO")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Stdout, "bar") {
		t.Errorf("env: got %q, want 'bar'", r.Stdout)
	}
}

func TestShTruncatesLargeOutput(t *testing.T) {
	sh := newSh(t)
	// Generate 100 KiB of output.
	r, err := sh.Run(context.Background(), "yes A | head -c 102400")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated {
		t.Errorf("expected truncated=true, got false; stdout len=%d", len(r.Stdout))
	}
	if len(r.Stdout) > OutputByteCap {
		t.Errorf("stdout exceeds cap: %d > %d", len(r.Stdout), OutputByteCap)
	}
	if !strings.Contains(r.TruncMessage, "truncated") {
		t.Errorf("truncation message missing recovery hint: %q", r.TruncMessage)
	}
}

func TestShStripsAnsi(t *testing.T) {
	sh := newSh(t)
	r, err := sh.Run(context.Background(), "printf '\\033[31mRED\\033[0m'")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.Stdout, "\x1b") {
		t.Errorf("ANSI not stripped: %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "RED") {
		t.Errorf("content lost: %q", r.Stdout)
	}
}

func TestShStripsCR(t *testing.T) {
	sh := newSh(t)
	r, err := sh.Run(context.Background(), "printf 'hello\\r\\nworld'")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.Stdout, "\r") {
		t.Errorf("CR not stripped: %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "hello") || !strings.Contains(r.Stdout, "world") {
		t.Errorf("content lost: %q", r.Stdout)
	}
}

func TestShSentinelNotInOutput(t *testing.T) {
	sh := newSh(t)
	r, err := sh.Run(context.Background(), "echo done")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.Stdout, "KALIP_DONE") {
		t.Errorf("sentinel leaked into output: %q", r.Stdout)
	}
}

func TestShCommandNotFound(t *testing.T) {
	sh := newSh(t)
	r, err := sh.Run(context.Background(), "this-command-does-not-exist-12345")
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode == 0 {
		t.Errorf("expected nonzero exit, got 0")
	}
}

func TestShPipeFailurePropagates(t *testing.T) {
	sh := newSh(t)
	// pipefail is set in init: this should report failure
	// from `false` even though `true` is the last command.
	r, err := sh.Run(context.Background(), "true | false")
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode == 0 {
		t.Errorf("expected nonzero exit under pipefail, got 0")
	}
}

func TestShRunsInBackgroundContext(t *testing.T) {
	sh := newSh(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := sh.Run(ctx, "sleep 10")
	if err == nil {
		t.Errorf("expected timeout error, got nil")
	}
}

func TestStripNoise(t *testing.T) {
	in := []byte("\x1b[31mhello\x1b[0m\r\nworld\r")
	out := string(stripNoise(in))
	if strings.Contains(out, "\x1b") {
		t.Errorf("ANSI not stripped: %q", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("CR not stripped: %q", out)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Errorf("content lost: %q", out)
	}
}

func TestSentinelIDStable(t *testing.T) {
	a := sentinelID("echo hello")
	b := sentinelID("echo hello")
	c := sentinelID("echo world")
	if a != b {
		t.Errorf("same input should give same id: %d != %d", a, b)
	}
	if a == c {
		t.Errorf("different input should give different id: %d == %d", a, c)
	}
}

func TestSentinelRE(t *testing.T) {
	m := sentinelRE.FindStringSubmatch("before <<<KALIP_DONE_42:7>>> after")
	if len(m) < 3 {
		t.Fatalf("no match: %v", m)
	}
	if m[1] != "42" {
		t.Errorf("id: got %q, want 42", m[1])
	}
	if m[2] != "7" {
		t.Errorf("exit code: got %q, want 7", m[2])
	}
}

func TestBoundedReaderStops(t *testing.T) {
	r := newBoundedReader(strings.NewReader("hello world"), 5)
	got := make([]byte, 100)
	n, err := r.Read(got)
	if err != nil {
		t.Errorf("err on first read: %v", err)
	}
	if string(got[:n]) != "hello" {
		t.Errorf("got %q, want 'hello'", got[:n])
	}
	n, err = r.Read(got)
	if err == nil {
		t.Errorf("expected EOF on exhausted reader")
	}
	if n != 0 {
		t.Errorf("expected 0 bytes, got %d", n)
	}
}
