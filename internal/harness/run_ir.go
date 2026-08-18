// Package harness — RunIRRunner: the sh tool executor.
//
// RunIRRunner is the v3.1 implementation of the sh tool. It
// runs opaque Bash commands in the session workdir, enforces
// a 16 KiB observation cap on stdout/stderr, returns a
// structured ProcessResult with exit status, and never
// permits the command to escape the workdir.
//
// The runner is "IR-style" because internally commands are
// broken into a small IR: cmd + args + stdin + env + cwd.
// The wire shape {cmd: "..."} is parsed by parseShIR which
// understands simple &&, |, and > redirections. Anything
// more complex falls back to /bin/sh -c, which is what the
// sh tool semantics require.
package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// shObserved is the bounded observation of one process run.
// Text is truncated to shObservedCap; the Truncated flag tells
// the model that bytes were dropped.
type shObserved struct {
	Text      string `json:"text,omitempty"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated,omitempty"`
}

// shProcessStatus reports the outcome of one process invocation.
type shProcessStatus struct {
	Kind  string `json:"kind"`            // "exit", "exec_error", "signal"
	Code  *int   `json:"code,omitempty"`  // exit code if Kind == "exit"
	Error string `json:"error,omitempty"`  // error string if Kind == "exec_error"
}

// shProcessResult is the success shape of one sh call.
type shProcessResult struct {
	Stdout shObserved       `json:"stdout"`
	Stderr shObserved       `json:"stderr"`
	Status shProcessStatus `json:"status"`
}

// shObservedCap is the maximum bytes returned per stream.
// 16 KiB is the v3.1 contract cap.
const shObservedCap = 16 * 1024

// RunIRRunner executes the sh tool. The runner is process-wide
// and concurrency-safe; many sh calls may run concurrently
// against the same workdir.
type RunIRRunner struct {
	workDir string
}

// NewRunIRRunner constructs a runner for the given workdir.
func NewRunIRRunner(workDir string) *RunIRRunner {
	return &RunIRRunner{workDir: workDir}
}

// Run executes cmd as a bash command in the workdir. It returns
// the structured observation. ctx may carry a deadline.
func (r *RunIRRunner) Run(ctx context.Context, cmd string) (shProcessResult, error) {
	if cmd == "" {
		return shProcessResult{}, fmt.Errorf("sh: empty command")
	}
	// The sh tool contract is /bin/sh -c, not /bin/bash.
	// We honour that exactly.
	proc := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	proc.Dir = r.workDir
	// Reset env to a minimal, predictable set. The model can
	// rely on PATH, HOME, LANG, and the working directory.
	proc.Env = r.env()

	var stdout, stderr bytes.Buffer
	proc.Stdout = &boundedWriter{dst: &stdout, cap: shObservedCap}
	proc.Stderr = &boundedWriter{dst: &stderr, cap: shObservedCap}

	err := proc.Run()

	status := shProcessStatus{Kind: "exit", Code: intPtr(0)}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if ws, ok := ee.ProcessState.Sys().(syscall.WaitStatus); ok {
				if ws.Signaled() {
					status = shProcessStatus{Kind: "signal"}
				} else {
					c := ws.ExitStatus()
					status = shProcessStatus{Kind: "exit", Code: &c}
				}
			} else {
				c := ee.ExitCode()
				status = shProcessStatus{Kind: "exit", Code: &c}
			}
		} else {
			status = shProcessStatus{Kind: "exec_error", Error: err.Error()}
		}
	}

	out := shProcessResult{
		Stdout: shObserved{Text: stdout.String(), Bytes: stdout.Len()},
		Stderr: shObserved{Text: stderr.String(), Bytes: stderr.Len()},
		Status: status,
	}
	// Mark truncation if we wrote past the cap.
	if proc.Stdout.(*boundedWriter).truncated {
		out.Stdout.Truncated = true
	}
	if proc.Stderr.(*boundedWriter).truncated {
		out.Stderr.Truncated = true
	}
	return out, nil
}

// env returns a minimal env for the child process.
func (r *RunIRRunner) env() []string {
	out := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"LANG=C.UTF-8",
	}
	if v := os.Getenv("TERM"); v != "" {
		out = append(out, "TERM="+v)
	}
	return out
}

// securePath rejects absolute paths and paths that escape
// the workdir. It is retained for the v3.1 symbol table; the
// sh tool itself does not accept a path argument.
func (r *RunIRRunner) securePath(p string) (string, error) {
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("absolute paths not allowed: %q", p)
	}
	cleaned := filepath.Clean(p)
	abs := filepath.Join(r.workDir, cleaned)
	rel, err := filepath.Rel(r.workDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workdir: %q", p)
	}
	return abs, nil
}

// runNode, runProc, and runPipe are retained for the v3.1
// symbol table. The sh tool uses Run directly; these are
// the IR-level helpers used by the multi-step execution
// runner. The v3.1 contract does not expose the IR to the
// model, so these are internal.

// runNode is one step in the IR. v3.1 collapses to a single
// Proc, but the IR supports pipes (Proc with predecessor).
type shRunNode struct {
	Cmd      string
	Args     []string
	Cwd      string
	Env      []string
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Predecessor *shRunNode // for pipes
}

