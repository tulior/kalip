// Package harness implements the kalip agent harness.
//
// The harness is a thin shell wrapper. Its only job is transport:
// spawn a persistent bash, accept commands, capture output, return
// structured results. Model behavior is governed by the system
// prompt in prompt.go. Repo history is git. The work is bash.
package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"time"

	"github.com/creack/pty"
)

// ShResult is the structured outcome of one shell command.
type ShResult struct {
	Stdout       string        `json:"stdout"`
	ExitCode     int           `json:"exit_code"`
	Duration     time.Duration `json:"duration"`
	Truncated    bool          `json:"truncated"`
	TruncMessage string        `json:"trunc_message,omitempty"`
}

// Byte caps.
const (
	OutputByteCap     = 30 * 1024 // 30 KiB hard cap on returned output
	TruncationReserve = 256       // bytes reserved for the truncation message
)

// Sentinel marks the end of a command's output. Format:
// <<<KALIP_DONE_<id>:<exit_code>>>
// where <id> is a per-call marker (to disambiguate when output
// is interleaved) and <exit_code> is the actual exit code.
var sentinelRE = regexp.MustCompile(`<<<KALIP_DONE_(-?\d+):(-?\d+)>>>`)

// ansiRE matches CSI escape sequences (\x1b[...letter).
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// carriageRE matches stray \r that PTYs sometimes emit.
var carriageRE = regexp.MustCompile(`\r`)

// Sh is a persistent bash subprocess exposed over a PTY.
//
// One Sh instance owns one bash. Commands are sent in order and
// run sequentially. cwd, env, shell state, and history persist
// across calls.
type Sh struct {
	cmd   *exec.Cmd
	ptmx  *os.File
	mu    sync.Mutex // serialize command submission
	closed bool
}

// rcFile writes the bash init script to a temp file and returns
// its path. The init script sets up TERM=dumb, enables pipefail,
// strips color from common commands, and disables history.
func rcFile() (string, error) {
	rc := `export TERM=dumb
shopt -s pipefail
alias ls='ls --color=never' 2>/dev/null
alias grep='grep --color=never' 2>/dev/null
alias diff='diff --color=never' 2>/dev/null
unset HISTFILE
PROMPT_COMMAND='printf "\033[KALIP_PROMPT_$$"'
`
	f, err := os.CreateTemp("", "kalip-rc-*.sh")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(rc); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

// NewSh spawns a fresh persistent bash.
//
// The shell is started with --noprofile --rcfile <tmp> -i, attached
// to a PTY for stdin/stdout/stderr. The init script sets up the
// environment (TERM=dumb, color aliases, pipefail, history off).
//
// The slave end of the PTY is kept open in the parent (assigned
// to cmd.Stdin/Stdout/Stderr) so the child has a live
// controlling terminal. The parent reads from the master
// end (ptmx) and writes commands to it.
func NewSh(ctx context.Context) (*Sh, error) {
	rcPath, err := rcFile()
	if err != nil {
		return nil, fmt.Errorf("rcfile: %w", err)
	}

	ptmx, tty, err := pty.Open()
	if err != nil {
		os.Remove(rcPath)
		return nil, fmt.Errorf("pty open: %w", err)
	}

	cmd := exec.CommandContext(ctx, "bash",
		"--noprofile", "--rcfile", rcPath, "-i")
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty

	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		_ = tty.Close()
		os.Remove(rcPath)
		return nil, fmt.Errorf("bash start: %w", err)
	}

	s := &Sh{
		cmd:  cmd,
		ptmx: ptmx,
	}

	// Reap the bash process when it exits. Don't close ptmx
	// or tty here — let Close() do that.
	go func() {
		_ = cmd.Wait()
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
	}()

	// Best-effort cleanup of the temp rcfile.
	go func() {
		time.Sleep(2 * time.Second)
		_ = os.Remove(rcPath)
	}()

	return s, nil
}

