package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	tooling "github.com/jkatigb/agentctl/internal/tooling"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
	"github.com/jkatigb/agentctl/internal/platform/errors"
)

type repoInlineMode string

const (
	repoInlineAuto          repoInlineMode = "auto"
	repoInlineFull          repoInlineMode = "full"
	repoInlinePreview       repoInlineMode = "preview"
	repoInlineArtifactOnly  repoInlineMode = "artifact_only"
	repoPreviewResults                     = 20
	repoPreviewAnchors                     = 12
	repoPreviewNodes                       = 40
	repoPreviewEdges                       = 80
	repoPreviewSeeds                       = 10
	repoPreviewDocLimit                    = 240
	repoPreviewSummaryLimit                = 180
	repoPreviewTrailLimit                  = 20
)

// registerRepoIndexTools registers repo index tools for agents.
func (r *Registry) registerRepoIndexTools() error {
	if r.openRepoIndexStore == nil {
		return nil
	}

	searchTool := tooling.NewFuncTool(
		repoindex.ToolSearch,
		"Search the repo index for nodes that match a short natural-language or symbol-name query. Avoid slash-heavy path strings.",
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
				"inline_mode": {
					Type:        "string",
					Description: "How much search detail to inline: auto, full, preview, or artifact_only",
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
				"inline_mode": {
					Type:        "string",
					Description: "How much graph detail to inline: auto, full, preview, or artifact_only",
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
		"Search and expand the repo index into a compact explanation subgraph using short natural-language or symbol-name queries. Avoid slash-heavy path strings.",
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
				"inline_mode": {
					Type:        "string",
					Description: "How much graph detail to inline: auto, full, preview, or artifact_only",
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
	return successResult(previewRepoIndexSearchOutput(result, repoInlineModeFromArgs(args))), nil
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
	return successResult(previewRepoIndexExpandOutput(result, repoInlineModeFromArgs(args))), nil
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
	return successResult(previewRepoIndexDAGOutput(output, repoInlineModeFromArgs(args))), nil
}

func repoInlineModeFromArgs(args map[string]any) repoInlineMode {
	if value, ok := args["inline_mode"].(string); ok {
		switch repoInlineMode(strings.ToLower(strings.TrimSpace(value))) {
		case repoInlineFull, repoInlinePreview, repoInlineArtifactOnly:
			return repoInlineMode(strings.ToLower(strings.TrimSpace(value)))
		}
	}
	return repoInlineAuto
}

func compactRepoNode(node repoindex.Node) repoindex.Node {
	if node.Doc != "" {
		node.Doc = truncateRepoText(node.Doc, repoPreviewDocLimit)
	}
	if node.Summary != "" {
		node.Summary = truncateRepoText(node.Summary, repoPreviewSummaryLimit)
	}
	node.Meta = nil
	return node
}

func compactRepoAnchor(anchor repoquery.Anchor) repoquery.Anchor {
	if anchor.Summary != "" {
		anchor.Summary = truncateRepoText(anchor.Summary, repoPreviewSummaryLimit)
	}
	return anchor
}

func truncateRepoText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit]) + "..."
}

func previewRepoIndexSearchOutput(result repoquery.SearchOutput, mode repoInlineMode) map[string]any {
	preview := mode == repoInlinePreview || mode == repoInlineArtifactOnly
	if mode == repoInlineAuto && (len(result.Nodes) > repoPreviewResults || len(result.Anchors) > repoPreviewAnchors) {
		preview = true
	}
	if mode == repoInlineArtifactOnly {
		return map[string]any{
			"count":         len(result.Nodes),
			"results_total": len(result.Nodes),
			"anchors_total": len(result.Anchors),
			"inline_mode":   string(repoInlineArtifactOnly),
			"truncated":     true,
		}
	}
	nodes := result.Nodes
	anchors := result.Anchors
	if preview {
		if len(nodes) > repoPreviewResults {
			nodes = append([]repoindex.Node(nil), nodes[:repoPreviewResults]...)
		} else {
			nodes = append([]repoindex.Node(nil), nodes...)
		}
		for i := range nodes {
			nodes[i] = compactRepoNode(nodes[i])
		}
		if len(anchors) > repoPreviewAnchors {
			anchors = append([]repoquery.Anchor(nil), anchors[:repoPreviewAnchors]...)
		} else {
			anchors = append([]repoquery.Anchor(nil), anchors...)
		}
		for i := range anchors {
			anchors[i] = compactRepoAnchor(anchors[i])
		}
	}
	out := map[string]any{
		"count":         len(result.Nodes),
		"results":       nodes,
		"anchors":       anchors,
		"results_total": len(result.Nodes),
		"anchors_total": len(result.Anchors),
		"inline_mode":   map[bool]string{true: string(repoInlinePreview), false: string(repoInlineFull)}[preview],
	}
	if preview {
		out["truncated"] = len(nodes) < len(result.Nodes) || len(anchors) < len(result.Anchors)
	}
	return out
}

func previewRepoIndexExpandOutput(result repoquery.ExpandOutput, mode repoInlineMode) map[string]any {
	preview := mode == repoInlinePreview || mode == repoInlineArtifactOnly
	if mode == repoInlineAuto && (len(result.Result.Nodes) > repoPreviewNodes || len(result.Result.Edges) > repoPreviewEdges || len(result.Anchors) > repoPreviewAnchors) {
		preview = true
	}
	if mode == repoInlineArtifactOnly {
		return map[string]any{
			"result":           map[string]any{},
			"anchors_total":    len(result.Anchors),
			"node_count_total": len(result.Result.Nodes),
			"edge_count_total": len(result.Result.Edges),
			"inline_mode":      string(repoInlineArtifactOnly),
			"truncated":        true,
		}
	}
	nodes := result.Result.Nodes
	edges := result.Result.Edges
	trail := result.Result.Trail
	anchors := result.Anchors
	if preview {
		keep := map[string]struct{}{}
		if len(nodes) > repoPreviewNodes {
			nodes = append([]repoindex.Node(nil), nodes[:repoPreviewNodes]...)
		} else {
			nodes = append([]repoindex.Node(nil), nodes...)
		}
		for i := range nodes {
			nodes[i] = compactRepoNode(nodes[i])
			keep[nodes[i].ID] = struct{}{}
		}
		filteredEdges := make([]repoindex.Edge, 0, minInt(repoPreviewEdges, len(edges)))
		for _, edge := range edges {
			if _, ok := keep[edge.Src]; !ok {
				continue
			}
			if _, ok := keep[edge.Dst]; !ok {
				continue
			}
			filteredEdges = append(filteredEdges, edge)
			if len(filteredEdges) >= repoPreviewEdges {
				break
			}
		}
		edges = filteredEdges
		if len(trail) > repoPreviewTrailLimit {
			trail = append([]string(nil), trail[:repoPreviewTrailLimit]...)
		}
		if len(anchors) > repoPreviewAnchors {
			anchors = append([]repoquery.Anchor(nil), anchors[:repoPreviewAnchors]...)
		} else {
			anchors = append([]repoquery.Anchor(nil), anchors...)
		}
		for i := range anchors {
			anchors[i] = compactRepoAnchor(anchors[i])
		}
	}
	out := map[string]any{
		"result": map[string]any{
			"nodes": nodes,
			"edges": edges,
			"trail": trail,
		},
		"anchors":          anchors,
		"node_count_total": len(result.Result.Nodes),
		"edge_count_total": len(result.Result.Edges),
		"anchors_total":    len(result.Anchors),
		"inline_mode":      map[bool]string{true: string(repoInlinePreview), false: string(repoInlineFull)}[preview],
	}
	if preview {
		out["truncated"] = len(nodes) < len(result.Result.Nodes) || len(edges) < len(result.Result.Edges) || len(anchors) < len(result.Anchors)
	}
	return out
}

func previewRepoIndexDAGOutput(output map[string]any, mode repoInlineMode) map[string]any {
	result, _ := output["result"].(repoindex.DAGGrepResult)
	rendered, _ := output["rendered"].(string)
	preview := mode == repoInlinePreview || mode == repoInlineArtifactOnly
	if mode == repoInlineAuto && (len(result.Graph.Nodes) > repoPreviewNodes || len(result.Graph.Edges) > repoPreviewEdges || len(result.Seeds) > repoPreviewSeeds) {
		preview = true
	}
	if mode == repoInlineArtifactOnly {
		return map[string]any{
			"inline_mode":      string(repoInlineArtifactOnly),
			"node_count_total": result.Stats.NodeCount,
			"edge_count_total": result.Stats.EdgeCount,
			"anchors_total":    len(outputAnchors(output)),
			"truncated":        true,
		}
	}
	if preview {
		trimmed := result
		if len(trimmed.Seeds) > repoPreviewSeeds {
			trimmed.Seeds = append([]repoindex.ScoredNode(nil), trimmed.Seeds[:repoPreviewSeeds]...)
		}
		nodeByID := make(map[string]repoindex.Node, len(trimmed.Graph.Nodes))
		for _, node := range trimmed.Graph.Nodes {
			nodeByID[node.ID] = compactRepoNode(node)
		}
		type layerItem struct {
			id    string
			layer int
		}
		items := make([]layerItem, 0, len(trimmed.DAG.Layers))
		for id, layer := range trimmed.DAG.Layers {
			items = append(items, layerItem{id: id, layer: layer})
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].layer == items[j].layer {
				return items[i].id < items[j].id
			}
			return items[i].layer < items[j].layer
		})
		keep := make(map[string]struct{}, repoPreviewNodes)
		nodes := make([]repoindex.Node, 0, repoPreviewNodes)
		layers := make(map[string]int, repoPreviewNodes)
		for _, item := range items {
			if len(nodes) >= repoPreviewNodes {
				break
			}
			if node, ok := nodeByID[item.id]; ok {
				nodes = append(nodes, node)
				keep[item.id] = struct{}{}
				layers[item.id] = item.layer
			}
		}
		filterEdges := func(src []repoindex.Edge, limit int) []repoindex.Edge {
			outEdges := make([]repoindex.Edge, 0, minInt(limit, len(src)))
			for _, edge := range src {
				if _, ok := keep[edge.Src]; !ok {
					continue
				}
				if _, ok := keep[edge.Dst]; !ok {
					continue
				}
				outEdges = append(outEdges, edge)
				if len(outEdges) >= limit {
					break
				}
			}
			return outEdges
		}
		trimmed.Graph.Nodes = nodes
		trimmed.Graph.Edges = filterEdges(trimmed.Graph.Edges, repoPreviewEdges)
		trimmed.DAG.Layers = layers
		trimmed.DAG.Edges = filterEdges(trimmed.DAG.Edges, repoPreviewEdges)
		trimmed.DAG.BackEdges = filterEdges(trimmed.DAG.BackEdges, repoPreviewEdges)
		trimmed.Stats.NodeCount = len(nodes)
		trimmed.Stats.EdgeCount = len(trimmed.Graph.Edges)
		out := map[string]any{
			"result":           result,
			"rendered":         rendered,
			"node_count_total": result.Stats.NodeCount,
			"edge_count_total": result.Stats.EdgeCount,
			"anchors_total":    len(outputAnchors(output)),
			"inline_mode":      string(repoInlinePreview),
			"truncated":        true,
		}
		out["result"] = trimmed
		if anchors := outputAnchors(output); len(anchors) > 0 {
			if len(anchors) > repoPreviewAnchors {
				anchors = append([]repoquery.Anchor(nil), anchors[:repoPreviewAnchors]...)
			} else {
				anchors = append([]repoquery.Anchor(nil), anchors...)
			}
			for i := range anchors {
				anchors[i] = compactRepoAnchor(anchors[i])
			}
			out["anchors"] = anchors
		}
		return out
	}
	output["inline_mode"] = string(repoInlineFull)
	output["node_count_total"] = result.Stats.NodeCount
	output["edge_count_total"] = result.Stats.EdgeCount
	output["anchors_total"] = len(outputAnchors(output))
	return output
}

func outputAnchors(output map[string]any) []repoquery.Anchor {
	if anchors, ok := output["anchors"].([]repoquery.Anchor); ok {
		return anchors
	}
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
