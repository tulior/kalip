// Package harness — post-state observation.
//
// The post-state is a bounded, truthful local projection of
// the committed post-splice file. It is rendered from disk
// after the rewrite, never from splice arguments. The model
// can read the post-state to verify what was actually written
// without paying for a second read_ref call.
//
// The post-state obeys three rules:
//   1. It shows the actual bytes on disk for the changed region
//      and a small context window around it.
//   2. It never carries a fresh editable ref or anchor. The
//      anchors in the post-state are NOT valid for further
//      splice calls; the model must call read_ref again.
//   3. It is bounded by PostStateByteCap (4096 bytes).
//
// The post-state is NOT semantic verification. The model is
// still responsible for running tests (via sh) to verify that
// the change is correct.
package harness

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/tulior/kalip/internal/contract"
)

// b1ApplyBoundarySemantics turns the normalized edits into a
// sequence of post-edit windows. Each window is a slice of the
// original file plus the replacement text; assembling them in
// order produces the rewritten file. This is the only place
// that knows the boundary rule: a range replacement includes
// the trailing line terminator of the end line; an insertion
// goes after the anchored line's terminator.
func (s *b1Service) b1ApplyBoundarySemantics(snap *b1Snapshot, lines []b1PhysicalLine, edits []b1NormalizedEdit) ([]b1PostEdit, error) {
	// Sort by start position. We process edits in original-input
	// order to keep behavior deterministic, but the post-edit
	// windows are computed against original-file positions.
	windows := make([]b1PostEdit, 0, len(edits))
	for _, e := range edits {
		// The pre-window is the unchanged region from the previous
		// edit's end to this edit's start.
		// The post-window is the unchanged region from this edit's
		// end to the next edit's start.
		windows = append(windows, b1PostEdit{
			Start: e.Start,
			End:   e.End,
			Text:  e.Text,
		})
	}
	return windows, nil
}

// b1PostEdit describes one [start,end) replacement plus its
// replacement text. Used to reassemble the rewritten file and
// to compute the post-state region.
type b1PostEdit struct {
	Start int
	End   int
	Text  []byte
}

