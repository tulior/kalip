// Package harness — file scanning and line ledger construction.
//
// These helpers split a UTF-8 file into physical lines (each with
// its byte offsets and content bounds), produce the Lnn:hh anchor
// token for a line, and build a fresh line ledger for a new
// observation.
package harness

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/tulior/kalip/internal/contract"
)

// b1PhysicalLine describes one physical line in a text file.
// StartByte is the first byte of line content; ContentEnd is
// the position of the trailing CR/LF (or end of file for the
// last line without a terminator); AfterByte is the position
// after the trailing CR/LF/CRLF (or equal to ContentEnd if the
// line has no terminator).
type b1PhysicalLine struct {
	StartByte  int
	ContentEnd int
	AfterByte  int
}

// b1SplitPhysicalLines scans content for line boundaries and
// returns one b1PhysicalLine per logical line. CR, LF, and CRLF
// are all recognised as line terminators.
func b1SplitPhysicalLines(content []byte) []b1PhysicalLine {
	lines := make([]b1PhysicalLine, 0, 64)
	start := 0
	i := 0
	for i < len(content) {
		switch content[i] {
		case '\r':
			// CR or CRLF
			contentEnd := i
			after := i + 1
			if i+1 < len(content) && content[i+1] == '\n' {
				after = i + 2
				i++
			}
			lines = append(lines, b1PhysicalLine{StartByte: start, ContentEnd: contentEnd, AfterByte: after})
			start = after
		case '\n':
			lines = append(lines, b1PhysicalLine{StartByte: start, ContentEnd: i, AfterByte: i + 1})
			start = i + 1
		}
		i++
	}
	if start < len(content) {
		// Final unterminated line.
		lines = append(lines, b1PhysicalLine{StartByte: start, ContentEnd: len(content), AfterByte: len(content)})
	} else if start == len(content) && len(content) > 0 && (content[len(content)-1] == '\n' || content[len(content)-1] == '\r') {
		// Trailing terminator on the last line. Don't append an empty line.
	}
	return lines
}

// b1SplitLinesWithTerm is an alias kept for the v3.1 symbol
// table. It returns the same result as b1SplitPhysicalLines.
func b1SplitLinesWithTerm(content []byte) []b1PhysicalLine {
	return b1SplitPhysicalLines(content)
}

// b1ReadTextFile reads the file at absPath and validates it is
// a regular file within the byte cap and is valid UTF-8. It
// returns the bytes and the line ledger.
func (s *b1Service) b1ReadTextFile(absPath string) ([]byte, []b1PhysicalLine, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, nil, b1Fail(contract.ErrNotARegularFile, "stat %s: %v; no edits were applied", absPath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, b1Fail(contract.ErrNotARegularFile, "not a regular file: %s", absPath)
	}
	if info.Size() > s.maxFileBytes {
		return nil, nil, b1Fail(contract.ErrFileTooLarge,
			"file is %d bytes; max is %d", info.Size(), s.maxFileBytes)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, nil, b1Fail(contract.ErrNotARegularFile, "read %s: %v; no edits were applied", absPath, err)
	}
	if !utf8.Valid(data) {
		return nil, nil, b1Fail(contract.ErrNotUTF8, "read_ref supports UTF-8 text files only: %s", absPath)
	}
	return data, b1SplitPhysicalLines(data), nil
}

// b1ExtractLineRange returns the byte slice of content from
// line start to line end (1-based, inclusive). It is the
// pre-state side of the post-state computation: the harness
// uses it to find the changed region's surrounding context.
func b1ExtractLineRange(content []byte, lines []b1PhysicalLine, start, end int) []byte {
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return nil
	}
	return content[lines[start-1].StartByte:lines[end-1].AfterByte]
}

// b1MakeLineToken computes the Lnn:hh token for line n with
// the given content bytes. The hash is the first two hex digits
// of sha256(line content). The token is what read_ref emits
// in the response and what splice consumes in the at/start/end/after
// fields.
func b1MakeLineToken(n int, content []byte) string {
	sum := sha256.Sum256(content)
	hh := fmt.Sprintf("%02x", sum[0])
	return "L" + intToA(n) + ":" + hh
}

// intToA is a small Itoa that avoids importing strconv in
// this file's hot path. (strconv is imported in b1.go.)
func intToA(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// isHex reports whether c is a lowercase hex digit.
func isHex(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

// joinLines joins lines with '\n'. Used in b1BuildPostState's
// pre-state text rendering; in practice b1BuildPostState works
// on byte ranges directly, but joinLines is retained for
// diagnostic output and unit tests.
func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

// sameFile reports whether two paths refer to the same file.
// Used by the provenance auditor to check for cross-file edits.
func sameFile(a, b string) bool {
	if a == b {
		return true
	}
	ap, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	bp, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	return ap == bp
}
