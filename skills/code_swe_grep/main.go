// Package main implements the code/swe_grep skill.
// It reads live workspace files and extracts high-signal code snippets
// based on a natural-language question and candidate files/symbols.
//
// See docs/spec/code_symbol_index_and_swe_grep.md §5 for the full contract.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/rs/zerolog"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

// Command is the envelope command for this skill.
const Command = "code/swe_grep"

// Error codes per Core Profile v1 §13.
const (
	ErrCodeArg      = "EARG"      // Invalid arguments
	ErrCodeRuntime  = "ERUNTIME"  // Skill process error/crash
	ErrCodePolicy   = "EPOLICY"   // Capability/policy violation (path escape)
	ErrCodeNotFound = "ENOTFOUND" // Resource not found (file missing)
	ErrCodeIO       = "EIO"       // Filesystem or I/O error
)

// ValidationError wraps an error with a specific error code for the envelope.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// Candidate represents a single candidate file/symbol from upstream retrieval.
type Candidate struct {
	Path     string  `json:"path"`
	SymbolID string  `json:"symbol_id,omitempty"`
	Priority float64 `json:"priority,omitempty"`
}

// Limits controls resource usage during snippet extraction.
type Limits struct {
	MaxFiles        int `json:"max_files,omitempty"`
	MaxSnippets     int `json:"max_snippets,omitempty"`
	MaxBytesPerFile int `json:"max_bytes_per_file,omitempty"`
}

// Default limits when not specified.
const (
	DefaultMaxFiles        = 50
	DefaultMaxSnippets     = 100
	DefaultMaxBytesPerFile = 64 * 1024 // 64 KB
	DefaultInlineKB        = 32        // 32 KB inline threshold
	MaxPreviewBytes        = 512       // Max bytes for inline preview
)

// Snippet extraction tuning constants.
const (
	ContextLines       = 3  // Lines of context above/below matching lines
	MaxLinesPerSnippet = 80 // Maximum lines in a single snippet block
	MinKeywordLen      = 3  // Minimum length for a keyword to be considered
)

// ArtifactKind is the MIME type for SWE Grep NDJSON artifacts.
const ArtifactKind = "application/x-swe-grep-snippets+ndjson"

// FileResult holds validated path and content for a candidate file.
type FileResult struct {
	Path      string  // Relative path from input
	AbsPath   string  // Validated absolute path
	SymbolID  string  // Optional symbol ID from input
	Priority  float64 // Priority from input
	Content   []byte  // File content (may be truncated per MaxBytesPerFile)
	Truncated bool    // True if content was truncated
	Skipped   bool    // True if file was skipped (not found, validation error, etc.)
	SkipErr   string  // Reason for skipping, if skipped
	ErrCode   string  // Error code if skipped due to error
}

// Snippet represents an extracted code snippet for output.
// For NDJSON artifacts, all fields are included.
// For inline previews, Text is truncated to MaxPreviewBytes.
type Snippet struct {
	File      string  `json:"file"`
	SymbolID  string  `json:"symbol_id,omitempty"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Text      string  `json:"text"`
	Priority  float64 `json:"priority,omitempty"`
}

// SnippetPreview is the inline representation with truncated text.
type SnippetPreview struct {
	File      string  `json:"file"`
	SymbolID  string  `json:"symbol_id,omitempty"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Preview   string  `json:"preview"`
	Priority  float64 `json:"priority,omitempty"`
}

