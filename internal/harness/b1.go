// Package harness — b1 service.
//
// The b1 service implements the B1 contract: read_ref returns
// source bytes with Lnn:hh line anchors; splice applies atomic
// edits using those anchors with optional local substitution
// (at+old+new), range replacement (start+end+text), and insertion
// (after+text). All edits are validated against a single snapshot
// and applied in original order. On success, the harness reads
// the committed file from disk and returns a bounded post-state
// observation describing what was actually written.
//
// The b1 service is fail-closed: any validation error rolls back
// the entire splice call. There is no partial application. There
// is no auto-retry, no fuzzy match, no fallback to a different
// edit dialect.
//
// The post-state is a read of disk bytes, not of splice arguments.
// This is a hard rule: the post-state must show what is on disk
// after commit, not what the model asked to write.
package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/tulior/kalip/internal/contract"
)

// b1Service is the B1 contract implementation. One service per
// workdir; the global registry in b1ServiceFor keys by absolute
// workdir path.
type b1Service struct {
	workDir      string
	maxReadLines int
	maxFileBytes int64

	nextRef atomic.Uint64

	mu       sync.RWMutex
	ledger   map[string]*b1Snapshot
}

// b1Snapshot is a single read_ref observation. The ledger keeps
// the file hash, file size, and the line ledger for every line
// that was exposed to the model. Splice rejects the snapshot
// if the file has been modified since observation.
type b1Snapshot struct {
	ID        string
	Path      string // workspace-relative path shown to the model
	AbsPath   string
	FileHash  [32]byte
	FileSize  int64

	// Only lines actually exposed by this read_ref call are valid
	// splice anchors.
	LinesByNumber map[int]b1LineLedger
}

// b1LineLedger records the byte offsets and the visible token
// for one line that read_ref showed to the model.
type b1LineLedger struct {
	Number     int
	Token      string // exact visible token, e.g. L38:f1
	StartByte  int    // first byte of line content
	ContentEnd int    // exclusive; before CR/LF
	AfterByte  int    // exclusive; after CRLF/LF/CR if present
	Content    string // exact observed content; telemetry only
}

// b1ReadRefRequest is the wire shape for read_ref.
type b1ReadRefRequest struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	LineCount int    `json:"line_count,omitempty"`
}

// b1ReadRefResponse is the wire shape for read_ref output.
// The text contains @ref R<n>, @file <path>, and Lnn:hh lines.
type b1ReadRefResponse struct {
	Ref  string
	Text string
}

// b1SpliceRequest is the wire shape for splice.
type b1SpliceRequest struct {
	Ref   string         `json:"ref"`
	Edits []b1SpliceEdit `json:"edits"`
}

// b1SpliceEdit is one edit entry. The harness enforces one of
// three shapes via field presence: at+old+new, start+end+text,
// or after+text. The model cannot mix shapes.
type b1SpliceEdit struct {
	// at+old+new: local substitution within one anchored line.
	At    string `json:"at,omitempty"`
	Old   string `json:"old,omitempty"`
	New   string `json:"new,omitempty"`

	// start+end+text: range replacement of complete logical lines.
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
	Text  string `json:"text,omitempty"`

	// after+text: insertion immediately after the anchored line.
	After string `json:"after,omitempty"`
}

// b1SpliceError is a typed failure. The Code is one of the
// contract.Err* constants; Message is the human-readable detail.
// The model is allowed to switch behavior based on Code.
type b1SpliceError struct {
	Code    string
	Message string
}

