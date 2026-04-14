package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/joshka0/foxctl/internal/platform/maputil"
	tooling "github.com/joshka0/foxctl/internal/tooling"
)

// StructuredDiff represents a diff output from code/diff skill.
// Matches the format in skills/code_diff/main.go.
type StructuredDiff struct {
	OldFile    string     `json:"old_file"`
	NewFile    string     `json:"new_file"`
	Statistics DiffStats  `json:"statistics"`
	Hunks      []DiffHunk `json:"hunks"`
	Unified    string     `json:"unified,omitempty"`
}

// DiffStats contains diff statistics.
type DiffStats struct {
	LinesAdded   int     `json:"lines_added"`
	LinesRemoved int     `json:"lines_removed"`
	LinesChanged int     `json:"lines_changed"`
	TotalChanges int     `json:"total_changes"`
	Similarity   float64 `json:"similarity_percent"`
}

// DiffHunk represents a single hunk in a diff.
type DiffHunk struct {
	OldStart int      `json:"old_start"`
	OldLines int      `json:"old_lines"`
	NewStart int      `json:"new_start"`
	NewLines int      `json:"new_lines"`
	Header   string   `json:"header"`
	Lines    []string `json:"lines"`
}

// registerEditTools registers file editing tools.
func (r *Registry) registerEditTools() error {
	// edit.apply_patch - apply a unified diff patch to a file
	patchTool := tooling.NewFuncTool(
		"edit.apply_patch",
		"Apply a text replacement to a file. Replaces old_text with new_text.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"path": {
					Type:        "string",
					Description: "Path to the file to edit (relative to workspace)",
					Required:    true,
				},
				"old_text": {
					Type:        "string",
					Description: "The exact text to find and replace",
					Required:    true,
				},
				"new_text": {
					Type:        "string",
					Description: "The text to replace old_text with",
					Required:    true,
				},
			},
		},
		r.wrapWithTelemetry("edit.apply_patch", r.applyPatch),
	)
	if err := r.tools.Register(patchTool); err != nil {
		return fmt.Errorf("register edit.apply_patch: %w", err)
	}

	// edit.apply_structured_diff - apply a structured diff from code/diff
	structuredDiffTool := tooling.NewFuncTool(
		"edit.apply_structured_diff",
		"Apply a structured diff (from code/diff skill) to transform a file. Accepts diff_json with hunks[] containing line-based changes.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"path": {
					Type:        "string",
					Description: "Path to the file to edit (relative to workspace)",
					Required:    true,
				},
				"diff_json": {
					Type:        "object",
					Description: "Structured diff object with hunks[] from code/diff",
					Required:    true,
				},
				"dry_run": {
					Type:        "boolean",
					Description: "If true, validate the diff without applying (default false)",
				},
			},
		},
		r.wrapWithTelemetry("edit.apply_structured_diff", r.applyStructuredDiff),
	)
	if err := r.tools.Register(structuredDiffTool); err != nil {
		return fmt.Errorf("register edit.apply_structured_diff: %w", err)
	}

	// edit.create_file - create a new file
	createTool := tooling.NewFuncTool(
		"edit.create_file",
		"Create a new file with the specified content.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"path": {
					Type:        "string",
					Description: "Path to the new file (relative to workspace)",
					Required:    true,
				},
				"content": {
					Type:        "string",
					Description: "Content to write to the file",
					Required:    true,
				},
			},
		},
		r.wrapWithTelemetry("edit.create_file", r.createFile),
	)
	if err := r.tools.Register(createTool); err != nil {
		return fmt.Errorf("register edit.create_file: %w", err)
	}

	return nil
}

// applyPatch implements the edit.apply_patch tool.
func (r *Registry) applyPatch(_ context.Context, args map[string]any) (*models.CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return errorResult("path is required"), nil
	}

	oldText, ok := args["old_text"].(string)
	if !ok {
		return errorResult("old_text is required"), nil
	}

	newText, ok := args["new_text"].(string)
	if !ok {
		return errorResult("new_text is required"), nil
	}

	absPath, err := r.resolvePath(path)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	// Read existing file
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return errorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	// Check if old_text exists
	contentStr := string(content)
	if !strings.Contains(contentStr, oldText) {
		return errorResult("old_text not found in file"), nil
	}

	// Count occurrences
	count := strings.Count(contentStr, oldText)
	if count > 1 {
		return errorResult(fmt.Sprintf("old_text appears %d times in file; must be unique", count)), nil
	}

	// Apply replacement
	newContent := strings.Replace(contentStr, oldText, newText, 1)

	// Write back
	if err := os.WriteFile(absPath, []byte(newContent), 0o644); err != nil {
		return errorResult(fmt.Sprintf("write file: %v", err)), nil
	}

	return successResult(map[string]any{
		"path":    path,
		"success": true,
		"message": "Patch applied successfully",
	}), nil
}

