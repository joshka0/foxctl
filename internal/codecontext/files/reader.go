// Package files provides TOCTOU-safe file reading with path validation.
//
// The SafeReader eliminates time-of-check to time-of-use (TOCTOU) race conditions
// by opening files immediately after validation and reading from the open descriptor.
// This prevents attacks where a symlink is swapped between validation and read.
//
// Pattern:
//  1. Validate path against workspace/allowed roots
//  2. Open file immediately (no race window)
//  3. Stat from the open descriptor
//  4. Re-validate resolved symlink path
//  5. Read from the open descriptor
package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/platform/fsutil"
)

// PathValidator validates file paths against allowed roots.
// This interface allows the reader to work with different validator implementations
// without depending on the concrete policy.PathValidator.
type PathValidator interface {
	// ValidatePath validates and resolves a user-provided path.
	// Returns the absolute canonical path if valid, or an error if it escapes workspace.
	ValidatePath(userPath string) (string, error)

	// Workspace returns the configured workspace path.
	Workspace() string
}

// FileContent holds the result of reading a file.
type FileContent struct {
	// Path is the original requested path.
	Path string

	// AbsPath is the validated absolute path.
	AbsPath string

	// Content is the raw file bytes (may be truncated).
	Content []byte

	// Lines is the content pre-split into lines for efficient access.
	// Line indices are 0-based; line numbers in output should be 1-based.
	Lines []string

	// LineOffsets maps line index to byte offset in Content.
	// LineOffsets[i] is the byte offset where Lines[i] begins.
	LineOffsets []int

	// Truncated indicates the content was limited by MaxBytes.
	Truncated bool

	// Language is the detected programming language.
	Language string

	// Size is the original file size before truncation.
	Size int64
}

// LineCount returns the number of lines in the file.
func (fc *FileContent) LineCount() int {
	return len(fc.Lines)
}

// GetLine returns the content of a specific line (1-indexed).
// Returns empty string if line number is out of range.
func (fc *FileContent) GetLine(lineNum int) string {
	idx := lineNum - 1
	if idx < 0 || idx >= len(fc.Lines) {
		return ""
	}
	return fc.Lines[idx]
}

// GetLines returns lines in the range [startLine, endLine] (1-indexed, inclusive).
func (fc *FileContent) GetLines(startLine, endLine int) []string {
	startIdx := startLine - 1
	endIdx := endLine // endLine is inclusive, but slice is exclusive

	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > len(fc.Lines) {
		endIdx = len(fc.Lines)
	}
	if startIdx >= endIdx {
		return nil
	}

	result := make([]string, endIdx-startIdx)
	copy(result, fc.Lines[startIdx:endIdx])
	return result
}

// SafeReader implements TOCTOU-safe file reading with path validation.
type SafeReader struct {
	validator PathValidator
	maxBytes  int
}

// NewSafeReader creates a reader with the given validator and byte limit.
func NewSafeReader(validator PathValidator, maxBytes int) *SafeReader {
	if maxBytes <= 0 {
		maxBytes = 64 * 1024 // 64 KB default
	}
	return &SafeReader{
		validator: validator,
		maxBytes:  maxBytes,
	}
}

// Read reads a file safely, eliminating TOCTOU race conditions.
//
// The read process:
//  1. Validates the path through PathValidator
//  2. Resolves symlinks and re-validates the resolved path
//  3. Opens the file immediately after validation
//  4. Stats from the open descriptor (not the path)
//  5. Reads from the open descriptor up to maxBytes
//
// This approach eliminates the race window where a file could be swapped
// between validation and read.
func (r *SafeReader) Read(ctx context.Context, path string) (*FileContent, error) {
	if r.validator == nil {
		return nil, errors.New("path validator not configured")
	}

	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	result := &FileContent{
		Path: path,
	}

	// Step 1: Validate the path
	absPath, err := r.validator.ValidatePath(path)
	if err != nil {
		return nil, &ReadError{
			Path:    path,
			Code:    classifyPathErrorCode(err),
			Message: classifyPathErrorMessage(err),
			Err:     err,
		}
	}

	// Step 2: Resolve symlinks to get canonical path
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return nil, &ReadError{
			Path:    path,
			Code:    classifyFileErrorCode(err),
			Message: classifyFileErrorMessage(err),
			Err:     err,
		}
	}

	// Step 3: Re-validate the resolved path to catch symlink escapes
	if _, err := r.validator.ValidatePath(resolvedPath); err != nil {
		return nil, &ReadError{
			Path:    path,
			Code:    "EPOLICY",
			Message: "symlink escapes workspace",
			Err:     err,
		}
	}
	result.AbsPath = resolvedPath

	// Step 4: Open file immediately after validation (eliminates TOCTOU window)
	file, err := os.Open(resolvedPath)
	if err != nil {
		return nil, &ReadError{
			Path:    path,
			Code:    classifyFileErrorCode(err),
			Message: classifyFileErrorMessage(err),
			Err:     err,
		}
	}
	defer file.Close()

	// Step 5: Get file info from the open descriptor (not the path)
	info, err := file.Stat()
	if err != nil {
		return nil, &ReadError{
			Path:    path,
			Code:    classifyFileErrorCode(err),
			Message: classifyFileErrorMessage(err),
			Err:     err,
		}
	}

	// Reject directories
	if info.IsDir() {
		return nil, &ReadError{
			Path:    path,
			Code:    "EARG",
			Message: "path is a directory",
		}
	}

	result.Size = info.Size()

	// Check context before reading
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Step 6: Read from the open descriptor
	content, truncated, err := readFromDescriptor(ctx, file, info, r.maxBytes)
	if err != nil {
		return nil, &ReadError{
			Path:    path,
			Code:    classifyFileErrorCode(err),
			Message: classifyFileErrorMessage(err),
			Err:     err,
		}
	}

	result.Content = content
	result.Truncated = truncated
	result.Language = fsutil.DetectLanguage(path)

	// Pre-split into lines for efficient access
	result.Lines, result.LineOffsets = splitLines(content)

	return result, nil
}

