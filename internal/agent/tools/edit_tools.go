package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
)

// registerEditTools registers file editing tools.
func (r *Registry) registerEditTools() error {
	// edit.apply_patch - apply a unified diff patch to a file
	patchTool := dstools.NewFuncTool(
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

	// edit.create_file - create a new file
	createTool := dstools.NewFuncTool(
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
	if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
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
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errorResult(fmt.Sprintf("create directories: %v", err)), nil
	}

	// Write file
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return errorResult(fmt.Sprintf("write file: %v", err)), nil
	}

	return successResult(map[string]any{
		"path":    path,
		"success": true,
		"message": "File created successfully",
	}), nil
}
