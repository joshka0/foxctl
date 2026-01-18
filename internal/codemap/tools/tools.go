// Package tools provides tool handlers for the codemap agent.
// These tools wrap existing skills and infrastructure to enable
// the agent to explore and understand the codebase.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/skillrun"
	"github.com/jkatigb/agentctl/internal/storage/graph"
)

// Registry holds codemap tool configurations and dependencies.
type Registry struct {
	workspace     string
	graphStore    graph.Store
	skillResolver *skill.Resolver
	tools         *dstools.InMemoryToolRegistry
	finalCodemap  json.RawMessage // Captured by finish_codemap tool
}

// RegistryOption configures the Registry.
type RegistryOption func(*Registry)

// WithWorkspace sets the workspace path.
func WithWorkspace(workspace string) RegistryOption {
	return func(r *Registry) {
		r.workspace = workspace
	}
}

// WithGraphStore sets the graph store.
func WithGraphStore(store graph.Store) RegistryOption {
	return func(r *Registry) {
		r.graphStore = store
	}
}

// WithSkillResolver sets the skill resolver.
func WithSkillResolver(resolver *skill.Resolver) RegistryOption {
	return func(r *Registry) {
		r.skillResolver = resolver
	}
}

// NewRegistry creates a new codemap tool registry.
func NewRegistry(opts ...RegistryOption) (*Registry, error) {
	r := &Registry{
		skillResolver: skill.NewResolver(),
		tools:         dstools.NewInMemoryToolRegistry(),
	}

	for _, opt := range opts {
		opt(r)
	}

	// Register all codemap tools
	if err := r.registerTools(); err != nil {
		return nil, fmt.Errorf("register tools: %w", err)
	}

	return r, nil
}

// Tools returns the underlying tool registry.
func (r *Registry) Tools() *dstools.InMemoryToolRegistry {
	return r.tools
}

// FinalCodemap returns the captured codemap from finish_codemap.
func (r *Registry) FinalCodemap() json.RawMessage {
	return r.finalCodemap
}

// registerTools registers all codemap tools.
func (r *Registry) registerTools() error {
	// read_file - Read file contents with optional line range
	if err := r.registerReadFile(); err != nil {
		return err
	}

	// search_pattern - Search for patterns using ripgrep
	if err := r.registerSearchPattern(); err != nil {
		return err
	}

	// get_symbols - Extract symbols from files
	if err := r.registerGetSymbols(); err != nil {
		return err
	}

	// get_graph_neighbors - Get graph neighbors
	if err := r.registerGetGraphNeighbors(); err != nil {
		return err
	}

	// semantic_search - Vector similarity search
	if err := r.registerSemanticSearch(); err != nil {
		return err
	}

	// finish_codemap - Capture final codemap output
	if err := r.registerFinishCodemap(); err != nil {
		return err
	}

	return nil
}

// registerReadFile registers the read_file tool.
func (r *Registry) registerReadFile() error {
	tool := dstools.NewFuncTool(
		"read_file",
		"Read the contents of a file. Supports optional line range for large files.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"path": {
					Type:        "string",
					Description: "File path relative to workspace",
					Required:    true,
				},
				"start_line": {
					Type:        "integer",
					Description: "Start line (1-based, optional)",
				},
				"end_line": {
					Type:        "integer",
					Description: "End line (1-based, optional)",
				},
			},
		},
		r.readFile,
	)
	return r.tools.Register(tool)
}

