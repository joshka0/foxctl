package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	tooling "github.com/jkatigb/agentctl/internal/tooling"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/repoquery"
)

// registerRepoIndexTools registers repo index tools for agents.
func (r *Registry) registerRepoIndexTools() error {
	if r.openRepoIndexStore == nil {
		return nil
	}

	searchTool := tooling.NewFuncTool(
		repoindex.ToolSearch,
		"Search the repo index for nodes that match a text query.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"query": {
					Type:        "string",
					Description: "FTS query string",
					Required:    true,
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum results (default 20)",
				},
			},
		},
		r.wrapWithTelemetry(repoindex.ToolSearch, r.repoIndexSearch),
	)
	if err := r.tools.Register(searchTool); err != nil {
		return fmt.Errorf("register repo index search: %w", err)
	}

	expandTool := tooling.NewFuncTool(
		repoindex.ToolExpand,
		"Expand the repo index graph from seed node IDs.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"seeds": {
					Type:        "array",
					Description: "Seed node IDs",
					Required:    true,
				},
				"edge_types": {
					Type:        "array",
					Description: "Edge types to traverse",
				},
				"direction": {
					Type:        "string",
					Description: "Traversal direction: out or in",
				},
				"depth": {
					Type:        "integer",
					Description: "Traversal depth (default 1)",
				},
				"budget": {
					Type:        "integer",
					Description: "Max nodes to return (default 50)",
				},
				"per_node_cap": {
					Type:        "integer",
					Description: "Max edges per node per hop (default 50)",
				},
			},
		},
		r.wrapWithTelemetry(repoindex.ToolExpand, r.repoIndexExpand),
	)
	if err := r.tools.Register(expandTool); err != nil {
		return fmt.Errorf("register repo index expand: %w", err)
	}

	openTool := tooling.NewFuncTool(
		repoindex.ToolOpen,
		"Open a repo index node by ID.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"id": {
					Type:        "string",
					Description: "Node ID",
					Required:    true,
				},
			},
		},
		r.wrapWithTelemetry(repoindex.ToolOpen, r.repoIndexOpen),
	)
	if err := r.tools.Register(openTool); err != nil {
		return fmt.Errorf("register repo index open: %w", err)
	}

	dagGrepTool := tooling.NewFuncTool(
		repoindex.ToolDAGGrep,
		"Search and expand the repo index into a compact explanation subgraph.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"query": {
					Type:        "string",
					Description: "Search query",
					Required:    true,
				},
				"mode": {
					Type:        "string",
					Description: "Search mode: fts, semantic, or hybrid",
				},
				"k": {
					Type:        "integer",
					Description: "Number of seed nodes (default 10)",
				},
				"node_kinds": {
					Type:        "array",
					Description: "Limit nodes to kinds: symbol, file, package, concept",
				},
				"edge_sets": {
					Type:        "array",
					Description: "Edge sets: structural, doc, all",
				},
				"edge_types": {
					Type:        "array",
					Description: "Edge types to traverse",
				},
				"direction": {
					Type:        "string",
					Description: "Traversal direction: out or in",
				},
				"depth": {
					Type:        "integer",
					Description: "Traversal depth (default 2)",
				},
				"budget": {
					Type:        "integer",
					Description: "Max nodes to return (default 80)",
				},
				"per_node_cap": {
					Type:        "integer",
					Description: "Max edges per node per hop (default 20)",
				},
				"include_anchors": {
					Type:        "boolean",
					Description: "Include file/package anchors for symbol/file nodes",
				},
				"render": {
					Type:        "string",
					Description: "Optional render format: tree or mermaid",
				},
			},
		},
		r.wrapWithTelemetry(repoindex.ToolDAGGrep, r.repoIndexDagGrep),
	)
	if err := r.tools.Register(dagGrepTool); err != nil {
		return fmt.Errorf("register repo index dag_grep: %w", err)
	}

	return nil
}