// Run executes one bash command and waits for completion.
//
// The command is sent to the persistent shell followed by a
// sentinel that prints the exit code. Output is read until the
// sentinel appears, then truncated to OutputByteCap bytes with a
// recovery hint.
func (s *Sh) Run(ctx context.Context, command string) (ShResult, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ShResult{}, errors.New("sh: shell closed")
	}
	s.mu.Unlock()

	start := time.Now()

	// The sentinel: emits the exit code of the command group.
	// We use printf with %d and $? so the actual exit code
	// is what we read back. The harness trusts bash to print
	// the sentinel — the model cannot bypass it because the
	// command is always wrapped in { ...; } 2>&1.
	sid := sentinelID(command)
	sentinel := fmt.Sprintf("<<<KALIP_DONE_%d:%d>>>", sid, 0) // placeholder; real code is from $?
	sentinelWithCmd := fmt.Sprintf("{ %s; } 2>&1; printf '<<<KALIP_DONE_%d:%%d>>>\\n' $?\n",
		command, sid)

	// Write the command.
	if _, err := io.WriteString(s.ptmx, sentinelWithCmd); err != nil {
		return ShResult{}, fmt.Errorf("sh: write: %w", err)
	}

	// Read until we see the sentinel. The PTY is line-buffered
	// and blocking; we use a deadline to avoid hanging forever
	// when the child produces no output.
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	deadline := time.Now().Add(60 * time.Second)
	_ = s.ptmx.SetReadDeadline(deadline)
	for {
		select {
		case <-ctx.Done():
			return ShResult{}, ctx.Err()
		default:
		}
		n, err := s.ptmx.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			// Timeout is expected when waiting for output.
			// Any other error (EIO from closed slave) is fatal.
			if isTimeout(err) {
				if bytes.Contains(buf.Bytes(), []byte(sentinel)) {
					break
				}
				if time.Now().After(deadline) {
					return ShResult{}, fmt.Errorf("sh: read timeout after %v", deadline)
				}
				// Otherwise keep reading.
				_ = s.ptmx.SetReadDeadline(time.Now().Add(2 * time.Second))
				continue
			}
			if !errors.Is(err, io.EOF) {
				return ShResult{}, fmt.Errorf("sh: read: %w", err)
			}
			break
		}
		// Got data. Reset deadline for the next chunk.
		_ = s.ptmx.SetReadDeadline(time.Now().Add(2 * time.Second))
		if bytes.Contains(buf.Bytes(), []byte(sentinel)) {
			break
		}
		if buf.Len() > OutputByteCap*4 {
			break
		}
	}

	raw := buf.Bytes()
	// Find any sentinel in the output and parse it.
	// The sentinel we sent includes the per-call id; the real
	// exit code is the second capture group.
	m := sentinelRE.FindSubmatch(raw)
	var exitCode int
	var outBytes []byte
	if m != nil {
		fmt.Sscanf(string(m[2]), "%d", &exitCode)
		// Output is everything before the sentinel line.
		idx := bytes.Index(raw, m[0])
		outBytes = raw[:idx]
	} else {
		// Sentinel missing — likely the cap killed it.
		outBytes = raw
		exitCode = -1
	}

	// Clean: strip CR, strip ANSI.
	cleaned := stripNoise(outBytes)

	// Truncate.
	truncated := false
	var truncMsg string
	if len(cleaned) > OutputByteCap {
		cleaned = cleaned[:OutputByteCap-TruncationReserve]
		truncated = true
		truncMsg = "...[truncated; inspect narrower ranges with grep -n / sed -n]\n"
	}

	return ShResult{
		Stdout:       string(cleaned),
		ExitCode:     exitCode,
		Duration:     time.Since(start),
		Truncated:    truncated,
		TruncMessage: truncMsg,
	}, nil
}

// sentinelID derives a small integer from the command string so
// the sentinel is per-call and can't be confused with prior output.
// It is not a security boundary; the shell is fully trusted.
func sentinelID(s string) int {
	var h uint32
	for i := 0; i < len(s); i++ {
		h = h*31 + uint32(s[i])
	}
	return int(h & 0x7fffffff)
}

// isTimeout reports whether an error from a network read was a
// timeout. PTY SetReadDeadline returns errors that satisfy this.
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	type timeout interface{ Timeout() bool }
	var t timeout
	if errors.As(err, &t) {
		return t.Timeout()
	}
	return false
}

// stripNoise removes CR and ANSI escape sequences.
func stripNoise(b []byte) []byte {
	b = carriageRE.ReplaceAll(b, nil)
	b = ansiRE.ReplaceAll(b, nil)
	return b
}

// Close terminates the persistent shell.
func (s *Sh) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.ptmx.Close()
	return nil
}

// boundedReader reads at most N bytes total then returns EOF.
type boundedReader struct {
	r       io.Reader
	remain  int
	started bool
}

func newBoundedReader(r io.Reader, n int) *boundedReader {
	return &boundedReader{r: r, remain: n}
}

func (b *boundedReader) Read(p []byte) (int, error) {
	if b.remain <= 0 {
		return 0, io.EOF
	}
	if len(p) > b.remain {
		p = p[:b.remain]
	}
	n, err := b.r.Read(p)
	b.remain -= n
	return n, err
}
