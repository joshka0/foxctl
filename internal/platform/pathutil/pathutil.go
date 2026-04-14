package pathutil

import (
	"encoding/json"
	"path/filepath"
	"strings"

	platformfs "github.com/joshka0/foxctl/internal/platform/fsutil"
)

// PathFields are the field names to check for file paths, in order of preference.
var PathFields = []string{
	"file_path",    // CC Edit/Write standard
	"path",         // foxctl canonical
	"file",         // alternative
	"current_path", // OC/alternative
}

// ExtractPath extracts the file path from tool input JSON.
// It tries PathFields in order and returns the first non-empty value.
// Returns empty string if no path is found.
func ExtractPath(toolInput json.RawMessage) string {
	if len(toolInput) == 0 {
		return ""
	}

	var input map[string]any
	if err := json.Unmarshal(toolInput, &input); err != nil {
		return ""
	}

	return ExtractPathFromMap(input)
}

// ExtractPathFromMap extracts the file path from a map.
// Useful when the input is already parsed.
func ExtractPathFromMap(input map[string]any) string {
	for _, key := range PathFields {
		if v, ok := input[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// ExtractPaths extracts all file paths from tool input JSON.
// Some tools (like MultiEdit) may have multiple paths.
// Returns nil if no paths are found.
func ExtractPaths(toolInput json.RawMessage) []string {
	if len(toolInput) == 0 {
		return nil
	}

	var input map[string]any
	if err := json.Unmarshal(toolInput, &input); err != nil {
		return nil
	}

	var paths []string

	// Check for array of edits (MultiEdit pattern)
	if edits, ok := input["edits"].([]any); ok {
		for _, edit := range edits {
			if editMap, ok := edit.(map[string]any); ok {
				if p := ExtractPathFromMap(editMap); p != "" {
					paths = append(paths, p)
				}
			}
		}
	}

	// Check for array of files
	if files, ok := input["files"].([]any); ok {
		for _, file := range files {
			if s, ok := file.(string); ok && s != "" {
				paths = append(paths, s)
			}
		}
	}

	// Check single path fields
	if p := ExtractPathFromMap(input); p != "" {
		paths = append(paths, p)
	}

	// Deduplicate
	return uniquePaths(paths)
}

// uniquePaths removes duplicate paths while preserving order.
func uniquePaths(paths []string) []string {
	if len(paths) <= 1 {
		return paths
	}

	seen := make(map[string]bool)
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	return result
}

// NormalizePath normalizes a file path:
// - Makes relative paths relative to workspace
// - Resolves .. and . components
// - Preserves absolute paths
func NormalizePath(path, workspaceRoot string) string {
	if path == "" {
		return ""
	}

	// If already absolute, clean it
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	// If workspace is provided, make relative to it
	if workspaceRoot != "" {
		return filepath.Clean(filepath.Join(workspaceRoot, path))
	}

	// Just clean the relative path
	return filepath.Clean(path)
}

// RelativePath returns the path relative to workspace.
// If the path is not under workspace, returns the original path.
func RelativePath(path, workspaceRoot string) string {
	if path == "" || workspaceRoot == "" {
		return path
	}

	// Clean both paths
	path = filepath.Clean(path)
	workspaceRoot = filepath.Clean(workspaceRoot)

	// Try to make relative
	rel, err := filepath.Rel(workspaceRoot, path)
	if err != nil {
		return path
	}

	// If relative path starts with .., it's not under workspace
	if strings.HasPrefix(rel, "..") {
		return path
	}

	return rel
}

// IsUnderWorkspace checks if a path is under the workspace root.
func IsUnderWorkspace(path, workspaceRoot string) bool {
	if path == "" || workspaceRoot == "" {
		return false
	}

	// Clean and make absolute if needed
	path = filepath.Clean(path)
	workspaceRoot = filepath.Clean(workspaceRoot)

	// Check if path starts with workspace root
	rel, err := filepath.Rel(workspaceRoot, path)
	if err != nil {
		return false
	}

	return !strings.HasPrefix(rel, "..")
}

// Extension returns the file extension without the dot.
// Returns empty string for files without extension.
func Extension(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return ""
	}
	return ext[1:] // Remove leading dot
}

// IsTestFile returns true if the path appears to be a test file.
func IsTestFile(path string) bool {
	return platformfs.IsTestFile(filepath.Base(path))
}
