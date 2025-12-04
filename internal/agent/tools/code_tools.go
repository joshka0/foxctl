package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/execution/runner"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
)

// registerCodeTools registers code search and retrieval tools.
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

	// code.symbol_search - searches the symbol index for relevant symbols
	symbolSearchTool := dstools.NewFuncTool(
		"code.symbol_search",
		"Search the code symbol index for functions, methods, and classes relevant to a question. Returns ranked candidates with file paths and symbol metadata.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"workspace_id": {
					Type:        "string",
					Description: "Workspace identifier",
					Required:    true,
				},
				"question": {
					Type:        "string",
					Description: "Natural language question describing what you're looking for",
					Required:    true,
				},
				"mode": {
					Type:        "string",
					Description: "Search mode: 'search' (default), 'callers', or 'callees'",
				},
				"symbol_hint": {
					Type:        "string",
					Description: "Optional symbol name hint to narrow search",
				},
				"max_results": {
					Type:        "integer",
					Description: "Maximum number of results to return (default 20)",
				},
			},
		},
		r.wrapWithTelemetry("code.symbol_search", r.codeSymbolSearch),
	)
	if err := r.tools.Register(symbolSearchTool); err != nil {
		return fmt.Errorf("register code.symbol_search: %w", err)
	}

	// code.swe_grep - extracts high-signal code snippets from candidate files
	sweGrepTool := dstools.NewFuncTool(
		"code.swe_grep",
		"Extract high-signal code snippets from candidate files based on a natural language question. Use this after code.symbol_search to get detailed code context.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"workspace_id": {
					Type:        "string",
					Description: "Workspace identifier",
					Required:    true,
				},
				"question": {
					Type:        "string",
					Description: "Natural language question to guide snippet extraction",
					Required:    true,
				},
				"candidate_files": {
					Type:        "array",
					Description: "Array of candidate files with optional symbol_id and priority",
					Required:    true,
				},
			},
		},
		r.wrapWithTelemetry("code.swe_grep", r.codeSweGrep),
	)
	if err := r.tools.Register(sweGrepTool); err != nil {
		return fmt.Errorf("register code.swe_grep: %w", err)
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
	var searchPath string
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
			_, _ = fmt.Sscanf(parts[1], "%d", &lineNum)
			matches = append(matches, SearchMatch{
				File:    file,
				Line:    lineNum,
				Content: parts[2],
			})
		}
	}

	return matches
}

// SymbolCandidate represents a symbol search result.
type SymbolCandidate struct {
	File     string  `json:"file"`
	SymbolID string  `json:"symbol_id"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Score    float64 `json:"score"`
}

// codeSymbolSearch implements the code.symbol_search tool.
// NOTE: This is a stub implementation. The symbol index is Phase 3-4 work.
// When the symbol index is available, this will wrap internal Go APIs.
func (r *Registry) codeSymbolSearch(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	// Check for cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate required args
	if _, ok := args["workspace_id"].(string); !ok {
		return errorResult("workspace_id is required"), nil
	}
	question, ok := args["question"].(string)
	if !ok || question == "" {
		return errorResult("question is required"), nil
	}

	// Parse optional args
	mode := "search"
	if m, ok := args["mode"].(string); ok && m != "" {
		mode = m
	}
	if mode != "search" && mode != "callers" && mode != "callees" {
		return errorResult("mode must be 'search', 'callers', or 'callees'"), nil
	}

	maxResults := 20
	if m, ok := args["max_results"].(float64); ok && m > 0 {
		maxResults = int(m)
	}

	// TODO(phase3-4): Wire to actual symbol index when available.
	// For now, return an empty result set to indicate the tool is recognized
	// but the backing index is not yet implemented.
	_ = maxResults // Will be used when symbol index is wired

	return successResult(map[string]any{
		"candidates": []SymbolCandidate{},
		"count":      0,
		"message":    "Symbol index not yet available. Use code.search or code.swe_grep with known file paths.",
	}), nil
}

// SweGrepSnippet represents a code snippet from SWE Grep.
type SweGrepSnippet struct {
	File      string `json:"file"`
	SymbolID  string `json:"symbol_id,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Preview   string `json:"preview"`
}

// SweGrepInput represents the input to the code/swe_grep skill.
type SweGrepInput struct {
	WorkspaceID string             `json:"workspace_id"`
	Question    string             `json:"question"`
	Candidates  []SweGrepCandidate `json:"candidates"`
	Limits      *SweGrepLimits     `json:"limits,omitempty"`
}

// SweGrepCandidate represents a candidate file for SWE Grep.
type SweGrepCandidate struct {
	Path     string  `json:"path"`
	SymbolID string  `json:"symbol_id,omitempty"`
	Priority float64 `json:"priority,omitempty"`
}

// SweGrepLimits represents optional limits for SWE Grep.
type SweGrepLimits struct {
	MaxFiles        int `json:"max_files,omitempty"`
	MaxSnippets     int `json:"max_snippets,omitempty"`
	MaxBytesPerFile int `json:"max_bytes_per_file,omitempty"`
}