// readFile implements the read_file tool.
func (r *Registry) readFile(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return errorResult("path is required"), nil
	}

	// Resolve path relative to workspace
	fullPath := filepath.Join(r.workspace, path)

	// Evaluate symlinks to prevent path traversal via symlinks
	resolvedPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		// If file doesn't exist, EvalSymlinks fails - check original path
		if !os.IsNotExist(err) {
			return errorResult(fmt.Sprintf("resolve path: %v", err)), nil
		}
		resolvedPath = fullPath
	}

	// Get the resolved workspace path for comparison
	resolvedWorkspace, err := filepath.EvalSymlinks(r.workspace)
	if err != nil {
		resolvedWorkspace = r.workspace
	}

	// Verify resolved path is within workspace (using resolved paths)
	if !strings.HasPrefix(resolvedPath, resolvedWorkspace+string(filepath.Separator)) &&
		resolvedPath != resolvedWorkspace {
		return errorResult("path must be within workspace"), nil
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return errorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	lines := strings.Split(string(content), "\n")

	// Apply line range if specified
	startLine := 1
	endLine := len(lines)

	if sl, ok := args["start_line"].(float64); ok && sl > 0 {
		startLine = int(sl)
	}
	if el, ok := args["end_line"].(float64); ok && el > 0 {
		endLine = int(el)
	}

	// Clamp to valid range
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > endLine {
		startLine = endLine
	}

	// Extract requested lines (convert to 0-based)
	selectedLines := lines[startLine-1 : endLine]

	return successResult(map[string]any{
		"path":        path,
		"content":     strings.Join(selectedLines, "\n"),
		"start_line":  startLine,
		"end_line":    endLine,
		"total_lines": len(lines),
	}), nil
}

// registerSearchPattern registers the search_pattern tool.
func (r *Registry) registerSearchPattern() error {
	tool := dstools.NewFuncTool(
		"search_pattern",
		"Search for patterns in code using ripgrep. Returns matching code blocks with context.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"pattern": {
					Type:        "string",
					Description: "Search pattern (regex supported)",
					Required:    true,
				},
				"path": {
					Type:        "string",
					Description: "Path to search in (relative to workspace, defaults to workspace root)",
				},
				"case_insensitive": {
					Type:        "boolean",
					Description: "Case insensitive search (default true)",
				},
				"max_matches": {
					Type:        "integer",
					Description: "Maximum matches to return (default 50)",
				},
			},
		},
		r.searchPattern,
	)
	return r.tools.Register(tool)
}

// searchPattern implements the search_pattern tool.
func (r *Registry) searchPattern(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return errorResult("pattern is required"), nil
	}

	// Build input for code/context_ripgrep skill
	input := map[string]any{
		"path":             r.workspace,
		"pattern":          pattern,
		"case_insensitive": true,
		"max_matches":      50,
		"max_blocks":       20,
		"max_block_lines":  100,
	}

	if path, ok := args["path"].(string); ok && path != "" {
		input["path"] = filepath.Join(r.workspace, path)
	}
	if ci, ok := args["case_insensitive"].(bool); ok {
		input["case_insensitive"] = ci
	}
	if mm, ok := args["max_matches"].(float64); ok && mm > 0 {
		input["max_matches"] = int(mm)
	}

	// Execute skill
	result, err := r.runSkill(ctx, "code/context_ripgrep", input)
	if err != nil {
		return errorResult(fmt.Sprintf("search failed: %v", err)), nil
	}

	return successResult(result), nil
}

// registerGetSymbols registers the get_symbols tool.
func (r *Registry) registerGetSymbols() error {
	tool := dstools.NewFuncTool(
		"get_symbols",
		"Extract code symbols (functions, types, methods) from files.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"path": {
					Type:        "string",
					Description: "File or directory path (relative to workspace)",
					Required:    true,
				},
				"symbol_type": {
					Type:        "string",
					Description: "Filter by type: all, function, method, type, struct, interface (default: all)",
				},
				"include_private": {
					Type:        "boolean",
					Description: "Include private/unexported symbols (default false)",
				},
				"include_docs": {
					Type:        "boolean",
					Description: "Include documentation comments (default true)",
				},
			},
		},
		r.getSymbols,
	)
	return r.tools.Register(tool)
}

