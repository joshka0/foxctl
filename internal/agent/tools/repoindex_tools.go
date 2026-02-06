package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	tooling "github.com/jkatigb/agentctl/internal/tooling"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/platform/errors"
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
	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return errorResult("query is required"), nil
	}
	limit := intArg(args, 20, "limit")
	if limit <= 0 {
		limit = 20
	}
	store, engine, err := r.openRepoIndexEngine(ctx)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	defer errors.Ignore(store.Close(), "close repoindex store")

	results, err := engine.Search(ctx, query, limit)
	if err != nil {
		return errorResult(fmt.Sprintf("repo index search error: %v", err)), nil
	}
	return successResult(map[string]any{
		"count":   len(results),
		"results": results,
	}), nil
}

func (r *Registry) repoIndexExpand(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	seeds := stringSliceFromAny(args["seeds"])
	if len(seeds) == 0 {
		seeds = stringSliceFromAny(args["seed"])
	}
	if len(seeds) == 0 {
		return errorResult("seeds are required"), nil
	}

	edgeTypes := stringSliceFromAny(args["edge_types"])
	if len(edgeTypes) == 0 {
		edgeTypes = stringSliceFromAny(args["edges"])
	}
	if len(edgeTypes) == 0 {
		edgeTypes = stringSliceFromAny(args["edge"])
	}
	edgeTypes = normalizeEdgeTypes(edgeTypes)
	parsedTypes, err := parseRepoIndexEdgeTypes(edgeTypes)
	if err != nil {
		return errorResult(fmt.Sprintf("invalid edge types: %v", err)), nil
	}

	direction := repoindex.DirOut
	if dir, ok := args["direction"].(string); ok && strings.TrimSpace(dir) != "" {
		direction = repoindex.Direction(strings.ToLower(strings.TrimSpace(dir)))
		if direction != repoindex.DirOut && direction != repoindex.DirIn {
			return errorResult(fmt.Sprintf("invalid direction: %s", dir)), nil
		}
	}

	depth := intArg(args, 1, "depth")
	budget := intArg(args, 50, "budget")
	perNodeCap := intArg(args, 50, "per_node_cap", "per_node")
	if depth <= 0 {
		depth = 1
	}
	if budget <= 0 {
		budget = 50
	}
	if perNodeCap <= 0 {
		perNodeCap = 50
	}

	store, engine, err := r.openRepoIndexEngine(ctx)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	defer errors.Ignore(store.Close(), "close repoindex store")

	result, err := engine.Expand(ctx, seeds, repoindex.ExpandOptions{
		Direction:  direction,
		EdgeTypes:  parsedTypes,
		Depth:      depth,
		Budget:     budget,
		PerNodeCap: perNodeCap,
	})
	if err != nil {
		return errorResult(fmt.Sprintf("repo index expand error: %v", err)), nil
	}

	return successResult(map[string]any{
		"result": result,
	}), nil
}

func (r *Registry) repoIndexOpen(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return errorResult("id is required"), nil
	}

	store, engine, err := r.openRepoIndexEngine(ctx)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	defer errors.Ignore(store.Close(), "close repoindex store")

	node, err := engine.Open(ctx, id)
	if err != nil {
		return errorResult(fmt.Sprintf("repo index open error: %v", err)), nil
	}

	return successResult(map[string]any{
		"node": node,
	}), nil
}