// codeSweGrep implements the code.swe_grep tool by invoking the code/swe_grep skill.
func (r *Registry) codeSweGrep(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	// Validate required args
	workspaceID, ok := args["workspace_id"].(string)
	if !ok || workspaceID == "" {
		return errorResult("workspace_id is required"), nil
	}
	question, ok := args["question"].(string)
	if !ok || question == "" {
		return errorResult("question is required"), nil
	}
	candidatesRaw, ok := args["candidate_files"].([]any)
	if !ok || len(candidatesRaw) == 0 {
		return errorResult("candidate_files is required and must not be empty"), nil
	}

	// Parse candidates
	candidates := make([]SweGrepCandidate, 0, len(candidatesRaw))
	for i, c := range candidatesRaw {
		cMap, ok := c.(map[string]any)
		if !ok {
			return errorResult(fmt.Sprintf("candidate_files[%d] must be an object", i)), nil
		}
		path, ok := cMap["path"].(string)
		if !ok || path == "" {
			return errorResult(fmt.Sprintf("candidate_files[%d].path is required", i)), nil
		}
		candidate := SweGrepCandidate{Path: path}
		if symID, ok := cMap["symbol_id"].(string); ok {
			candidate.SymbolID = symID
		}
		if prio, ok := cMap["priority"].(float64); ok {
			candidate.Priority = prio
		}
		candidates = append(candidates, candidate)
	}

	// Build skill input
	input := SweGrepInput{
		WorkspaceID: workspaceID,
		Question:    question,
		Candidates:  candidates,
	}

	inputBytes, err := json.Marshal(input)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal input: %v", err)), nil
	}

	// Resolve the code/swe_grep skill
	resolver := skill.NewResolver(skill.WithSearchPaths(
		r.config.WorkspaceRoot+"/dist/skills",
		r.config.WorkspaceRoot+"/skills",
	))
	handle, err := resolver.Resolve("code/swe_grep")
	if err != nil {
		// Skill not found - return helpful error
		return errorResult(fmt.Sprintf("skill code/swe_grep not found: %v (ensure skill is installed)", err)), nil
	}

	// Load manifest
	manifest, err := skill.LoadManifest(handle.ManifestPath)
	if err != nil {
		return errorResult(fmt.Sprintf("load manifest: %v", err)), nil
	}

	// Set workspace in context for the skill
	ctx = workspace.WithContext(ctx, r.config.WorkspaceRoot)

	// Execute the skill
	stdout, stderr, err := runner.RunWithOptions(ctx, runner.RunOptions{
		Manifest:     manifest,
		ArtifactPath: handle.ArtifactPath + "/bin",
		Input:        inputBytes,
	})
	if err != nil {
		// Include stderr in error message for debugging
		errMsg := fmt.Sprintf("skill execution failed: %v", err)
		if len(stderr) > 0 {
			errMsg += fmt.Sprintf(" (stderr: %s)", string(stderr))
		}
		return errorResult(errMsg), nil
	}

	// Parse the envelope response
	var envelope map[string]any
	if err := json.Unmarshal(stdout, &envelope); err != nil {
		return errorResult(fmt.Sprintf("parse skill response: %v", err)), nil
	}

	// Check envelope status
	status, _ := envelope["status"].(string)
	if status == "error" {
		errObj, _ := envelope["error"].(map[string]any)
		errCode, _ := errObj["code"].(string)
		errMsg, _ := errObj["message"].(string)
		return errorResult(fmt.Sprintf("skill error [%s]: %s", errCode, errMsg)), nil
	}

	// Extract data from envelope
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		return errorResult("invalid skill response: missing data"), nil
	}

	// Build tool result
	result := map[string]any{
		"snippets": []SweGrepSnippet{},
		"count":    0,
	}

	// Extract summary
	if summary, ok := data["summary"].(map[string]any); ok {
		result["files_considered"] = summary["files_considered"]
		result["files_relevant"] = summary["files_relevant"]
		result["snippets_emitted"] = summary["snippets_emitted"]
	}

	// Extract inline snippets
	if snippetsInline, ok := data["snippets_inline"].([]any); ok {
		snippets := make([]SweGrepSnippet, 0, len(snippetsInline))
		for _, s := range snippetsInline {
			sMap, ok := s.(map[string]any)
			if !ok {
				continue
			}
			snippet := SweGrepSnippet{}
			if f, ok := sMap["file"].(string); ok {
				snippet.File = f
			}
			if sym, ok := sMap["symbol_id"].(string); ok {
				snippet.SymbolID = sym
			}
			if sl, ok := sMap["start_line"].(float64); ok {
				snippet.StartLine = int(sl)
			}
			if el, ok := sMap["end_line"].(float64); ok {
				snippet.EndLine = int(el)
			}
			if p, ok := sMap["preview"].(string); ok {
				snippet.Preview = p
			}
			snippets = append(snippets, snippet)
		}
		result["snippets"] = snippets
		result["count"] = len(snippets)
	}

	// Include CAS artifact reference if present
	if artifact, ok := data["artifact"].(string); ok {
		result["cas_artifact"] = artifact
	}
	if artifactKind, ok := data["artifact_kind"].(string); ok {
		result["cas_artifact_kind"] = artifactKind
	}

	return successResult(result), nil
}
