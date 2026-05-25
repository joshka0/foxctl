package pathutil

import (
	"os"
	"path/filepath"
	"strings"

	platformpath "github.com/joshka0/foxctl/internal/platform/pathutil"
	"github.com/joshka0/foxctl/internal/platform/workspace"
)

// Re-export commonly used functions from internal/platform/pathutil
var (
	// ExtractPath extracts the file path from tool input JSON.
	ExtractPath = platformpath.ExtractPath

	// DecodeToolPathInput decodes a tool payload into the typed path input model.
	DecodeToolPathInput = platformpath.DecodeToolPathInput

	// ExtractPathFromInput extracts the first path from a typed path input.
	ExtractPathFromInput = platformpath.ExtractPathFromInput

	// ExtractPaths extracts all file paths from tool input JSON.
	ExtractPaths = platformpath.ExtractPaths

	// ExtractPathsFromInput extracts all paths from a typed path input.
	ExtractPathsFromInput = platformpath.ExtractPathsFromInput

	// NormalizePath normalizes a file path relative to workspace.
	NormalizePath = platformpath.NormalizePath

	// RelativePath returns the path relative to workspace.
	RelativePath = platformpath.RelativePath

	// IsUnderWorkspace checks if a path is under the workspace root.
	IsUnderWorkspace = platformpath.IsUnderWorkspace

	// Extension returns the file extension without the dot.
	Extension = platformpath.Extension

	// IsTestFile returns true if the path appears to be a test file.
	IsTestFile = platformpath.IsTestFile

	// PathFields are the field names to check for file paths.
	PathFields = platformpath.PathFields
)

type ToolPathInput = platformpath.ToolPathInput

// ResolveSearchPath resolves a search path candidate to an absolute path.
// It handles empty candidates by defaulting to workspace, and validates
// that the path exists and is under the workspace.
//
// Returns:
//   - workspace: the resolved workspace root
//   - searchPath: the resolved absolute search path
//   - error: if the path is invalid or outside workspace
func ResolveSearchPath(workspaceRoot, candidate string) (string, string, error) {
	// Resolve workspace if not provided
	if workspaceRoot == "" {
		workspaceRoot = workspace.Detect("")
	}
	if workspaceRoot == "" {
		var err error
		workspaceRoot, err = os.Getwd()
		if err != nil {
			return "", "", err
		}
	}
	workspaceRoot = filepath.Clean(workspaceRoot)

	// Default to workspace if no candidate
	if candidate == "" {
		return workspaceRoot, workspaceRoot, nil
	}

	// Resolve candidate to absolute path
	var searchPath string
	if filepath.IsAbs(candidate) {
		searchPath = filepath.Clean(candidate)
	} else {
		searchPath = filepath.Clean(filepath.Join(workspaceRoot, candidate))
	}

	// Verify path is under workspace
	if !IsUnderWorkspace(searchPath, workspaceRoot) {
		return workspaceRoot, "", &PathError{
			Op:        "resolve",
			Path:      candidate,
			Workspace: workspaceRoot,
			Err:       ErrOutsideWorkspace,
		}
	}

	return workspaceRoot, searchPath, nil
}

// RelTo returns the path relative to the base directory.
// If the path cannot be made relative, returns the original path.
// Uses forward slashes for consistency across platforms.
func RelTo(base, target string) string {
	if base == "" {
		base = "."
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	// If the relative path escapes the base, return absolute.
	if isParentRelativePath(rel) {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

// ToSlash converts a path to forward slashes.
// Useful for consistent output across platforms.
func ToSlash(path string) string {
	return filepath.ToSlash(path)
}

// JoinAndClean joins path elements and cleans the result.
func JoinAndClean(elem ...string) string {
	return filepath.Clean(filepath.Join(elem...))
}

// IsHidden returns true if the path or any of its components is hidden (starts with .).
func IsHidden(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part != "" && part != "." && part != ".." && strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// IsDotfile returns true if the file itself (not parent directories) is hidden.
func IsDotfile(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, ".") && base != "." && base != ".."
}

// CommonPrefix returns the longest common directory prefix of the given paths.
// Returns empty string if paths have no common prefix.
func CommonPrefix(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0])
	}

	// Split first path into components
	first := filepath.ToSlash(filepath.Clean(paths[0]))
	parts := strings.Split(first, "/")

	for _, path := range paths[1:] {
		path = filepath.ToSlash(filepath.Clean(path))
		otherParts := strings.Split(path, "/")

		// Find common prefix length
		commonLen := 0
		for i := 0; i < len(parts) && i < len(otherParts); i++ {
			if parts[i] == otherParts[i] {
				commonLen = i + 1
			} else {
				break
			}
		}
		parts = parts[:commonLen]
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, string(filepath.Separator))
}

func isParentRelativePath(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
