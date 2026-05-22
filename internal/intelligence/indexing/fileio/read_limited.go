package fileio

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DefaultReadLimit is the maximum file size indexers read from disk.
const DefaultReadLimit int64 = 10 * 1024 * 1024

// ReadLimited reads a regular file under workspaceRoot after resolving
// symlinks, rejecting path traversal and files larger than maxBytes.
func ReadLimited(workspaceRoot, path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("max read size must be positive")
	}

	cleanPath := filepath.Clean(path)
	if filepath.IsAbs(cleanPath) {
		return nil, fmt.Errorf("absolute paths not allowed: %s", path)
	}
	if cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path traversal not allowed: %s", path)
	}

	fullPath := filepath.Join(workspaceRoot, cleanPath)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}

	evalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("resolve symlinks for path: %w", err)
		}
		evalPath = absPath
	}
	evalWorkspace, err := filepath.EvalSymlinks(absWorkspace)
	if err != nil {
		return nil, fmt.Errorf("resolve symlinks for workspace: %w", err)
	}

	rel, err := filepath.Rel(evalWorkspace, evalPath)
	if err != nil {
		return nil, fmt.Errorf("relativize path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("path escapes workspace: %s", path)
	}

	info, err := os.Stat(evalPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file too large (%d bytes, max %d): %s", info.Size(), maxBytes, path)
	}

	file, err := os.Open(evalPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("file too large (exceeded max %d): %s", maxBytes, path)
	}
	return content, nil
}