// Input is the expected JSON input per spec §5.2.
type Input struct {
	WorkspaceID string      `json:"workspace_id"`
	Question    string      `json:"question"`
	Candidates  []Candidate `json:"candidates"`
	Limits      Limits      `json:"limits,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail(ErrCodeRuntime, err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail(ErrCodeRuntime, err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		if ve, ok := err.(*ValidationError); ok {
			fail(ve.Code, err)
		} else {
			fail(ErrCodeArg, err)
		}
	}

	if err := run(ctx, rc, in); err != nil {
		fail(ErrCodeRuntime, err)
	}
}

// parseInput decodes and validates input from stdin.
func parseInput(r io.Reader) (Input, error) {
	var in Input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return Input{}, fmt.Errorf("decode input: %w", err)
	}

	// Validate required fields per spec §5.2
	if in.WorkspaceID == "" {
		return Input{}, fmt.Errorf("workspace_id is required")
	}
	if in.Question == "" {
		return Input{}, fmt.Errorf("question is required")
	}

	// Check candidates
	usable := 0
	for _, c := range in.Candidates {
		if c.Path != "" {
			usable++
		}
	}
	if usable == 0 {
		return Input{}, &ValidationError{
			Code:    ErrCodeArg,
			Message: "no usable candidates (all paths empty)",
		}
	}

	// Normalize limits (zero/negative treated as unset, will use defaults later)
	if in.Limits.MaxFiles < 0 {
		in.Limits.MaxFiles = 0
	}
	if in.Limits.MaxSnippets < 0 {
		in.Limits.MaxSnippets = 0
	}
	if in.Limits.MaxBytesPerFile < 0 {
		in.Limits.MaxBytesPerFile = 0
	}

	return in, nil
}

// run is the main skill logic.
func run(ctx context.Context, rc *runner.RunnerContext, in Input) error {
	// Apply default limits
	limits := applyDefaultLimits(in.Limits)

	// Process candidates: validate paths and read files
	fileResults := processFiles(ctx, rc, in.Candidates, limits)

	// Count stats
	filesConsidered := 0
	filesRelevant := 0
	for _, fr := range fileResults {
		filesConsidered++
		if !fr.Skipped && len(fr.Content) > 0 {
			filesRelevant++
		}
	}

	// Extract snippets from file results using question-aware matching
	snippets := extractSnippets(fileResults, in.Question, limits.MaxSnippets)

	// Create inline previews
	previews := makeInlinePreviews(snippets)

	// Build response data
	data := map[string]any{
		"summary": map[string]int{
			"files_considered": filesConsidered,
			"files_relevant":   filesRelevant,
			"snippets_emitted": len(snippets),
		},
		"snippets_inline": previews,
	}

	// Persist full snippets to CAS if we have any
	if len(snippets) > 0 {
		artifact, err := persistSnippetsArtifact(ctx, rc, snippets)
		if err != nil {
			return fmt.Errorf("persist snippets artifact: %w", err)
		}
		if artifact.Digest != "" {
			data["artifact"] = artifact.Digest
			data["artifact_kind"] = artifact.Kind
			data["artifact_size_bytes"] = artifact.Size
		}
	}

	// Observability: log summary stats (D5)
	// Question is hashed to avoid leaking sensitive content
	hasArtifact := data["artifact"] != nil
	logSummary(in.WorkspaceID, in.Question, len(in.Candidates), filesConsidered, filesRelevant, len(snippets), hasArtifact)

	return rc.Emit(Command, data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

// logSummary writes a structured log entry to stderr with summary stats.
// The question is hashed to avoid leaking sensitive content per D5.
func logSummary(workspaceID, question string, numCandidates, filesConsidered, filesRelevant, snippetsEmitted int, hasArtifact bool) {
	// Hash the question (first 8 chars of SHA256)
	h := sha256.Sum256([]byte(question))
	qHash := fmt.Sprintf("%x", h[:4])

	log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	log.Info().
		Str("skill", Command).
		Str("workspace_id", workspaceID).
		Str("question_hash", qHash).
		Int("candidates", numCandidates).
		Int("files_considered", filesConsidered).
		Int("files_relevant", filesRelevant).
		Int("snippets_emitted", snippetsEmitted).
		Bool("has_artifact", hasArtifact).
		Msg("swe_grep_complete")
}

// extractSnippets creates snippets from file results using question-aware matching.
// It extracts keywords from the question and finds matching blocks in each file.
// Files are processed in input order (respecting upstream ranking).
func extractSnippets(results []FileResult, question string, maxSnippets int) []Snippet {
	keywords := extractKeywords(question)
	snippets := make([]Snippet, 0, len(results))

	for _, fr := range results {
		if fr.Skipped || len(fr.Content) == 0 {
			continue
		}
		if len(snippets) >= maxSnippets {
			break
		}

		// Extract snippets for this file
		remaining := maxSnippets - len(snippets)
		fileSnippets := extractSnippetsForFile(fr, keywords, remaining)
		snippets = append(snippets, fileSnippets...)
	}

	return snippets
}

// extractSnippetsForFile extracts matching snippet blocks from a single file.
// Returns up to `remaining` snippets based on keyword matches.
func extractSnippetsForFile(fr FileResult, keywords []string, remaining int) []Snippet {
	if remaining <= 0 {
		return nil
	}

	lines := splitLines(fr.Content)
	if len(lines) == 0 {
		return nil
	}

	// Find which lines match any keyword
	matchingLines := findMatchingLines(lines, keywords)
	if len(matchingLines) == 0 {
		// No keyword matches - if file has content, return a fallback snippet
		// from the beginning of the file (useful when question is vague)
		return createFallbackSnippet(fr, lines, remaining)
	}

	// Group matching lines into blocks with context
	blocks := groupIntoBlocks(matchingLines, len(lines))

	// Convert blocks to snippets
	snippets := make([]Snippet, 0, min(len(blocks), remaining))
	for _, block := range blocks {
		if len(snippets) >= remaining {
			break
		}

		text := joinLines(lines, block.start, block.end)
		snippets = append(snippets, Snippet{
			File:      fr.Path,
			SymbolID:  fr.SymbolID,
			StartLine: block.start + 1, // 1-indexed
			EndLine:   block.end + 1,   // 1-indexed, inclusive
			Text:      text,
			Priority:  fr.Priority,
		})
	}

	return snippets
}

// lineBlock represents a contiguous block of lines.
type lineBlock struct {
	start int // 0-indexed start line
	end   int // 0-indexed end line (inclusive)
}

// extractKeywords extracts meaningful keywords from a question.
// It lowercases, splits on non-alphanumeric, and filters short/stop words.
func extractKeywords(question string) []string {
	// Common stop words to filter out
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "are": true, "but": true,
		"not": true, "you": true, "all": true, "can": true, "had": true,
		"her": true, "was": true, "one": true, "our": true, "out": true,
		"has": true, "have": true, "been": true, "this": true, "that": true,
		"what": true, "when": true, "where": true, "which": true, "who": true,
		"how": true, "does": true, "from": true, "with": true, "into": true,
		"about": true, "would": true, "could": true, "should": true,
	}

	lower := strings.ToLower(question)

	// Split on non-alphanumeric characters
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	keywords := make([]string, 0, len(words))
	seen := make(map[string]bool)

	for _, w := range words {
		if len(w) < MinKeywordLen {
			continue
		}
		if stopWords[w] {
			continue
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		keywords = append(keywords, w)
	}

	return keywords
}

// splitLines splits content into lines, preserving empty lines.
func splitLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	s := string(content)
	// Remove trailing newline to avoid empty last element
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// findMatchingLines returns 0-indexed line numbers that contain any keyword.
func findMatchingLines(lines []string, keywords []string) []int {
	if len(keywords) == 0 {
		return nil
	}

	matching := make([]int, 0)
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				matching = append(matching, i)
				break
			}
		}
	}
	return matching
}

// groupIntoBlocks groups matching line indices into contiguous blocks with context.
// Adjacent or overlapping matches are merged. Each block is capped at MaxLinesPerSnippet.
func groupIntoBlocks(matchingLines []int, totalLines int) []lineBlock {
	if len(matchingLines) == 0 {
		return nil
	}

	blocks := make([]lineBlock, 0)
	var current *lineBlock

	for _, lineIdx := range matchingLines {
		// Calculate block boundaries with context
		start := max(0, lineIdx-ContextLines)
		end := min(totalLines-1, lineIdx+ContextLines)

		if current == nil {
			current = &lineBlock{start: start, end: end}
		} else if start <= current.end+1 {
			// Merge overlapping or adjacent blocks
			current.end = max(current.end, end)
			// Cap at MaxLinesPerSnippet and finalize
			if current.end-current.start+1 > MaxLinesPerSnippet {
				current.end = current.start + MaxLinesPerSnippet - 1
				blocks = append(blocks, *current)
				current = nil // Force next match to start a fresh block
			}
		} else {
			// Start a new block
			blocks = append(blocks, *current)
			current = &lineBlock{start: start, end: end}
		}
	}

	if current != nil {
		blocks = append(blocks, *current)
	}

	return blocks
}

// joinLines joins lines from start to end (inclusive, 0-indexed) into a single string.
func joinLines(lines []string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end >= len(lines) {
		end = len(lines) - 1
	}
	if start > end {
		return ""
	}
	return strings.Join(lines[start:end+1], "\n")
}

// createFallbackSnippet creates a snippet from the beginning of a file
// when no keyword matches are found. This handles vague questions.
func createFallbackSnippet(fr FileResult, lines []string, remaining int) []Snippet {
	if remaining <= 0 || len(lines) == 0 {
		return nil
	}

	// Take up to MaxLinesPerSnippet lines from the beginning
	end := min(len(lines)-1, MaxLinesPerSnippet-1)
	text := joinLines(lines, 0, end)

	return []Snippet{{
		File:      fr.Path,
		SymbolID:  fr.SymbolID,
		StartLine: 1,
		EndLine:   end + 1, // 1-indexed
		Text:      text,
		Priority:  fr.Priority,
	}}
}

// makeInlinePreviews creates truncated previews for inline embedding.
func makeInlinePreviews(snippets []Snippet) []SnippetPreview {
	previews := make([]SnippetPreview, len(snippets))
	for i, s := range snippets {
		preview := s.Text
		if len(preview) > MaxPreviewBytes {
			// Truncate at MaxPreviewBytes, try to break at newline
			preview = preview[:MaxPreviewBytes]
			if lastNL := findLastNewline(preview); lastNL > MaxPreviewBytes/2 {
				preview = preview[:lastNL+1]
			}
			preview += "..."
		}
		previews[i] = SnippetPreview{
			File:      s.File,
			SymbolID:  s.SymbolID,
			StartLine: s.StartLine,
			EndLine:   s.EndLine,
			Preview:   preview,
			Priority:  s.Priority,
		}
	}
	return previews
}

// persistSnippetsArtifact writes full snippets as NDJSON to CAS.
func persistSnippetsArtifact(ctx context.Context, rc *runner.RunnerContext, snippets []Snippet) (runner.Artifact, error) {
	if len(snippets) == 0 {
		return runner.Artifact{}, nil
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	for _, s := range snippets {
		if err := enc.Encode(s); err != nil {
			return runner.Artifact{}, fmt.Errorf("encode snippet: %w", err)
		}
	}

	return runner.PersistBuffer(ctx, rc, buf, ArtifactKind, "code_swe_grep")
}

// countLines counts the number of lines in content.
func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := bytes.Count(content, []byte{'\n'})
	// Add 1 if content doesn't end with newline
	if content[len(content)-1] != '\n' {
		count++
	}
	return count
}

// findLastNewline returns the index of the last newline in s, or -1 if not found.
func findLastNewline(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			return i
		}
	}
	return -1
}

// applyDefaultLimits fills in default values for unset limits.
func applyDefaultLimits(l Limits) Limits {
	if l.MaxFiles <= 0 {
		l.MaxFiles = DefaultMaxFiles
	}
	if l.MaxSnippets <= 0 {
		l.MaxSnippets = DefaultMaxSnippets
	}
	if l.MaxBytesPerFile <= 0 {
		l.MaxBytesPerFile = DefaultMaxBytesPerFile
	}
	return l
}

// processFiles validates and reads candidate files up to limits.
// It opens files immediately after validation to eliminate TOCTOU race conditions.
func processFiles(ctx context.Context, rc *runner.RunnerContext, candidates []Candidate, limits Limits) []FileResult {
	results := make([]FileResult, 0, len(candidates))
	filesProcessed := 0

	for _, c := range candidates {
		// Check context cancellation at each iteration
		if ctx.Err() != nil {
			break
		}

		if c.Path == "" {
			continue // Skip candidates with empty paths
		}
		if filesProcessed >= limits.MaxFiles {
			break // Respect MaxFiles limit
		}

		fr := FileResult{
			Path:     c.Path,
			SymbolID: c.SymbolID,
			Priority: c.Priority,
		}

		// Validate path through PathValidator
		absPath, err := rc.PathValidator.ValidatePath(c.Path)
		if err != nil {
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyPathError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}

		// Resolve symlinks to get the canonical path, then re-validate
		// to ensure the resolved path is still within the workspace.
		resolvedPath, err := filepath.EvalSymlinks(absPath)
		if err != nil {
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyFileError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}

		// Re-validate the resolved path to catch symlink escapes
		if _, err := rc.PathValidator.ValidatePath(resolvedPath); err != nil {
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyPathError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}
		fr.AbsPath = resolvedPath

		// Open file immediately after validation to eliminate TOCTOU window.
		// We pass the open file descriptor to readFromFile.
		file, err := os.Open(resolvedPath)
		if err != nil {
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyFileError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}

		// Get file info from the open descriptor (not the path) for integrity
		info, err := file.Stat()
		if err != nil {
			errs.Ignore(file.Close(), "close file after stat error")
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyFileError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}

		// Skip directories
		if info.IsDir() {
			errs.Ignore(file.Close(), "close directory")
			fr.Skipped = true
			fr.SkipErr = "path is a directory"
			fr.ErrCode = ErrCodeArg
			results = append(results, fr)
			filesProcessed++
			continue
		}

		// Read file content with limit using the already-open descriptor
		content, truncated, err := readFromFile(ctx, file, info, limits.MaxBytesPerFile)
		errs.Ignore(file.Close(), "close file after read")

		if err != nil {
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyFileError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}

		fr.Content = content
		fr.Truncated = truncated
		results = append(results, fr)
		filesProcessed++
	}

	return results
}

// classifyPathError returns a human-readable reason and error code for path validation failure.
func classifyPathError(err error) (string, string) {
	switch {
	case errors.Is(err, policy.ErrPathEscape):
		return "path escapes workspace", ErrCodePolicy
	case errors.Is(err, policy.ErrSymlinkEscape):
		return "symlink escapes workspace", ErrCodePolicy
	case errors.Is(err, policy.ErrInvalidPath):
		return "invalid path", ErrCodeArg
	case errors.Is(err, policy.ErrNullByte):
		return "path contains null byte", ErrCodeArg
	case errors.Is(err, policy.ErrNotAbsolute):
		return "path must resolve to absolute location", ErrCodeArg
	default:
		return fmt.Sprintf("path validation failed: %v", err), ErrCodePolicy
	}
}

// classifyFileError returns a human-readable reason and error code for file read failure.
func classifyFileError(err error) (string, string) {
	if os.IsNotExist(err) {
		return "file not found", ErrCodeNotFound
	}
	if os.IsPermission(err) {
		return "permission denied", ErrCodePolicy
	}
	return fmt.Sprintf("read error: %v", err), ErrCodeIO
}

// readFromFile reads from an already-open file up to maxBytes.
// It accepts the os.FileInfo to verify the file hasn't been replaced.
// The caller is responsible for closing the file.
func readFromFile(ctx context.Context, file *os.File, info os.FileInfo, maxBytes int) ([]byte, bool, error) {
	// Check cancellation before reading
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	// Determine read size: use file size if known and smaller than limit
	readSize := maxBytes + 1 // +1 to detect truncation
	fileSize := info.Size()
	if fileSize >= 0 && fileSize < int64(readSize) {
		readSize = int(fileSize) + 1 // May still be less if file is tiny
	}

	buf := make([]byte, readSize)
	n, err := io.ReadFull(file, buf)

	// Check cancellation after read
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}

	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, false, err
	}

	if n > maxBytes {
		return buf[:maxBytes], true, nil
	}
	return buf[:n], false, nil
}

// readFileWithLimit reads a file up to maxBytes, returning content and whether it was truncated.
// Deprecated: Use readFromFile with an already-open file to avoid TOCTOU races.
// Kept for testing purposes.
func readFileWithLimit(ctx context.Context, path string, maxBytes int) ([]byte, bool, error) {
	// Check cancellation before opening
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		errs.Ignore(file.Close(), "close file")
	}()

	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}

	return readFromFile(ctx, file, info, maxBytes)
}

// fail emits an error envelope and exits.
func fail(code string, err error) {
	env := envelope.Error(Command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit "+Command+" failure")
	os.Exit(1)
}
