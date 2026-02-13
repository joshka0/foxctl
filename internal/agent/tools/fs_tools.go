package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	tooling "github.com/jkatigb/agentctl/internal/tooling"
)

// registerFSTools registers filesystem tools.
func (r *Registry) registerFSTools() error {
	// fs.read_file
	readFileTool := tooling.NewFuncTool(
		"fs.read_file",
		"Read the contents of a file. Returns the text content with size limits.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"path": {
					Type:        "string",
					Description: "Path to the file (relative to workspace or absolute within workspace)",
					Required:    true,
				},
				"max_bytes": {
					Type:        "integer",
					Description: "Maximum bytes to read (optional, default 1MB)",
				},
			},
		},
		r.wrapWithTelemetry("fs.read_file", r.readFile),
	)
	if err := r.tools.Register(readFileTool); err != nil {
		return fmt.Errorf("register fs.read_file: %w", err)
	}

	// fs.list_dir
	listDirTool := tooling.NewFuncTool(
		"fs.list_dir",
		"List contents of a directory. Returns file and directory names with basic metadata.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"path": {
					Type:        "string",
					Description: "Path to the directory (relative to workspace or absolute within workspace)",
					Required:    true,
				},
				"depth": {
					Type:        "integer",
					Description: "Maximum depth to recurse (optional, default 1 = direct children only)",
				},
			},
		},
		r.wrapWithTelemetry("fs.list_dir", r.listDir),
	)
	if err := r.tools.Register(listDirTool); err != nil {
		return fmt.Errorf("register fs.list_dir: %w", err)
	}

	return nil
}

// readFile implements the fs.read_file tool.
func (r *Registry) readFile(_ context.Context, args map[string]any) (*models.CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return errorResult("path is required"), nil
	}

	absPath, err := r.resolvePath(path)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	// Check if file exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return errorResult(fmt.Sprintf("stat file: %v", err)), nil
	}

	if info.IsDir() {
		return errorResult(fmt.Sprintf("%s is a directory, not a file", path)), nil
	}

	// Determine max bytes
	maxBytes := r.config.MaxFileSize
	if mb, ok := args["max_bytes"].(float64); ok && mb > 0 {
		maxBytes = int64(mb)
	}

	// Check file size
	if info.Size() > maxBytes {
		return errorResult(fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), maxBytes)), nil
	}

	// Read file
	content, err := os.ReadFile(absPath)
	if err != nil {
		return errorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	result := map[string]any{
		"path":    path,
		"content": string(content),
		"size":    len(content),
	}
	return successResult(result), nil
}

// DirEntry represents an entry in a directory listing.
type DirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size,omitempty"`
}

// listDir implements the fs.list_dir tool.
func (r *Registry) listDir(_ context.Context, args map[string]any) (*models.CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		path = "."
	}
	if path == "" {
		path = "."
	}

	absPath, err := r.resolvePath(path)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	// Check if directory exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errorResult(fmt.Sprintf("directory not found: %s", path)), nil
		}
		return errorResult(fmt.Sprintf("stat directory: %v", err)), nil
	}

	if !info.IsDir() {
		return errorResult(fmt.Sprintf("%s is a file, not a directory", path)), nil
	}

	// Determine max depth
	maxDepth := 1
	if d, ok := args["depth"].(float64); ok && d > 0 {
		maxDepth = int(d)
	}

	var entries []DirEntry

	err = filepath.WalkDir(absPath, func(walkPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // Skip entries with errors
		}

		// Calculate relative depth
		rel, relErr := filepath.Rel(absPath, walkPath)
		if relErr != nil {
			return nil
		}

		// Skip the root directory itself
		if rel == "." {
			return nil
		}

		// Check depth
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Get file info for size
		var size int64
		if !d.IsDir() {
			if fi, fiErr := d.Info(); fiErr == nil {
				size = fi.Size()
			}
		}

		// Build relative path from workspace.
		// Rel error is safe to ignore when both paths are absolute from same root.
		relFromWorkspace, _ := filepath.Rel(r.config.WorkspaceRoot, walkPath) //nolint:errcheck

		entries = append(entries, DirEntry{
			Name:  d.Name(),
			Path:  relFromWorkspace,
			IsDir: d.IsDir(),
			Size:  size,
		})

		return nil
	})
	if err != nil {
		return errorResult(fmt.Sprintf("walk directory: %v", err)), nil
	}

	result := map[string]any{
		"path":    path,
		"entries": entries,
		"count":   len(entries),
	}
	return successResult(result), nil
}

// successResult creates a successful CallToolResult from any data.
func successResult(data any) *models.CallToolResult {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal result: %v", err))
	}
	return &models.CallToolResult{
		Content: []models.Content{
			models.TextContent{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
		IsError: false,
	}
}

// errorResult creates an error CallToolResult.
func errorResult(msg string) *models.CallToolResult {
	return &models.CallToolResult{
		Content: []models.Content{
			models.TextContent{
				Type: "text",
				Text: msg,
			},
		},
		IsError: true,
	}
}
