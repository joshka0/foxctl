package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
)

// registerCodeTools registers code search tools.
func (r *Registry) registerCodeTools() error {
	// code.search - uses ripgrep for code search
	searchTool := dstools.NewFuncTool(
		"code.search",
		"Search for patterns in code files using ripgrep. Returns matching lines with context.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"pattern": {
					Type:        "string",
					Description: "The search pattern (regex supported)",
					Required:    true,
				},
				"path": {
					Type:        "string",
					Description: "Path to search in (relative to workspace, defaults to current directory)",
				},
				"file_pattern": {
					Type:        "string",
					Description: "Glob pattern to filter files (e.g., '*.go', '*.ts')",
				},
				"context_lines": {
					Type:        "integer",
					Description: "Number of context lines to include (default 2)",
				},
				"max_results": {
					Type:        "integer",
					Description: "Maximum number of results to return (default 50)",
				},
			},
		},
		r.wrapWithTelemetry("code.search", r.codeSearch),
	)
	if err := r.tools.Register(searchTool); err != nil {
		return fmt.Errorf("register code.search: %w", err)
	}

	return nil
}

// SearchMatch represents a single search match.
type SearchMatch struct {
	File        string `json:"file"`
	Line        int    `json:"line"`
	Content     string `json:"content"`
	ContextPre  string `json:"context_pre,omitempty"`
	ContextPost string `json:"context_post,omitempty"`
}

// codeSearch implements the code.search tool using ripgrep.
func (r *Registry) codeSearch(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return errorResult("pattern is required"), nil
	}

	// Build search path
	searchPath := "."
	if p, ok := args["path"].(string); ok && p != "" {
		resolved, err := r.resolvePath(p)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		searchPath = resolved
	} else {
		searchPath = r.config.WorkspaceRoot
	}

	// Build rg command (using text output format for simpler parsing)
	rgArgs := []string{
		"--line-number",
		"--no-heading",
		"--max-count", "100", // Limit matches per file
	}

	// Context lines
	contextLines := 2
	if c, ok := args["context_lines"].(float64); ok && c >= 0 {
		contextLines = int(c)
	}
	if contextLines > 0 {
		rgArgs = append(rgArgs, "-C", fmt.Sprintf("%d", contextLines))
	}

	// File pattern filter
	if fp, ok := args["file_pattern"].(string); ok && fp != "" {
		rgArgs = append(rgArgs, "-g", fp)
	}

	// Add pattern and path
	rgArgs = append(rgArgs, pattern, searchPath)

	// Execute ripgrep with context for cancellation support
	cmd := exec.CommandContext(ctx, "rg", rgArgs...)
	output, err := cmd.Output()
	if err != nil {
		// rg returns exit code 1 when no matches found
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return successResult(map[string]any{
				"matches": []SearchMatch{},
				"count":   0,
			}), nil
		}
		return errorResult(fmt.Sprintf("ripgrep error: %v", err)), nil
	}

	// Parse text output from ripgrep (file:line:content format)
	matches := parseRipgrepOutput(string(output), r.config.WorkspaceRoot)

	// Apply max results limit
	maxResults := r.config.MaxSearchResults
	if m, ok := args["max_results"].(float64); ok && m > 0 {
		maxResults = int(m)
	}
	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}

	return successResult(map[string]any{
		"matches": matches,
		"count":   len(matches),
	}), nil
}

// parseRipgrepOutput parses ripgrep text output (file:line:content format) into SearchMatch slice.
func parseRipgrepOutput(output, workspaceRoot string) []SearchMatch {
	var matches []SearchMatch

	// Simple line-by-line parsing for non-JSON fallback
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Basic parsing - in production would use proper JSON parsing
		// Format: file:line:content
		parts := strings.SplitN(line, ":", 3)
		if len(parts) >= 3 {
			file := parts[0]
			if strings.HasPrefix(file, workspaceRoot) {
				file = strings.TrimPrefix(file, workspaceRoot+"/")
			}
			lineNum := 0
			fmt.Sscanf(parts[1], "%d", &lineNum)
			matches = append(matches, SearchMatch{
				File:    file,
				Line:    lineNum,
				Content: parts[2],
			})
		}
	}

	return matches
}