func (r *Registry) repoIndexSearch(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return errorResult(fmt.Sprintf("invalid args: %v", err)), nil
	}
	input, err := repoquery.ParseSearchRequest(raw)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	store, svc, err := r.openRepoQueryService(ctx)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	defer errors.Ignore(store.Close(), "close repoindex store")

	result, err := svc.SearchWithProjection(ctx, input)
	if err != nil {
		return errorResult(fmt.Sprintf("repo index search error: %v", err)), nil
	}
	return successResult(map[string]any{
		"count":   len(result.Nodes),
		"results": result.Nodes,
		"anchors": result.Anchors,
	}), nil
}

func (r *Registry) repoIndexExpand(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	normalized := map[string]any{}
	for k, v := range args {
		normalized[k] = v
	}
	if _, ok := normalized["seeds"]; !ok {
		if seeds := stringSliceFromAny(args["seed"]); len(seeds) > 0 {
			normalized["seeds"] = seeds
		}
	}
	if _, ok := normalized["edge_types"]; !ok {
		if edges := stringSliceFromAny(args["edges"]); len(edges) > 0 {
			normalized["edge_types"] = edges
		} else if edges := stringSliceFromAny(args["edge"]); len(edges) > 0 {
			normalized["edge_types"] = edges
		}
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return errorResult(fmt.Sprintf("invalid args: %v", err)), nil
	}
	input, err := repoquery.ParseExpandRequest(raw)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	store, svc, err := r.openRepoQueryService(ctx)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	defer errors.Ignore(store.Close(), "close repoindex store")

	result, err := svc.ExpandWithProjection(ctx, input)
	if err != nil {
		return errorResult(fmt.Sprintf("repo index expand error: %v", err)), nil
	}

	return successResult(map[string]any{
		"result":  result.Result,
		"anchors": result.Anchors,
	}), nil
}

func (r *Registry) repoIndexOpen(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return errorResult(fmt.Sprintf("invalid args: %v", err)), nil
	}
	input, err := repoquery.ParseOpenRequest(raw)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	store, svc, err := r.openRepoQueryService(ctx)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	defer errors.Ignore(store.Close(), "close repoindex store")

	result, err := svc.OpenWithProjection(ctx, input)
	if err != nil {
		return errorResult(fmt.Sprintf("repo index open error: %v", err)), nil
	}

	return successResult(map[string]any{
		"node":   result.Node,
		"anchor": result.Anchor,
	}), nil
}

func (r *Registry) repoIndexDagGrep(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return errorResult(fmt.Sprintf("invalid args: %v", err)), nil
	}
	input, err := repoquery.ParseDAGGrepRequest(raw)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	store, svc, err := r.openRepoQueryService(ctx)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	defer errors.Ignore(store.Close(), "close repoindex store")

	result, err := svc.DAGGrepWithProjection(ctx, input)
	if err != nil {
		return errorResult(fmt.Sprintf("repo index dag_grep error: %v", err)), nil
	}
	output := map[string]any{
		"result":  result.Result,
		"anchors": result.Anchors,
	}
	if len(result.Rendered) > 0 {
		for format, rendered := range result.Rendered {
			if rendered == "" {
				continue
			}
			output["rendered"] = rendered
			output["rendered_format"] = format
			break
		}
	}
	if rendered := repoquery.RenderDAG(result.Result, input.Render); rendered != "" && len(result.Rendered) == 0 {
		output["rendered"] = rendered
	}
	return successResult(output), nil
}

func (r *Registry) openRepoQueryService(ctx context.Context) (*repoindex.Store, *repoquery.QueryService, error) {
	if r.openRepoIndexStore == nil {
		return nil, nil, fmt.Errorf("repo index store not configured")
	}
	store, err := r.openRepoIndexStore(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("open repo index store: %w", err)
	}
	return store, repoquery.NewQueryService(repoindex.NewQueryEngine(store)), nil
}

func stringSliceFromAny(value any) []string {
	switch v := value.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			trimmed := strings.TrimSpace(item)
			if trimmed == "" {
				continue
			}
			out = append(out, trimmed)
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}