func (e *b1SpliceError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// b1NewService constructs a new b1 service rooted at workDir.
// workDir must be an absolute path.
func b1NewService(workDir string) (*b1Service, error) {
	if !filepath.IsAbs(workDir) {
		return nil, fmt.Errorf("workdir must be absolute: %q", workDir)
	}
	return &b1Service{
		workDir:      workDir,
		maxReadLines: DefaultMaxReadLines,
		maxFileBytes: DefaultMaxFileBytes,
		ledger:       make(map[string]*b1Snapshot),
	}, nil
}

// b1ServiceFor returns the b1 service for the given workDir,
// creating one if needed. The registry is package-global so
// that read_ref and splice see the same ledger across calls.
var (
	b1ServiceMu sync.Mutex
	b1Services  = make(map[string]*b1Service)
)

func b1ServiceFor(workDir string) (*b1Service, error) {
	b1ServiceMu.Lock()
	defer b1ServiceMu.Unlock()
	if s, ok := b1Services[workDir]; ok {
		return s, nil
	}
	s, err := b1NewService(workDir)
	if err != nil {
		return nil, err
	}
	b1Services[workDir] = s
	return s, nil
}

// b1FailureCodeOf returns the b1 error code embedded in err, or
// empty string if err is not a b1SpliceError. Used by the
// provenance auditor and the tool dispatcher to surface the
// typed failure code to the model.
func b1FailureCodeOf(err error) string {
	var be *b1SpliceError
	if errors.As(err, &be) {
		return be.Code
	}
	return ""
}

// b1Fail constructs a b1SpliceError with the given code and
// formatted message. The code must be one of the contract.Err*
// constants.
func b1Fail(code, format string, args ...any) error {
	return &b1SpliceError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// b1ResolvePath validates that path is non-empty, relative,
// workspace-bounded, and resolves to a regular UTF-8 file
// within the max-byte cap. It returns the absolute path.
func (s *b1Service) b1ResolvePath(path string) (string, error) {
	if path == "" {
		return "", b1Fail(contract.ErrPathRequired, "read_ref/splice path must be workspace-relative")
	}
	if filepath.IsAbs(path) {
		return "", b1Fail(contract.ErrPathEscapesWorkspace, "read_ref/splice path must be workspace-relative; got absolute %q", path)
	}
	cleaned := filepath.Clean(path)
	abs := filepath.Join(s.workDir, cleaned)
	rel, err := filepath.Rel(s.workDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", b1Fail(contract.ErrPathEscapesWorkspace, "path escapes workspace: %q", path)
	}
	return abs, nil
}

// b1ReadRef serves the read_ref tool. It validates the request,
// reads the file, builds the line ledger, allocates a fresh
// snapshot ID, and returns the formatted observation text.
func (s *b1Service) b1ReadRef(req b1ReadRefRequest) (b1ReadRefResponse, error) {
	abs, err := s.b1ResolvePath(req.Path)
	if err != nil {
		return b1ReadRefResponse{}, err
	}

	text, lines, err := s.b1ReadTextFile(abs)
	if err != nil {
		return b1ReadRefResponse{}, err
	}

	start := req.StartLine
	if start < 1 {
		start = 1
	}
	count := req.LineCount
	if count <= 0 {
		count = s.maxReadLines
	}
	if count > s.maxReadLines {
		count = s.maxReadLines
	}
	end := start + count
	if end > len(lines) {
		end = len(lines) + 1
	}

	hash := sha256.Sum256(text)
	id := "R" + strconv.FormatUint(s.nextRef.Add(1), 10)
	snap := &b1Snapshot{
		ID:           id,
		Path:         req.Path,
		AbsPath:      abs,
		FileHash:     hash,
		FileSize:     int64(len(text)),
		LinesByNumber: make(map[int]b1LineLedger, end-start),
	}
	for i := start; i < end; i++ {
		if i < 1 || i > len(lines) {
			continue
		}
		pl := lines[i-1]
		tok := b1MakeLineToken(i, text[pl.StartByte:pl.ContentEnd])
		ll := b1LineLedger{
			Number:     i,
			Token:      tok,
			StartByte:  pl.StartByte,
			ContentEnd: pl.ContentEnd,
			AfterByte:  pl.AfterByte,
			Content:    string(text[pl.StartByte:pl.ContentEnd]),
		}
		snap.LinesByNumber[i] = ll
	}

	s.mu.Lock()
	s.ledger[id] = snap
	s.mu.Unlock()

	var sb strings.Builder
	fmt.Fprintf(&sb, "@ref %s\n@file %s\n\n", id, req.Path)
	for i := start; i < end; i++ {
		ll, ok := snap.LinesByNumber[i]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "%s|%s\n", ll.Token, string(text[ll.StartByte:ll.ContentEnd]))
	}

	return b1ReadRefResponse{Ref: id, Text: sb.String()}, nil
}

// b1Splice is the public entry point. It validates the snapshot,
// validates each edit, and applies them in original order. On
// success it returns the bounded post-state observation.
func (s *b1Service) b1Splice(req b1SpliceRequest) (b1SpliceResponse, error) {
	return s.b1SpliceWithObs(req)
}

// b1SpliceWithObs is the v3.1 splice entry that always returns
// the post-state observation. b1SpliceNoObs is the v3.0 (B_current)
// variant that returns only "ok".
func (s *b1Service) b1SpliceWithObs(req b1SpliceRequest) (b1SpliceResponse, error) {
	// Validate snapshot ref.
	if !b1SnapshotRefRE.MatchString(req.Ref) {
		return b1SpliceResponse{}, b1Fail(contract.ErrInvalidSnapshotRef,
			"invalid snapshot reference %q; expected a ref from read_ref such as R1; no edits were applied", req.Ref)
	}
	if len(req.Edits) == 0 {
		return b1SpliceResponse{}, b1Fail(contract.ErrEmptyEdits, "splice requires at least one edit; no edits were applied")
	}

	s.mu.RLock()
	snap, ok := s.ledger[req.Ref]
	s.mu.RUnlock()
	if !ok {
		return b1SpliceResponse{}, b1Fail(contract.ErrUnknownSnapshot,
			"unknown snapshot %s; call read_ref again; no edits were applied", req.Ref)
	}

	// Read the current file and check the snapshot hash.
	current, currentLines, err := s.b1ReadTextFile(snap.AbsPath)
	if err != nil {
		return b1SpliceResponse{}, err
	}
	curHash := sha256.Sum256(current)
	if curHash != snap.FileHash {
		return b1SpliceResponse{}, b1Fail(contract.ErrStaleSnapshot,
			"snapshot %s is stale: %s changed after read_ref; no edits were applied", req.Ref, snap.Path)
	}

	// Normalize edits. Each edit must match exactly one of the
	// three shapes (at+old+new, start+end+text, after+text).
	normalized := make([]b1NormalizedEdit, 0, len(req.Edits))
	for i, e := range req.Edits {
		ne, err := s.b1NormalizeEdit(snap, i, e)
		if err != nil {
			return b1SpliceResponse{}, err
		}
		normalized = append(normalized, ne)
	}

	// Validate non-overlap on the original file's byte positions.
	if err := s.b1ValidateNoOverlap(snap, normalized); err != nil {
		return b1SpliceResponse{}, err
	}

	// Apply edits in original order, computing the rewritten file
	// as a list of post-edit windows. Edits are applied left-to-right
	// so the resulting file is deterministic.
	postEdits, err := s.b1ApplyBoundarySemantics(snap, currentLines, normalized)
	if err != nil {
		return b1SpliceResponse{}, err
	}

	// Reassemble the new file content from the windows.
	newContent, err := s.b1AtomicRewrite(snap, current, postEdits)
	if err != nil {
		return b1SpliceResponse{}, err
	}

	// Build the post-state observation from the rewritten file.
	obs, err := s.b1BuildPostState(snap, newContent, postEdits)
	if err != nil {
		return b1SpliceResponse{}, err
	}

	// Refresh the snapshot hash so subsequent splices see the new state.
	newHash := sha256.Sum256(newContent)
	s.mu.Lock()
	snap.FileHash = newHash
	snap.FileSize = int64(len(newContent))
	s.mu.Unlock()

	return b1SpliceResponse{
		OK:         true,
		PostState:  obs,
		NewContent: newContent,
	}, nil
}

// b1SpliceNoObs is the v3.0 (B_current) variant: success returns
// only "ok" with no post-state body.
func (s *b1Service) b1SpliceNoObs(req b1SpliceRequest) (b1SpliceResponse, error) {
	resp, err := s.b1SpliceWithObs(req)
	if err != nil {
		return b1SpliceResponse{}, err
	}
	resp.PostState = ""
	return resp, nil
}

// b1SpliceResponse is the splice success body. PostState is
// non-empty only for the B_fixed arm; NewContent is non-empty
// only for the internal pipeline; OK is always true here.
type b1SpliceResponse struct {
	OK         bool
	PostState  string
	NewContent []byte
}

// b1NormalizeEdit converts a wire b1SpliceEdit into a normalized
// form anchored to byte positions in the original file. The
// normalized form is what the rest of the pipeline operates on.
func (s *b1Service) b1NormalizeEdit(snap *b1Snapshot, index int, e b1SpliceEdit) (b1NormalizedEdit, error) {
	// For at+old+new: at least at and old must be present.
	// new can be empty (deletion of old's content).
	// For start+end+text and after+text: text is required
	// (harness always rewrites complete line ranges).
	hasAt := e.At != ""
	hasOld := e.Old != ""
	hasNew := e.New != ""
	hasStart := e.Start != ""
	hasEnd := e.End != ""
	hasText := e.Text != ""
	hasAfter := e.After != ""

	atOldNew := hasAt && hasOld && !hasStart && !hasEnd && !hasText && !hasAfter
	rangeRepl := !hasAt && !hasOld && !hasNew && hasStart && hasEnd && hasText && !hasAfter
	insertion := !hasAt && !hasOld && !hasNew && !hasStart && !hasEnd && hasAfter && hasText

	if !atOldNew && !rangeRepl && !insertion {
		return b1NormalizedEdit{}, b1Fail(contract.ErrInvalidEditShape,
			"edits[%d]: must match one of at+old+new, start+end+text, after+text; got (%s%s%s%s%s%s%s)",
			index,
			fieldPresent("at", hasAt), fieldPresent("old", hasOld), fieldPresent("new", hasNew),
			fieldPresent("start", hasStart), fieldPresent("end", hasEnd), fieldPresent("text", hasText),
			fieldPresent("after", hasAfter))
	}

	if atOldNew {
		if e.Old == "" {
			return b1NormalizedEdit{}, b1Fail(contract.ErrEmptyOld,
				"edits[%d]: old must be non-empty for at+old+new; no edits were applied", index)
		}
		_ = e.New // New is allowed to be empty (deletion).
		lnum, _, err := s.b1ResolveLineToken(snap, e.At)
		if err != nil {
			return b1NormalizedEdit{}, err
		}
		ll, ok := snap.LinesByNumber[lnum]
		if !ok {
			return b1NormalizedEdit{}, b1Fail(contract.ErrUnknownLineRef,
				"edits[%d]: line %d was not exposed by read_ref %s; call read_ref again",
				index, lnum, snap.ID)
		}
		lineContent := string(snapAbsText(snap)[ll.StartByte:ll.ContentEnd])
		count := strings.Count(lineContent, e.Old)
		if count == 0 {
			return b1NormalizedEdit{}, b1Fail(contract.ErrOldNotFoundInAnchor,
				"edits[%d]: old not found in anchored line L%d; no edits were applied", index, lnum)
		}
		if count > 1 {
			return b1NormalizedEdit{}, b1Fail(contract.ErrOldMultipleMatches,
				"edits[%d]: old matches %d times in anchored line L%d; no edits were applied",
				index, count, lnum)
		}
		// Compute the byte positions of `old` within the line.
		rel := strings.Index(lineContent, e.Old)
		startByte := ll.StartByte + rel
		endByte := startByte + len(e.Old)
		return b1NormalizedEdit{
			Index: index,
			Start: startByte,
			End:   endByte,
			Text:  []byte(e.New),
		}, nil
	}

	if rangeRepl {
		sNum, _, err := s.b1ResolveLineToken(snap, e.Start)
		if err != nil {
			return b1NormalizedEdit{}, err
		}
		eNum, _, err := s.b1ResolveLineToken(snap, e.End)
		if err != nil {
			return b1NormalizedEdit{}, err
		}
		if sNum > eNum {
			return b1NormalizedEdit{}, b1Fail(contract.ErrInvalidLineRef,
				"edits[%d]: start L%d is after end L%d; no edits were applied", index, sNum, eNum)
		}
		sl, ok := snap.LinesByNumber[sNum]
		if !ok {
			return b1NormalizedEdit{}, b1Fail(contract.ErrUnknownLineRef,
				"edits[%d]: start line L%d was not exposed by read_ref %s", index, sNum, snap.ID)
		}
		el, ok := snap.LinesByNumber[eNum]
		if !ok {
			return b1NormalizedEdit{}, b1Fail(contract.ErrUnknownLineRef,
				"edits[%d]: end line L%d was not exposed by read_ref %s", index, eNum, snap.ID)
		}
		// Range replaces complete logical lines, including the
		// trailing line terminator after the end line. We replace
		// [sl.StartByte, el.AfterByte).
		return b1NormalizedEdit{
			Index: index,
			Start: sl.StartByte,
			End:   el.AfterByte,
			Text:  []byte(e.Text),
		}, nil
	}

	// insertion (after+text)
	aNum, _, err := s.b1ResolveLineToken(snap, e.After)
	if err != nil {
		return b1NormalizedEdit{}, err
	}
	al, ok := snap.LinesByNumber[aNum]
	if !ok {
		return b1NormalizedEdit{}, b1Fail(contract.ErrUnknownLineRef,
			"edits[%d]: after line L%d was not exposed by read_ref %s", index, aNum, snap.ID)
	}
	return b1NormalizedEdit{
		Index: index,
		Start: al.AfterByte,
		End:   al.AfterByte,
		Text:  []byte(e.Text),
	}, nil
}

// b1NormalizedEdit is an edit expressed as [start,end) byte
// positions in the original file plus a replacement text.
// start == end means insertion (no bytes removed).
type b1NormalizedEdit struct {
	Index int
	Start int
	End   int
	Text  []byte
}

// b1ResolveLineToken parses an Lnn:hh anchor and verifies the
// line was exposed by the snapshot. It returns the line number
// and the line's hash suffix.
func (s *b1Service) b1ResolveLineToken(snap *b1Snapshot, tok string) (int, string, error) {
	m := b1LineRefRE.FindStringSubmatch(tok)
	if m == nil {
		return 0, "", b1Fail(contract.ErrInvalidLineRef,
			"line reference %q must match L<num>:<hh> where num is a positive integer and hh is two hex digits", tok)
	}
	num, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, "", b1Fail(contract.ErrInvalidLineRef, "line reference %q has invalid number: %v", tok, err)
	}
	if _, ok := snap.LinesByNumber[num]; !ok {
		return num, m[2], b1Fail(contract.ErrUnknownLineRef,
			"line %d was not exposed by read_ref %s; call read_ref again", num, snap.ID)
	}
	return num, m[2], nil
}