// getSymbols implements the get_symbols tool.
func (r *Registry) getSymbols(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return errorResult("path is required"), nil
	}

	// Build input for code/symbols skill
	input := map[string]any{
		"path":            filepath.Join(r.workspace, path),
		"symbol_type":     "all",
		"include_private": false,
		"include_docs":    true,
		"max_results":     200,
	}

	if st, ok := args["symbol_type"].(string); ok && st != "" {
		input["symbol_type"] = st
	}
	if ip, ok := args["include_private"].(bool); ok {
		input["include_private"] = ip
	}
	if id, ok := args["include_docs"].(bool); ok {
		input["include_docs"] = id
	}

	// Execute skill
	result, err := r.runSkill(ctx, "code/symbols", input)
	if err != nil {
		return errorResult(fmt.Sprintf("get symbols failed: %v", err)), nil
	}

	return successResult(result), nil
}

// registerGetGraphNeighbors registers the get_graph_neighbors tool.
func (r *Registry) registerGetGraphNeighbors() error {
	tool := dstools.NewFuncTool(
		"get_graph_neighbors",
		"Get neighboring nodes in the code graph. Useful for understanding relationships between files, symbols, and tasks.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"node_id": {
					Type:        "string",
					Description: "Node ID to get neighbors for",
					Required:    true,
				},
				"direction": {
					Type:        "string",
					Description: "Direction: in, out, or both (default: both)",
				},
				"edge_types": {
					Type:        "array",
					Description: "Filter by edge types: imports, calls, depends_on, etc.",
				},
			},
		},
		r.getGraphNeighbors,
	)
	return r.tools.Register(tool)
}

// getGraphNeighbors implements the get_graph_neighbors tool.
func (r *Registry) getGraphNeighbors(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.graphStore == nil {
		return errorResult("graph store not available"), nil
	}

	nodeID, ok := args["node_id"].(string)
	if !ok || nodeID == "" {
		return errorResult("node_id is required"), nil
	}

	direction := "both"
	if d, ok := args["direction"].(string); ok && d != "" {
		direction = d
	}

	var edgeTypes []graph.EdgeType
	if et, ok := args["edge_types"].([]any); ok {
		for _, e := range et {
			if s, ok := e.(string); ok {
				edgeTypes = append(edgeTypes, graph.EdgeType(s))
			}
		}
	}

	neighbors, err := r.graphStore.GetNeighbors(ctx, r.workspace, nodeID, graph.NeighborOptions{
		Direction: direction,
		EdgeTypes: edgeTypes,
	})
	if err != nil {
		return errorResult(fmt.Sprintf("get neighbors failed: %v", err)), nil
	}

	// Format results
	results := make([]map[string]any, 0, len(neighbors))
	for _, n := range neighbors {
		// Determine direction based on edge from/to IDs
		dir := "out"
		if n.Edge.ToID == nodeID {
			dir = "in"
		}
		results = append(results, map[string]any{
			"node_id":   n.Node.NodeID,
			"node_type": string(n.Node.NodeType),
			"title":     n.Node.Title,
			"path":      n.Node.CurrentPath,
			"edge_type": string(n.Edge.EdgeType),
			"direction": dir,
			"pagerank":  n.Node.PageRank,
		})
	}

	return successResult(map[string]any{
		"node_id":   nodeID,
		"neighbors": results,
		"count":     len(results),
	}), nil
}

// registerSemanticSearch registers the semantic_search tool.
func (r *Registry) registerSemanticSearch() error {
	tool := dstools.NewFuncTool(
		"semantic_search",
		"Search for code using natural language. Uses vector embeddings for semantic similarity.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"query": {
					Type:        "string",
					Description: "Natural language search query",
					Required:    true,
				},
				"scope": {
					Type:        "string",
					Description: "Search scope: symbols, sessions, memories, tasks (default: symbols)",
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum results to return (default 20)",
				},
			},
		},
		r.semanticSearch,
	)
	return r.tools.Register(tool)
}