func (r *Registry) repoIndexDagGrep(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return errorResult("query is required"), nil
	}

	nodeKinds, err := parseRepoIndexNodeKinds(stringSliceFromAny(args["node_kinds"]))
	if err != nil {
		return errorResult(fmt.Sprintf("invalid node kinds: %v", err)), nil
	}

	edgeSets := stringSliceFromAny(args["edge_sets"])
	edgeTypes := stringSliceFromAny(args["edge_types"])
	parsedTypes, err := mergeEdgeTypes(edgeSets, edgeTypes)
	if err != nil {
		return errorResult(fmt.Sprintf("invalid edge sets/types: %v", err)), nil
	}

	direction := repoindex.DirOut
	if dir, ok := args["direction"].(string); ok && strings.TrimSpace(dir) != "" {
		direction = repoindex.Direction(strings.ToLower(strings.TrimSpace(dir)))
		if direction != repoindex.DirOut && direction != repoindex.DirIn {
			return errorResult(fmt.Sprintf("invalid direction: %s", dir)), nil
		}
	}

	includeAnchors := true
	if raw, ok := args["include_anchors"]; ok {
		if val, ok := raw.(bool); ok {
			includeAnchors = val
		}
	}

	mode, _ := args["mode"].(string)
	renderMode, _ := args["render"].(string)
	k := intArg(args, 10, "k")
	if k < 1 {
		k = 1
	}

	req := repoindex.DAGGrepRequest{
		Query:          query,
		Mode:           mode,
		K:              k,
		NodeKinds:      nodeKinds,
		EdgeTypes:      parsedTypes,
		Direction:      direction,
		Depth:          intArg(args, 2, "depth"),
		Budget:         intArg(args, 80, "budget"),
		PerNodeCap:     intArg(args, 20, "per_node_cap", "per_node"),
		IncludeAnchors: includeAnchors,
	}
	if req.Depth <= 0 {
		req.Depth = 2
	}
	if req.Budget <= 0 {
		req.Budget = 80
	}
	if req.PerNodeCap <= 0 {
		req.PerNodeCap = 20
	}

	store, engine, err := r.openRepoIndexEngine(ctx)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	defer errors.Ignore(store.Close(), "close repoindex store")

	result, err := engine.DAGGrep(ctx, req)
	if err != nil {
		return errorResult(fmt.Sprintf("repo index dag_grep error: %v", err)), nil
	}
	output := map[string]any{
		"result": result,
	}
	if rendered := renderRepoIndexDAG(result, renderMode); rendered != "" {
		output["rendered"] = rendered
	}
	return successResult(output), nil
}

func (r *Registry) openRepoIndexEngine(ctx context.Context) (*repoindex.Store, *repoindex.QueryEngine, error) {
	if r.openRepoIndexStore == nil {
		return nil, nil, fmt.Errorf("repo index store not configured")
	}
	store, err := r.openRepoIndexStore(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("open repo index store: %w", err)
	}
	engine := repoindex.NewQueryEngine(store)
	return store, engine, nil
}

func parseRepoIndexEdgeTypes(values []string) ([]repoindex.EdgeType, error) {
	if len(values) == 0 {
		return nil, nil
	}
	allowed := map[string]repoindex.EdgeType{
		string(repoindex.EdgeContains):        repoindex.EdgeContains,
		string(repoindex.EdgeImports):         repoindex.EdgeImports,
		string(repoindex.EdgeRefersTo):        repoindex.EdgeRefersTo,
		string(repoindex.EdgeCalls):           repoindex.EdgeCalls,
		string(repoindex.EdgeImplements):      repoindex.EdgeImplements,
		string(repoindex.EdgeEmbeds):          repoindex.EdgeEmbeds,
		string(repoindex.EdgeTests):           repoindex.EdgeTests,
		string(repoindex.EdgeHasKeyword):      repoindex.EdgeHasKeyword,
		string(repoindex.EdgeHasOutputField):  repoindex.EdgeHasOutputField,
		string(repoindex.EdgeTouchesResource): repoindex.EdgeTouchesResource,
		string(repoindex.EdgeEmitsEvent):      repoindex.EdgeEmitsEvent,
		string(repoindex.EdgeDocRelated):      repoindex.EdgeDocRelated,
		string(repoindex.EdgeDocFlow):         repoindex.EdgeDocFlow,
	}
	var types []repoindex.EdgeType
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		edgeType, ok := allowed[upper]
		if !ok {
			return nil, fmt.Errorf("unknown edge type: %s", value)
		}
		types = append(types, edgeType)
	}
	return types, nil
}

func parseRepoIndexNodeKinds(values []string) ([]repoindex.NodeKind, error) {
	if len(values) == 0 {
		return nil, nil
	}
	kinds := make([]repoindex.NodeKind, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		switch trimmed {
		case "symbol":
			kinds = append(kinds, repoindex.NodeSymbol)
		case "file":
			kinds = append(kinds, repoindex.NodeFile)
		case "package":
			kinds = append(kinds, repoindex.NodePackage)
		case "concept":
			kinds = append(kinds, repoindex.NodeConcept)
		default:
			return nil, fmt.Errorf("unknown node kind: %s", value)
		}
	}
	return kinds, nil
}

func edgeTypesFromSets(sets []string) ([]repoindex.EdgeType, error) {
	if len(sets) == 0 {
		return nil, nil
	}
	var types []repoindex.EdgeType
	for _, set := range sets {
		trimmed := strings.ToLower(strings.TrimSpace(set))
		if trimmed == "" {
			continue
		}
		switch trimmed {
		case "structural":
			types = append(types, repoindex.EdgeSetStructural...)
		case "doc":
			types = append(types, repoindex.EdgeSetDoc...)
		case "all":
			types = append(types, repoindex.EdgeSetStructural...)
			types = append(types, repoindex.EdgeSetDoc...)
		default:
			return nil, fmt.Errorf("unknown edge set: %s", set)
		}
	}
	return uniqueEdgeTypes(types), nil
}