// b1ValidateNoOverlap ensures the [start,end) intervals of the
// normalized edits do not overlap. Two insertions at the same
// point are NOT considered overlapping because they are applied
// in original-order; the harness processes them in input order.
// We reject overlapping REPLACEMENT intervals.
func (s *b1Service) b1ValidateNoOverlap(snap *b1Snapshot, edits []b1NormalizedEdit) error {
	for i := 0; i < len(edits); i++ {
		for j := i + 1; j < len(edits); j++ {
			a, b := edits[i], edits[j]
			if a.End <= a.Start || b.End <= b.Start {
				// pure insertion
				continue
			}
			if a.Start == b.Start && a.End == b.End {
				// two edits at exactly the same interval
				return b1Fail(contract.ErrOverlap,
					"edits[%d] and edits[%d] target the same byte range [%d,%d); no edits were applied",
					a.Index, b.Index, a.Start, b.End)
			}
			if a.Start < b.End && b.Start < a.End {
				return b1Fail(contract.ErrOverlap,
					"edits[%d] range [%d,%d) overlaps edits[%d] range [%d,%d); no edits were applied",
					a.Index, a.Start, a.End, b.Index, b.Start, b.End)
			}
		}
	}
	return nil
}

// fieldPresent is a tiny helper for the b1NormalizeEdit error
// message; it formats "name," or "name+..." so the model sees
// which fields it included.
func fieldPresent(name string, present bool) string {
	if !present {
		return ""
	}
	return name + ","
}

// snapAbsText reads the snap's file. The bytes are immutable
// for the lifetime of the snap; the b1 service re-reads on
// each splice to detect staleness.
func snapAbsText(snap *b1Snapshot) []byte {
	b, _ := os.ReadFile(snap.AbsPath)
	return b
}

// regexRE shadows the package-level regexes so that the helper
// file does not have to import regexp explicitly in this file.
var regexRE = struct{ line, snap *regexp.Regexp }{
	line: b1LineRefRE,
	snap: b1SnapshotRefRE,
}

// hexEncode is used by b1MakeLineToken.
func hexEncode(b []byte) string { return hex.EncodeToString(b) }
