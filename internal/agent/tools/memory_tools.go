package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/skillrun"
)

// registerMemoryTools registers memory access tools.
func (r *Registry) registerMemoryTools() error {
	// memory.query - search named memories
	queryTool := dstools.NewFuncTool(
		"memory.query",
		"Query stored memories (gotchas, decisions, learnings, insights) for relevant context. Use this to find previously recorded knowledge about the codebase.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"query": {
					Type:        "string",
					Description: "Search query describing what you're looking for",
					Required:    true,
				},
				"types": {
					Type:        "string",
					Description: "Comma-separated memory types to filter: gotcha, decision, learning, insight, pattern (default: all)",
				},
				"file": {
					Type:        "string",
					Description: "Filter by file path (exact or partial match)",
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum results to return (default 10)",
				},
			},
		},
		r.wrapWithTelemetry("memory.query", r.memoryQuery),
	)
	if err := r.tools.Register(queryTool); err != nil {
		return fmt.Errorf("register memory.query: %w", err)
	}

	// memory.put - store new memory
	putTool := dstools.NewFuncTool(
		"memory.put",
		"Store a new memory (gotcha, decision, learning, insight) for future reference. Use this to record important findings about the codebase.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"name": {
					Type:        "string",
					Description: "Short identifier for the memory (e.g., 'sqlite-wal-mode-gotcha')",
					Required:    true,
				},
				"type": {
					Type:        "string",
					Description: "Memory type: gotcha, decision, learning, insight, pattern",
					Required:    true,
				},
				"summary": {
					Type:        "string",
					Description: "Brief description of the memory (1-2 sentences)",
					Required:    true,
				},
				"content": {
					Type:        "string",
					Description: "Full content/details of the memory",
				},
				"file": {
					Type:        "string",
					Description: "Associated file path (if relevant)",
				},
			},
		},
		r.wrapWithTelemetry("memory.put", r.memoryPut),
	)
	if err := r.tools.Register(putTool); err != nil {
		return fmt.Errorf("register memory.put: %w", err)
	}

	return nil
}