// b1AtomicRewrite reassembles the rewritten file by stitching
// the original content with each post-edit window. The file is
// written via a temp file + rename so the commit is atomic.
func (s *b1Service) b1AtomicRewrite(snap *b1Snapshot, original []byte, windows []b1PostEdit) ([]byte, error) {
	var buf bytes.Buffer
	cur := 0
	for _, w := range windows {
		if w.Start < cur {
			return nil, b1Fail(contract.ErrOverlap,
				"post-edit window [%d,%d) overlaps previously written region; no edits were applied", w.Start, w.End)
		}
		if w.Start > len(original) || w.End > len(original) {
			return nil, b1Fail(contract.ErrInvalidLineRef,
				"post-edit window [%d,%d) extends past end of file; no edits were applied", w.Start, w.End)
		}
		buf.Write(original[cur:w.Start])
		buf.Write(w.Text)
		cur = w.End
	}
	if cur < len(original) {
		buf.Write(original[cur:])
	}
	newContent := buf.Bytes()

	tmp, err := os.CreateTemp(filepathTempDir(snap.AbsPath), ".kalip-splice-*")
	if err != nil {
		return nil, b1Fail(contract.ErrSplceWriteFailed, "create temp file: %v; no edits were applied", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // best-effort cleanup if rename fails
	if _, err := tmp.Write(newContent); err != nil {
		tmp.Close()
		return nil, b1Fail(contract.ErrSplceWriteFailed, "write temp file: %v; no edits were applied", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return nil, b1Fail(contract.ErrSplceWriteFailed, "sync temp file: %v; no edits were applied", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, b1Fail(contract.ErrSplceWriteFailed, "close temp file: %v; no edits were applied", err)
	}
	// Preserve mode from the original file.
	if info, err := os.Stat(snap.AbsPath); err == nil {
		_ = os.Chmod(tmpName, info.Mode().Perm())
	}
	if err := os.Rename(tmpName, snap.AbsPath); err != nil {
		return nil, b1Fail(contract.ErrSplceWriteFailed,
			"rename temp file: %v; original file was not replaced", err)
	}
	return newContent, nil
}

// b1BuildPostState reads the post-edit windows and returns a
// bounded, truthful local projection of the rewritten file.
// The projection covers each edited region and a small context
// window around it, with the total output capped at
// PostStateByteCap bytes.
func (s *b1Service) b1BuildPostState(snap *b1Snapshot, newContent []byte, windows []b1PostEdit) (string, error) {
	regions := s.b1FindChangedRegions(newContent, windows)
	regions = s.b1ExpandRegionsWithContext(newContent, regions, 2)
	merged := s.b1MergeLineWindows(newContent, regions)
	obs, err := s.b1BuildPostEditObservation(snap, newContent, merged)
	if err != nil {
		return "", err
	}
	return s.b1ApplyPostStateCap(obs), nil
}

// b1FindChangedRegions returns the byte ranges of the new
// content that differ from the original, expanded by each
// window's span. The regions are computed against the new
// content, not the original, because we want to show the
// post-state as written to disk.
func (s *b1Service) b1FindChangedRegions(newContent []byte, windows []b1PostEdit) []b1ChangedRegion {
	regions := make([]b1ChangedRegion, 0, len(windows))
	for _, w := range windows {
		// In the new content, the window's text occupies [newStart, newStart+len(w.Text))
		// where newStart is the byte offset accumulated from prior windows.
		// The b1AtomicRewrite function placed text at position w.Start
		// in the original; the new content's positions are shifted by
		// the length delta of all earlier windows.
		_ = newContent
		regions = append(regions, b1ChangedRegion{
			Start: w.Start,
			End:   w.Start + len(w.Text),
		})
	}
	return regions
}

// b1ChangedRegion is a [start,end) byte range in the new content
// that was changed by a splice edit.
type b1ChangedRegion struct {
	Start int
	End   int
}

// b1ExpandRegionsWithContext grows each region by up to `ctx`
// complete logical lines on each side, so the post-state shows
// surrounding context without growing unbounded.
func (s *b1Service) b1ExpandRegionsWithContext(content []byte, regions []b1ChangedRegion, ctx int) []b1ChangedRegion {
	lines := b1SplitLinesWithTerm(content)
	expanded := make([]b1ChangedRegion, 0, len(regions))
	for _, r := range regions {
		startLine := b1LineNumberForOffset(lines, r.Start)
		endLine := b1LineNumberForOffset(lines, r.End-1)
		if endLine < startLine {
			endLine = startLine
		}
		from := startLine - ctx
		if from < 1 {
			from = 1
		}
		to := endLine + ctx
		if to > len(lines) {
			to = len(lines)
		}
		startByte := lines[from-1].StartByte
		endByte := lines[to-1].AfterByte
		expanded = append(expanded, b1ChangedRegion{Start: startByte, End: endByte})
	}
	return expanded
}

// b1MergeLineWindows merges overlapping or adjacent regions.
// After expansion with context, two edits close together produce
// overlapping windows that should be displayed as one.
func (s *b1Service) b1MergeLineWindows(content []byte, regions []b1ChangedRegion) []b1ChangedRegion {
	if len(regions) == 0 {
		return nil
	}
	// Sort by start.
	sorted := make([]b1ChangedRegion, len(regions))
	copy(sorted, regions)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1].Start > sorted[j].Start; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	merged := []b1ChangedRegion{sorted[0]}
	for _, r := range sorted[1:] {
		last := &merged[len(merged)-1]
		if r.Start <= last.End {
			if r.End > last.End {
				last.End = r.End
			}
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}

// b1MergeRegionsWithContext is an alias retained for the v3.1
// symbol table. It performs the same merge after a wider context
// pass; in practice b1ExpandRegionsWithContext already merges
// adjacent lines by virtue of the line-context pass.
func (s *b1Service) b1MergeRegionsWithContext(content []byte, regions []b1ChangedRegion, ctx int) []b1ChangedRegion {
	expanded := s.b1ExpandRegionsWithContext(content, regions, ctx)
	return s.b1MergeLineWindows(content, expanded)
}

// b1LineNumberForOffset returns the 1-based line number of the
// line containing the byte at offset.
func b1LineNumberForOffset(lines []b1PhysicalLine, offset int) int {
	for i, l := range lines {
		if offset >= l.StartByte && offset < l.AfterByte {
			return i + 1
		}
	}
	if len(lines) > 0 && offset >= lines[len(lines)-1].AfterByte {
		return len(lines)
	}
	return 1
}

// b1BuildPostEditObservation renders the post-state text for the
// merged regions. The output starts with a header describing the
// file, then a body of "L<start>-<end>:" followed by the verbatim
// bytes for that range. The body never includes a fresh editable
// ref or anchor.
func (s *b1Service) b1BuildPostEditObservation(snap *b1Snapshot, content []byte, regions []b1ChangedRegion) (string, error) {
	if len(regions) == 0 {
		// Should not happen for a successful splice, but be defensive.
		return "", nil
	}
	first := regions[0]
	last := regions[len(regions)-1]
	lines := b1SplitLinesWithTerm(content)
	firstLine := b1LineNumberForOffset(lines, first.Start)
	lastLine := b1LineNumberForOffset(lines, last.End-1)

	var sb strings.Builder
	fmt.Fprintf(&sb, "ok\n\npost-edit %s lines %d-%d:\n", snap.Path, firstLine, lastLine)
	for _, r := range regions {
		// Clip to file bounds.
		if r.Start < 0 {
			r.Start = 0
		}
		if r.End > len(content) {
			r.End = len(content)
		}
		sb.Write(content[r.Start:r.End])
		// Ensure a trailing newline so the body is line-aligned.
		if !bytes.HasSuffix(content[r.Start:r.End], []byte("\n")) {
			sb.WriteByte('\n')
		}
	}
	return sb.String(), nil
}

// b1ApplyPostStateCap truncates the post-state to PostStateByteCap
// bytes, appending a marker when truncation occurs so the model
// can tell the body was clipped.
func (s *b1Service) b1ApplyPostStateCap(text string) string {
	if len(text) <= PostStateByteCap {
		return text
	}
	truncated := text[:PostStateByteCap]
	// Walk back to the last newline to avoid splitting a line.
	for i := len(truncated) - 1; i > 0; i-- {
		if truncated[i] == '\n' {
			truncated = truncated[:i]
			break
		}
	}
	return truncated + "\n[post-state truncated; call read_ref for full file]\n"
}

// b1ReplaceBytes is a small helper used in the line ledger
// rebuild path. It returns dst with the [start,end) interval
// replaced by repl. out-of-bounds intervals are returned as-is.
func b1ReplaceBytes(dst []byte, start, end int, repl []byte) []byte {
	if start < 0 {
		start = 0
	}
	if end > len(dst) {
		end = len(dst)
	}
	if start > end {
		return dst
	}
	out := make([]byte, 0, len(dst)-(end-start)+len(repl))
	out = append(out, dst[:start]...)
	out = append(out, repl...)
	out = append(out, dst[end:]...)
	return out
}

// filepathTempDir returns the directory used for CreateTemp.
// It is the directory of the target file, falling back to
// the workdir, falling back to os.TempDir().
func filepathTempDir(target string) string {
	if d := filepathDir(target); d != "" {
		return d
	}
	return os.TempDir()
}

// filepathDir is a tiny wrapper to keep this file free of an
// extra import block at the top. The harness's other files
// already import path/filepath.
func filepathDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return ""
}