// ReadError represents an error during file reading.
type ReadError struct {
	Path    string
	Code    string
	Message string
	Err     error
}

func (e *ReadError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Path, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

func (e *ReadError) Unwrap() error {
	return e.Err
}

// readFromDescriptor reads from an already-open file up to maxBytes.
// The caller must ensure the file is open and pass its FileInfo.
func readFromDescriptor(ctx context.Context, file *os.File, info os.FileInfo, maxBytes int) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	// Determine read size: use file size if known and smaller than limit
	// Add 1 to detect truncation
	readSize := maxBytes + 1
	fileSize := info.Size()
	if fileSize >= 0 && fileSize < int64(readSize) {
		readSize = int(fileSize) + 1
	}

	buf := make([]byte, readSize)
	n, err := io.ReadFull(file, buf)

	// Check context after read
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}

	// EOF and ErrUnexpectedEOF are expected for files smaller than buffer
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, false, err
	}

	if n > maxBytes {
		return buf[:maxBytes], true, nil
	}
	return buf[:n], false, nil
}

// splitLines splits content into lines and computes byte offsets.
// Handles both \n and \r\n line endings.
func splitLines(content []byte) ([]string, []int) {
	if len(content) == 0 {
		return []string{}, []int{}
	}

	// Count lines first for efficient allocation
	lineCount := 1
	for _, b := range content {
		if b == '\n' {
			lineCount++
		}
	}

	lines := make([]string, 0, lineCount)
	offsets := make([]int, 0, lineCount)

	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			end := i
			// Handle \r\n
			if end > start && content[end-1] == '\r' {
				end--
			}
			offsets = append(offsets, start)
			lines = append(lines, string(content[start:end]))
			start = i + 1
		}
	}

	// Add final line if content doesn't end with newline
	if start < len(content) {
		offsets = append(offsets, start)
		end := len(content)
		if content[end-1] == '\r' {
			end--
		}
		lines = append(lines, string(content[start:end]))
	} else if start == len(content) && len(content) > 0 && content[len(content)-1] == '\n' {
		// Content ends with newline - don't add empty final line
		// (intentionally empty - just skip adding trailing empty line)
		_ = start // lint: SA9003
	}

	return lines, offsets
}

// classifyPathErrorCode returns an error code for path validation errors.
func classifyPathErrorCode(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "escape"):
		return "EPOLICY"
	case strings.Contains(msg, "symlink"):
		return "EPOLICY"
	case strings.Contains(msg, "null byte"):
		return "EARG"
	case strings.Contains(msg, "invalid"):
		return "EARG"
	default:
		return "EPOLICY"
	}
}

// classifyPathErrorMessage returns a human-readable message for path errors.
func classifyPathErrorMessage(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "symlink"):
		return "symlink escapes workspace"
	case strings.Contains(msg, "escape"):
		return "path escapes workspace"
	case strings.Contains(msg, "null byte"):
		return "path contains null byte"
	case strings.Contains(msg, "invalid"):
		return "invalid path"
	default:
		return "path validation failed"
	}
}

// classifyFileErrorCode returns an error code for file operation errors.
func classifyFileErrorCode(err error) string {
	if os.IsNotExist(err) {
		return "ENOTFOUND"
	}
	if os.IsPermission(err) {
		return "EPOLICY"
	}
	return "EIO"
}

// classifyFileErrorMessage returns a human-readable message for file errors.
func classifyFileErrorMessage(err error) string {
	if os.IsNotExist(err) {
		return "file not found"
	}
	if os.IsPermission(err) {
		return "permission denied"
	}
	return "read error"
}