// memoryQueryInput matches the memory/query skill input.
type memoryQueryInput struct {
	Query     string `json:"query"`
	Types     string `json:"types,omitempty"`
	File      string `json:"file,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// memoryQuery implements the memory.query tool by invoking the memory/query skill.
func (r *Registry) memoryQuery(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return errorResult("query is required"), nil
	}

	input := memoryQueryInput{
		Query:     query,
		Workspace: r.config.WorkspaceRoot,
		Limit:     10,
	}

	if types, ok := args["types"].(string); ok && types != "" {
		input.Types = types
	}

	if file, ok := args["file"].(string); ok && file != "" {
		input.File = file
	}

	// Parse limit from various numeric types (float64, int, int64, json.Number)
	switch v := args["limit"].(type) {
	case float64:
		if v > 0 {
			input.Limit = int(v)
		}
	case int:
		if v > 0 {
			input.Limit = v
		}
	case int64:
		if v > 0 {
			input.Limit = int(v)
		}
	case json.Number:
		if n, err := v.Int64(); err == nil && n > 0 {
			input.Limit = int(n)
		}
	}

	inputBytes, err := json.Marshal(input)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal input: %v", err)), nil
	}

	resolver := skill.NewResolver(skill.WithSearchPaths(
		r.config.WorkspaceRoot+"/dist/skills",
		r.config.WorkspaceRoot+"/skills",
	))

	ctx = workspace.WithContext(ctx, r.config.WorkspaceRoot)

	var payload struct {
		Memories []struct {
			Name      string  `json:"name"`
			Type      string  `json:"type"`
			Summary   string  `json:"summary"`
			File      string  `json:"file,omitempty"`
			Score     float64 `json:"score"`
			CreatedAt string  `json:"created_at,omitempty"`
			SessionID string  `json:"session_id,omitempty"`
			Content   any     `json:"content,omitempty"`
		} `json:"memories"`
		Pagination struct {
			Total   int  `json:"total"`
			HasMore bool `json:"has_more"`
		} `json:"pagination"`
		Stats struct {
			TotalFound   int    `json:"total_found"`
			SearchMethod string `json:"search_method"`
			LatencyMS    int    `json:"latency_ms"`
		} `json:"stats"`
	}

	_, err = skillrun.RunAndDecodeInto(ctx, resolver, "memory/query", inputBytes, skillrun.Options{
		PreferCGO: buildinfo.IsCGO(),
		EntryRoot: r.config.WorkspaceRoot,
	}, &payload)
	if err != nil {
		if errors.Is(err, skill.ErrArtifactsMissing) {
			return errorResult("memory/query skill not found (ensure skill is installed)"), nil
		}
		var runErr skillrun.RunError
		if errors.As(err, &runErr) {
			errMsg := fmt.Sprintf("skill execution failed: %v", runErr.Err)
			if len(runErr.Stderr) > 0 {
				errMsg += fmt.Sprintf(" (stderr: %s)", string(runErr.Stderr))
			}
			return errorResult(errMsg), nil
		}
		return errorResult(fmt.Sprintf("skill error: %v", err)), nil
	}

	// Build simplified result for agent consumption
	memories := make([]map[string]any, 0, len(payload.Memories))
	for _, m := range payload.Memories {
		mem := map[string]any{
			"name":    m.Name,
			"type":    m.Type,
			"summary": m.Summary,
			"score":   m.Score,
		}
		if m.File != "" {
			mem["file"] = m.File
		}
		if m.CreatedAt != "" {
			mem["created_at"] = m.CreatedAt
		}
		memories = append(memories, mem)
	}

	return successResult(map[string]any{
		"memories":    memories,
		"count":       len(memories),
		"total_found": payload.Stats.TotalFound,
		"has_more":    payload.Pagination.HasMore,
	}), nil
}

// memoryPutInput matches the memory/put skill input.
type memoryPutInput struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Summary   string `json:"summary"`
	Content   string `json:"content,omitempty"`
	File      string `json:"file,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

// memoryPut implements the memory.put tool by invoking the memory/put skill.
func (r *Registry) memoryPut(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return errorResult("name is required"), nil
	}

	memType, ok := args["type"].(string)
	if !ok || memType == "" {
		return errorResult("type is required (gotcha, decision, learning, insight, pattern)"), nil
	}

	summary, ok := args["summary"].(string)
	if !ok || summary == "" {
		return errorResult("summary is required"), nil
	}

	input := memoryPutInput{
		Name:      name,
		Type:      memType,
		Summary:   summary,
		Workspace: r.config.WorkspaceRoot,
	}

	if content, ok := args["content"].(string); ok {
		input.Content = content
	}

	if file, ok := args["file"].(string); ok {
		input.File = file
	}

	inputBytes, err := json.Marshal(input)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal input: %v", err)), nil
	}

	resolver := skill.NewResolver(skill.WithSearchPaths(
		r.config.WorkspaceRoot+"/dist/skills",
		r.config.WorkspaceRoot+"/skills",
	))

	ctx = workspace.WithContext(ctx, r.config.WorkspaceRoot)

	var payload struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Status  string `json:"status"`
		Message string `json:"message"`
	}

	_, err = skillrun.RunAndDecodeInto(ctx, resolver, "memory/put", inputBytes, skillrun.Options{
		PreferCGO: buildinfo.IsCGO(),
		EntryRoot: r.config.WorkspaceRoot,
	}, &payload)
	if err != nil {
		if errors.Is(err, skill.ErrArtifactsMissing) {
			return errorResult("memory/put skill not found (ensure skill is installed)"), nil
		}
		var runErr skillrun.RunError
		if errors.As(err, &runErr) {
			errMsg := fmt.Sprintf("skill execution failed: %v", runErr.Err)
			if len(runErr.Stderr) > 0 {
				errMsg += fmt.Sprintf(" (stderr: %s)", string(runErr.Stderr))
			}
			return errorResult(errMsg), nil
		}
		return errorResult(fmt.Sprintf("skill error: %v", err)), nil
	}

	return successResult(map[string]any{
		"id":      payload.ID,
		"name":    payload.Name,
		"type":    payload.Type,
		"status":  payload.Status,
		"message": payload.Message,
	}), nil
}