// createFile implements the edit.create_file tool.
func (r *Registry) createFile(_ context.Context, args map[string]any) (*models.CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return errorResult("path is required"), nil
	}

	content, ok := args["content"].(string)
	if !ok {
		return errorResult("content is required"), nil
	}

	absPath, err := r.resolvePath(path)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	// Check if file already exists
	if _, err := os.Stat(absPath); err == nil {
		return errorResult(fmt.Sprintf("file already exists: %s", path)), nil
	}

	// Create parent directories if needed
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errorResult(fmt.Sprintf("create directories: %v", err)), nil
	}

	// Write file
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return errorResult(fmt.Sprintf("write file: %v", err)), nil
	}

	return successResult(map[string]any{
		"path":    path,
		"success": true,
		"message": "File created successfully",
	}), nil
}

// applyStructuredDiff implements the edit.apply_structured_diff tool.
// It applies a structured diff (from code/diff skill) to a file.
func (r *Registry) applyStructuredDiff(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	// Check for cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path, ok := args["path"].(string)
	if !ok || path == "" {
		return errorResult("path is required"), nil
	}

	diffJSON, ok := maputil.AsStringMap(args["diff_json"])
	if !ok {
		return errorResult("diff_json is required and must be an object"), nil
	}

	dryRun := false
	if dr, ok := args["dry_run"].(bool); ok {
		dryRun = dr
	}

	// Parse hunks from diff_json
	hunks, err := parseHunksFromJSON(diffJSON)
	if err != nil {
		return errorResult(fmt.Sprintf("parse diff_json: %v", err)), nil
	}

	if len(hunks) == 0 {
		return errorResult("diff_json contains no hunks"), nil
	}

	// Resolve path
	absPath, err := r.resolvePath(path)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	// Open file for read/write to prevent TOCTOU race
	f, err := os.OpenFile(absPath, os.O_RDWR, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return errorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return errorResult(fmt.Sprintf("open file: %v", err)), nil
	}
	defer f.Close() //nolint:errcheck // Deferred close; error is not actionable after write

	content, err := io.ReadAll(f)
	if err != nil {
		return errorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	// Split into lines
	lines := splitFileLines(string(content))

	// Apply hunks in reverse order to preserve line numbers
	newLines, err := applyHunks(lines, hunks)
	if err != nil {
		return errorResult(fmt.Sprintf("apply hunks: %v", err)), nil
	}

	// If dry run, just validate
	if dryRun {
		return successResult(map[string]any{
			"path":         path,
			"success":      true,
			"dry_run":      true,
			"hunks_count":  len(hunks),
			"lines_before": len(lines),
			"lines_after":  len(newLines),
			"message":      "Dry run: diff validated successfully",
		}), nil
	}

	// Write back using same file handle
	newContent := strings.Join(newLines, "\n")
	// Preserve trailing newline if original had one
	if len(content) > 0 && content[len(content)-1] == '\n' {
		newContent += "\n"
	}

	if err := f.Truncate(0); err != nil {
		return errorResult(fmt.Sprintf("truncate file: %v", err)), nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return errorResult(fmt.Sprintf("seek file: %v", err)), nil
	}
	if _, err := f.WriteString(newContent); err != nil {
		return errorResult(fmt.Sprintf("write file: %v", err)), nil
	}

	return successResult(map[string]any{
		"path":         path,
		"success":      true,
		"hunks_count":  len(hunks),
		"lines_before": len(lines),
		"lines_after":  len(newLines),
		"message":      "Structured diff applied successfully",
	}), nil
}

// parseHunksFromJSON extracts DiffHunk slice from diff_json map.
func parseHunksFromJSON(diffJSON map[string]any) ([]DiffHunk, error) {
	// Check for nested diff object (code/diff returns {"diff": {...}})
	if diff, ok := maputil.AsStringMap(diffJSON["diff"]); ok {
		diffJSON = diff
	}

	hunksRaw, ok := diffJSON["hunks"].([]any)
	if !ok {
		return nil, fmt.Errorf("hunks field is required")
	}

	hunks := make([]DiffHunk, 0, len(hunksRaw))
	for i, h := range hunksRaw {
		hMap, ok := maputil.AsStringMap(h)
		if !ok {
			return nil, fmt.Errorf("hunk[%d] must be an object", i)
		}

		hunk := DiffHunk{}

		// Required fields - handle both int and float64 (JSON numbers are float64)
		if oldStart, ok := toInt(hMap["old_start"]); ok {
			hunk.OldStart = oldStart
		} else {
			return nil, fmt.Errorf("hunk[%d].old_start is required", i)
		}

		if oldLines, ok := toInt(hMap["old_lines"]); ok {
			hunk.OldLines = oldLines
		} else {
			return nil, fmt.Errorf("hunk[%d].old_lines is required", i)
		}

		if newStart, ok := toInt(hMap["new_start"]); ok {
			hunk.NewStart = newStart
		} else {
			return nil, fmt.Errorf("hunk[%d].new_start is required", i)
		}

		if newLines, ok := toInt(hMap["new_lines"]); ok {
			hunk.NewLines = newLines
		} else {
			return nil, fmt.Errorf("hunk[%d].new_lines is required", i)
		}

		// Optional fields
		if header, ok := hMap["header"].(string); ok {
			hunk.Header = header
		}

		if lines, ok := hMap["lines"].([]any); ok {
			hunk.Lines = make([]string, len(lines))
			for j, l := range lines {
				if line, ok := l.(string); ok {
					hunk.Lines[j] = line
				} else {
					return nil, fmt.Errorf("hunk[%d].lines[%d] must be a string", i, j)
				}
			}
		} else {
			return nil, fmt.Errorf("hunk[%d].lines is required", i)
		}

		hunks = append(hunks, hunk)
	}

	return hunks, nil
}

// toInt converts a value to int, handling both int and float64.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

// splitFileLines splits file content into lines without losing empty lines.
func splitFileLines(content string) []string {
	if content == "" {
		return []string{}
	}
	// Remove trailing newline to avoid empty last element
	content = strings.TrimSuffix(content, "\n")
	return strings.Split(content, "\n")
}

// applyHunks applies diff hunks to the original lines.
// Hunks are processed in reverse order to preserve line numbers.
func applyHunks(lines []string, hunks []DiffHunk) ([]string, error) {
	// Sort hunks by old_start in descending order
	sortedHunks := make([]DiffHunk, len(hunks))
	copy(sortedHunks, hunks)
	for i := 0; i < len(sortedHunks)-1; i++ {
		for j := i + 1; j < len(sortedHunks); j++ {
			if sortedHunks[i].OldStart < sortedHunks[j].OldStart {
				sortedHunks[i], sortedHunks[j] = sortedHunks[j], sortedHunks[i]
			}
		}
	}

	result := make([]string, len(lines))
	copy(result, lines)

	for _, hunk := range sortedHunks {
		var err error
		result, err = applyHunk(result, hunk)
		if err != nil {
			return nil, fmt.Errorf("hunk at line %d: %w", hunk.OldStart, err)
		}
	}

	return result, nil
}

// applyHunk applies a single hunk to lines.
// The hunk.Lines contain prefixed lines: ' ' for context, '-' for removed, '+' for added.
func applyHunk(lines []string, hunk DiffHunk) ([]string, error) {
	// old_start is 1-indexed
	startIdx := hunk.OldStart - 1
	if startIdx < 0 {
		startIdx = 0
	}

	// Validate hunk can be applied
	if startIdx > len(lines) {
		return nil, fmt.Errorf("old_start %d exceeds file length %d", hunk.OldStart, len(lines))
	}

	// Parse hunk lines into context, removed, and added
	var newSection []string
	oldIdx := startIdx
	expectedOldLines := 0

	for lineIdx, line := range hunk.Lines {
		if len(line) == 0 {
			// Empty line - treat as context
			if oldIdx < len(lines) {
				newSection = append(newSection, lines[oldIdx])
				oldIdx++
			}
			expectedOldLines++
			continue
		}

		prefix := line[0]
		content := ""
		if len(line) > 1 {
			content = line[1:]
		}

		switch prefix {
		case ' ': // Context line
			if oldIdx >= len(lines) {
				return nil, fmt.Errorf("context line at %d: unexpected end of file", oldIdx+1)
			}
			// Verify context matches
			if lines[oldIdx] != content {
				return nil, fmt.Errorf("context mismatch at line %d: expected %q, got %q", oldIdx+1, content, lines[oldIdx])
			}
			newSection = append(newSection, lines[oldIdx])
			oldIdx++
			expectedOldLines++
		case '-': // Removed line
			if oldIdx >= len(lines) {
				return nil, fmt.Errorf("removed line at %d: unexpected end of file", oldIdx+1)
			}
			// Verify line to remove matches
			if lines[oldIdx] != content {
				return nil, fmt.Errorf("remove mismatch at line %d: expected %q, got %q", oldIdx+1, content, lines[oldIdx])
			}
			// Skip this line (don't add to newSection)
			oldIdx++
			expectedOldLines++
		case '+': // Added line
			newSection = append(newSection, content)
		default:
			return nil, fmt.Errorf("unsupported diff line prefix %q at hunk line %d", prefix, lineIdx+1)
		}
	}

	// Validate old_lines count
	if expectedOldLines != hunk.OldLines {
		return nil, fmt.Errorf("old_lines mismatch: expected %d, processed %d", hunk.OldLines, expectedOldLines)
	}

	// Build new lines: before + newSection + after
	endIdx := startIdx + hunk.OldLines
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	result := make([]string, 0, len(lines)-hunk.OldLines+len(newSection))
	result = append(result, lines[:startIdx]...)
	result = append(result, newSection...)
	result = append(result, lines[endIdx:]...)

	return result, nil
}