// semanticSearch implements the semantic_search tool.
func (r *Registry) semanticSearch(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return errorResult("query is required"), nil
	}

	// Build input for code/semantic_search skill
	input := map[string]any{
		"query":     query,
		"scope":     []string{"symbols"},
		"limit":     20,
		"workspace": r.workspace,
	}

	if scope, ok := args["scope"].(string); ok && scope != "" {
		input["scope"] = []string{scope}
	}
	if limit, ok := args["limit"].(float64); ok && limit > 0 {
		input["limit"] = int(limit)
	}

	// Execute skill
	result, err := r.runSkill(ctx, "code/semantic_search", input)
	if err != nil {
		return errorResult(fmt.Sprintf("semantic search failed: %v", err)), nil
	}

	return successResult(result), nil
}

// registerFinishCodemap registers the finish_codemap tool.
func (r *Registry) registerFinishCodemap() error {
	tool := dstools.NewFuncTool(
		"finish_codemap",
		"Complete the codemap generation by providing the final structured output as a JSON string. Call this when you have gathered enough context.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"codemap": {
					Type:        "string",
					Description: "The complete codemap as a JSON string with title, description, and traces",
					Required:    true,
				},
			},
		},
		r.finishCodemap,
	)
	return r.tools.Register(tool)
}

// finishCodemap implements the finish_codemap tool.
func (r *Registry) finishCodemap(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	codemapStr, ok := args["codemap"].(string)
	if !ok || codemapStr == "" {
		return errorResult("codemap JSON string is required"), nil
	}

	// Validate it's valid JSON
	var codemap map[string]any
	if err := json.Unmarshal([]byte(codemapStr), &codemap); err != nil {
		return errorResult(fmt.Sprintf("invalid codemap JSON: %v", err)), nil
	}

	// Validate required fields
	if _, ok := codemap["title"].(string); !ok {
		return errorResult("codemap must have a 'title' string field"), nil
	}
	if _, ok := codemap["description"].(string); !ok {
		return errorResult("codemap must have a 'description' string field"), nil
	}
	if _, ok := codemap["traces"].([]any); !ok {
		return errorResult("codemap must have a 'traces' array field"), nil
	}

	// Re-marshal to ensure clean JSON
	codemapJSON, err := json.Marshal(codemap)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal codemap: %v", err)), nil
	}
	r.finalCodemap = codemapJSON

	return successResult(map[string]any{
		"status":  "completed",
		"message": "Codemap captured successfully",
		"codemap": codemap,
	}), nil
}

// runSkill executes a skill and returns the parsed data.
func (r *Registry) runSkill(ctx context.Context, skillName string, input map[string]any) (map[string]any, error) {
	if r.skillResolver == nil {
		return nil, fmt.Errorf("skill resolver not available")
	}

	var payload map[string]any
	result, err := skillrun.RunAndDecodeInto(ctx, r.skillResolver, skillName, input, skillrun.Options{
		PreferCGO: buildinfo.IsCGO(),
	}, &payload)
	if err != nil {
		var runErr skillrun.RunError
		if errors.As(err, &runErr) {
			return nil, runErr
		}
		var statusErr protocol.EnvelopeStatusError
		if errors.As(err, &statusErr) {
			return nil, statusErr
		}
		if errors.Is(err, skill.ErrArtifactsMissing) {
			if result.Handle.ManifestPath == "" {
				return nil, fmt.Errorf("skill binary not found for %s; run 'make skills-install'", skillName)
			}
			return nil, fmt.Errorf("skill binary not found in %s; run 'make skills-install'", filepath.Dir(result.Handle.ManifestPath))
		}
		return nil, err
	}

	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

// Helper functions

func successResult(data map[string]any) *models.CallToolResult {
	content, _ := json.Marshal(data)
	return &models.CallToolResult{
		Content: []models.Content{
			models.TextContent{Type: "text", Text: string(content)},
		},
		IsError: false,
	}
}

func errorResult(msg string) *models.CallToolResult {
	return &models.CallToolResult{
		Content: []models.Content{
			models.TextContent{Type: "text", Text: msg},
		},
		IsError: true,
	}
}
