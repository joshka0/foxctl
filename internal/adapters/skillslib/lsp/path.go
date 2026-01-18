package lsp

import (
	"path/filepath"
	"strings"
)

// ResolvePath ensures a relative path is resolved within the workspace.
func ResolvePath(workspace, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workspace, path)
}

// URIToPath converts a file:// URI into a workspace-relative path when possible.
func URIToPath(uri, workspace string) string {
	path := strings.TrimPrefix(uri, "file://")
	rel, err := filepath.Rel(workspace, path)
	if err != nil {
		return path
	}
	return rel
}
