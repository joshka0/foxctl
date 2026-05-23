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

// ToolPathInput captures the path-bearing fields supported by hook tool inputs.
type ToolPathInput struct {
	FilePath    string
	Path        string
	File        string
	CurrentPath string
	Edits       []ToolPathInput
	Files       []string
}

// UnmarshalJSON accepts partially-typed hook payloads while keeping the
// extracted path shape explicit inside the package.
func (in *ToolPathInput) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	in.FilePath = stringField(fields, "file_path")
	in.Path = stringField(fields, "path")
	in.File = stringField(fields, "file")
	in.CurrentPath = stringField(fields, "current_path")
	in.Edits = decodeEdits(fields["edits"])
	in.Files = decodeFiles(fields["files"])
	return nil
}

// ExtractPath extracts the file path from tool input JSON.
// It tries PathFields in order and returns the first non-empty value.
// Returns empty string if no path is found.
func ExtractPath(toolInput json.RawMessage) string {
	input, ok := DecodeToolPathInput(toolInput)
	if !ok {
		return ""
	}
	return ExtractPathFromInput(input)
}

// ExtractPathFromInput extracts the first path from a typed path input.
func ExtractPathFromInput(input ToolPathInput) string {
	for _, key := range PathFields {
		if path := input.pathValue(key); path != "" {
			return path
		}
	}
	return ""
}

// ExtractPaths extracts all file paths from tool input JSON.
// Some tools (like MultiEdit) may have multiple paths.
// Returns nil if no paths are found.
func ExtractPaths(toolInput json.RawMessage) []string {
	input, ok := DecodeToolPathInput(toolInput)
	if !ok {
		return nil
	}
	return ExtractPathsFromInput(input)
}

// ExtractPathsFromInput extracts all paths from a typed path input.
func ExtractPathsFromInput(input ToolPathInput) []string {
	var paths []string

	for _, edit := range input.Edits {
		if p := ExtractPathFromInput(edit); p != "" {
			paths = append(paths, p)
		}
	}

	for _, file := range input.Files {
		if file != "" {
			paths = append(paths, file)
		}
	}

	if p := ExtractPathFromInput(input); p != "" {
		paths = append(paths, p)
	}

	return uniquePaths(paths)
}

// DecodeToolPathInput decodes a tool payload into the typed path input model.
func DecodeToolPathInput(toolInput json.RawMessage) (ToolPathInput, bool) {
	if len(toolInput) == 0 {
		return ToolPathInput{}, false
	}

	var input ToolPathInput
	if err := json.Unmarshal(toolInput, &input); err != nil {
		return ToolPathInput{}, false
	}
	return input, true
}

func (in ToolPathInput) pathValue(key string) string {
	switch key {
	case "file_path":
		return in.FilePath
	case "path":
		return in.Path
	case "file":
		return in.File
	case "current_path":
		return in.CurrentPath
	default:
		return ""
	}
}

func stringField(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func decodeEdits(raw json.RawMessage) []ToolPathInput {
	if len(raw) == 0 {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	edits := make([]ToolPathInput, 0, len(entries))
	for _, entry := range entries {
		var edit ToolPathInput
		if err := json.Unmarshal(entry, &edit); err == nil {
			if ExtractPathFromInput(edit) != "" || len(edit.Edits) > 0 || len(edit.Files) > 0 {
				edits = append(edits, edit)
			}
		}
	}
	return edits
}

func decodeFiles(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		var file string
		if err := json.Unmarshal(entry, &file); err == nil && file != "" {
			files = append(files, file)
		}
	}
	return files
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