func mergeEdgeTypes(edgeSets, edgeTypes []string) ([]repoindex.EdgeType, error) {
	typesFromSets, err := edgeTypesFromSets(edgeSets)
	if err != nil {
		return nil, err
	}
	parsedTypes, err := parseRepoIndexEdgeTypes(normalizeEdgeTypes(edgeTypes))
	if err != nil {
		return nil, err
	}
	if len(typesFromSets) == 0 && len(parsedTypes) == 0 {
		return repoindex.EdgeSetStructural, nil
	}
	types := append(typesFromSets, parsedTypes...)
	return uniqueEdgeTypes(types), nil
}

func uniqueEdgeTypes(values []repoindex.EdgeType) []repoindex.EdgeType {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[repoindex.EdgeType]struct{}, len(values))
	out := make([]repoindex.EdgeType, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeEdgeTypes(values []string) []string {
	var result []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			result = append(result, part)
		}
	}
	return result
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

func renderRepoIndexDAG(result repoindex.DAGGrepResult, mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "tree":
		return renderRepoIndexTree(result)
	case "mermaid":
		return renderRepoIndexMermaid(result)
	default:
		return ""
	}
}

func renderRepoIndexTree(result repoindex.DAGGrepResult) string {
	if len(result.Graph.Nodes) == 0 {
		return ""
	}
	nodeLabels := make(map[string]string, len(result.Graph.Nodes))
	for _, node := range result.Graph.Nodes {
		nodeLabels[node.ID] = repoIndexNodeLabel(node)
	}
	layerBuckets := make(map[int][]string)
	maxLayer := 0
	for id, layer := range result.DAG.Layers {
		layerBuckets[layer] = append(layerBuckets[layer], id)
		if layer > maxLayer {
			maxLayer = layer
		}
	}
	var b strings.Builder
	for layer := 0; layer <= maxLayer; layer++ {
		ids := layerBuckets[layer]
		if len(ids) == 0 {
			continue
		}
		sort.Strings(ids)
		b.WriteString(fmt.Sprintf("Layer %d:\n", layer))
		for _, id := range ids {
			label := nodeLabels[id]
			if label == "" {
				label = id
			}
			b.WriteString("  - ")
			b.WriteString(label)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func renderRepoIndexMermaid(result repoindex.DAGGrepResult) string {
	if len(result.DAG.Edges) == 0 {
		return ""
	}
	nodeLabels := make(map[string]string, len(result.Graph.Nodes))
	for _, node := range result.Graph.Nodes {
		nodeLabels[node.ID] = repoIndexNodeLabel(node)
	}
	var b strings.Builder
	b.WriteString("graph TD\n")
	for _, edge := range result.DAG.Edges {
		src := edge.Src
		dst := edge.Dst
		srcLabel := nodeLabels[src]
		if srcLabel == "" {
			srcLabel = src
		}
		dstLabel := nodeLabels[dst]
		if dstLabel == "" {
			dstLabel = dst
		}
		b.WriteString(fmt.Sprintf("  \"%s\"[\"%s\"] --> \"%s\"[\"%s\"]\n",
			escapeMermaidID(src), escapeMermaidLabel(srcLabel),
			escapeMermaidID(dst), escapeMermaidLabel(dstLabel),
		))
	}
	return strings.TrimSpace(b.String())
}

func repoIndexNodeLabel(node repoindex.Node) string {
	switch node.Kind {
	case repoindex.NodeSymbol:
		if node.Name != "" && node.File != "" {
			return fmt.Sprintf("%s (%s)", node.Name, node.File)
		}
		if node.Name != "" {
			return node.Name
		}
	case repoindex.NodeFile:
		if node.File != "" {
			return node.File
		}
	case repoindex.NodePackage:
		if node.Pkg != "" {
			return node.Pkg
		}
	case repoindex.NodeConcept:
		if node.Name != "" {
			return node.Name
		}
	}
	if node.ID != "" {
		return node.ID
	}
	return "unknown"
}

func escapeMermaidID(value string) string {
	return strings.ReplaceAll(value, "\"", "'")
}

func escapeMermaidLabel(value string) string {
	value = strings.ReplaceAll(value, "\"", "'")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func intArg(args map[string]any, fallback int, keys ...string) int {
	for _, key := range keys {
		if v, ok := args[key]; ok {
			switch val := v.(type) {
			case int:
				return val
			case int64:
				return int(val)
			case float64:
				return int(val)
			}
		}
	}
	return fallback
}
