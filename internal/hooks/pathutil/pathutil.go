// Package pathutil provides cross-platform path extraction utilities for hook skills.
// It re-exports the canonical implementations from internal/platform/pathutil.
package pathutil

import (
	"encoding/json"

	platformpath "github.com/jkatigb/agentctl/internal/platform/pathutil"
)

// PathFields are the field names to check for file paths, in order of preference.
var PathFields = platformpath.PathFields

// ExtractPath extracts the file path from tool input JSON.
func ExtractPath(toolInput json.RawMessage) string {
	return platformpath.ExtractPath(toolInput)
}

// ExtractPathFromMap extracts the file path from a map.
func ExtractPathFromMap(input map[string]any) string {
	return platformpath.ExtractPathFromMap(input)
}

// ExtractPaths extracts all file paths from tool input JSON.
func ExtractPaths(toolInput json.RawMessage) []string {
	return platformpath.ExtractPaths(toolInput)
}

// NormalizePath normalizes a file path relative to workspace.
func NormalizePath(path, workspaceRoot string) string {
	return platformpath.NormalizePath(path, workspaceRoot)
}

// RelativePath returns the path relative to workspace.
func RelativePath(path, workspaceRoot string) string {
	return platformpath.RelativePath(path, workspaceRoot)
}

// IsUnderWorkspace checks if a path is under the workspace root.
func IsUnderWorkspace(path, workspaceRoot string) bool {
	return platformpath.IsUnderWorkspace(path, workspaceRoot)
}

// Extension returns the file extension without the dot.
func Extension(path string) string {
	return platformpath.Extension(path)
}

// IsTestFile returns true if the path appears to be a test file.
func IsTestFile(path string) bool {
	return platformpath.IsTestFile(path)
}