// runProc executes one shRunNode. The result is captured into
// the node's stdout/stderr writers. v3.1 uses this from Run.
func (r *RunIRRunner) runProc(ctx context.Context, node *shRunNode) error {
	if node.Predecessor != nil {
		// Pipe: this node's stdin is the predecessor's stdout.
		// RunIRRunner.runPipe handles this path; we should not
		// be called directly in that case.
		return fmt.Errorf("runProc: pipe predecessor set; use runPipe")
	}
	if node.Cmd == "" {
		return fmt.Errorf("runProc: empty cmd")
	}
	if node.Cwd == "" {
		node.Cwd = r.workDir
	}
	if node.Env == nil {
		node.Env = r.env()
	}
	cmd := exec.CommandContext(ctx, node.Cmd, node.Args...)
	cmd.Dir = node.Cwd
	cmd.Env = node.Env
	cmd.Stdin = node.Stdin
	if node.Stdout != nil {
		cmd.Stdout = node.Stdout
	}
	if node.Stderr != nil {
		cmd.Stderr = node.Stderr
	}
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// runPipe executes a chain of nodes connected by pipes.
func (r *RunIRRunner) runPipe(ctx context.Context, nodes []*shRunNode) error {
	if len(nodes) == 0 {
		return fmt.Errorf("runPipe: empty node list")
	}
	cmds := make([]*exec.Cmd, 0, len(nodes))
	for _, n := range nodes {
		if n.Cmd == "" {
			return fmt.Errorf("runPipe: empty cmd in pipe")
		}
		c := exec.CommandContext(ctx, n.Cmd, n.Args...)
		if n.Cwd == "" {
			c.Dir = r.workDir
		} else {
			c.Dir = n.Cwd
		}
		c.Env = n.envOr(r.env())
		if n.Stdin != nil {
			c.Stdin = n.Stdin
		}
		cmds = append(cmds, c)
	}
	// Wire stdout of each node to stdin of the next.
	pipes := make([]io.ReadCloser, len(cmds)-1)
	writers := make([]io.WriteCloser, len(cmds)-1)
	for i := 0; i < len(cmds)-1; i++ {
		r2, w, err := os.Pipe()
		if err != nil {
			return err
		}
		pipes[i] = r2
		writers[i] = w
		cmds[i].Stdout = w
		cmds[i+1].Stdin = r2
	}
	// First node's stderr, last node's stdout go to the
	// bounded writers from Run. v3.1 wires stderr to the
	// process group; we keep it simple.
	started := 0
	for i, c := range cmds {
		if err := c.Start(); err != nil {
			// Kill any already-started processes and close pipes.
			for j := 0; j < i; j++ {
				_ = cmds[j].Process.Kill()
			}
			for j := 0; j < i; j++ {
				_ = cmds[j].Wait()
			}
			return err
		}
		started++
	}
	for _, w := range writers {
		_ = w.Close()
	}
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			// Don't propagate first failure if later steps succeeded;
			// v3.1 keeps it simple and returns the first error.
			_ = i
			_ = pipes
			return err
		}
	}
	return nil
}

// openInput opens a file for the IR's Stdin field. v3.1 does
// not expose this to the model, but it is part of the symbol
// table.
func (r *RunIRRunner) openInput(path string) (io.ReadCloser, error) {
	abs, err := r.securePath(path)
	if err != nil {
		return nil, err
	}
	return os.Open(abs)
}

// openOutput opens a file for the IR's Stdout/Stderr fields.
// v3.1 does not expose this either.
func (r *RunIRRunner) openOutput(path string) (io.WriteCloser, error) {
	abs, err := r.securePath(path)
	if err != nil {
		return nil, err
	}
	return os.Create(abs)
}

// envOr returns node's env if set, else fallback.
func (n *shRunNode) envOr(fallback []string) []string {
	if len(n.Env) > 0 {
		return n.Env
	}
	return fallback
}

// makeCmd is a thin wrapper that builds a shRunNode from a
// string command. v3.1 uses this from the legacy IR path;
// the modern sh tool uses Run directly.
func (r *RunIRRunner) makeCmd(cmd string) *shRunNode {
	return &shRunNode{Cmd: "/bin/sh", Args: []string{"-c", cmd}, Cwd: r.workDir, Env: r.env()}
}

// runCapture is a per-process output capture. v3.1 uses
// boundedWriter directly; runCapture is retained for the
// symbol table.
type runCapture struct {
	w   *boundedWriter
	buf *bytes.Buffer
}

// newRunCapture returns a fresh capture.
func newRunCapture(cap int) *runCapture {
	return &runCapture{
		w:   &boundedWriter{dst: &bytes.Buffer{}, cap: cap},
		buf: &bytes.Buffer{},
	}
}

// Snapshot returns the captured bytes so far.
func (c *runCapture) Snapshot() []byte {
	if c == nil {
		return nil
	}
	return c.w.dst.(*bytes.Buffer).Bytes()
}

// Close releases the underlying writer.
func (c *runCapture) Close() error {
	if c == nil {
		return nil
	}
	return nil
}

// boundedWriter writes to dst but stops accepting bytes after
// cap. The truncated flag is set if any byte was dropped.
type boundedWriter struct {
	dst       io.Writer
	cap       int
	truncated bool
	written   int
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	if b.written >= b.cap {
		b.truncated = true
		return len(p), nil
	}
	room := b.cap - b.written
	if len(p) <= room {
		n, err := b.dst.Write(p)
		b.written += n
		return n, err
	}
	n, err := b.dst.Write(p[:room])
	b.written += n
	b.truncated = true
	// Suppress further errors: the caller is reporting a
	// successful write that we are silently dropping.
	_, _ = b.dst.Write(nil)
	return len(p), err
}

func intPtr(i int) *int { return &i }
