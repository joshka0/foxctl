package lsp

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolvePath resolves path to an absolute path inside workspace.
func ResolvePath(workspace, path string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	path = strings.TrimSpace(path)
	if workspace == "" {
		return "", fmt.Errorf("workspace is required")
	}
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	root, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	root = filepath.Clean(root)

	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate = filepath.Clean(candidate)
	if !isWithinWorkspace(candidate, root) {
		return "", fmt.Errorf("path %q escapes workspace %q", path, workspace)
	}
	return candidate, nil
}

// URIToPath converts a file:// URI into a workspace-relative path when possible.
func URIToPath(uri, workspace string) string {
	path := strings.TrimSpace(strings.TrimPrefix(uri, "file://"))
	if path == "" || !filepath.IsAbs(path) {
		return path
	}
	path = filepath.Clean(path)

	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return path
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return path
	}
	root = filepath.Clean(root)
	if !isWithinWorkspace(path, root) {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func isWithinWorkspace(path, workspace string) bool {
	rel, err := filepath.Rel(workspace, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
