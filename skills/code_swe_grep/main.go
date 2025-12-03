// Package main implements the code/swe_grep skill.
// It reads live workspace files and extracts high-signal code snippets
// based on a natural-language question and candidate files/symbols.
//
// See docs/spec/code_symbol_index_and_swe_grep.md §5 for the full contract.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
)

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

	// TODO(phase5-pr5): implement snippet extraction from file contents
	// For now, emit summary with files processed but no snippets
	data := map[string]any{
		"summary": map[string]int{
			"files_considered": filesConsidered,
			"files_relevant":   filesRelevant,
			"snippets_emitted": 0,
		},
		"snippets_inline": []any{},
	}

	return rc.Emit(Command, data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
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
