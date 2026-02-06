package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/observability"
)

// RepoIndexToolExecutor provides repo index tools for LLM access.
type RepoIndexToolExecutor struct {
	engine      *repoindex.QueryEngine
	workspaceID string
}

// NewRepoIndexToolExecutor creates a new repo index tool executor.
//
// Index:
// - Purpose: Initialize repo index tool execution with workspace context
// - Flow: create query engine → capture workspace ID → return executor
// - SideEffects: constructs query engine
// - Related: RepoIndexToolExecutor.Execute, repoindex.NewQueryEngine
// - Keywords: repo_index, tool_executor, workspace_id, query_engine
func NewRepoIndexToolExecutor(store *repoindex.Store) *RepoIndexToolExecutor {
	var engine *repoindex.QueryEngine
	workspaceID := ""
	if store != nil {
		engine = repoindex.NewQueryEngine(store)
		workspaceID = store.RepoRoot()
	}
	return &RepoIndexToolExecutor{
		engine:      engine,
		workspaceID: workspaceID,
	}
}

// Execute implements ToolExecutor.
func (e *RepoIndexToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if e == nil || e.engine == nil {
		return "", errors.New("repoindex executor not configured")
	}

	switch name {
	case repoindex.ToolSearch, repoindex.ToolSearchLegacy:
		return e.executeSearch(ctx, args)
	case repoindex.ToolExpand, repoindex.ToolExpandLegacy:
		return e.executeExpand(ctx, args)
	case repoindex.ToolOpen, repoindex.ToolOpenLegacy:
		return e.executeOpen(ctx, args)
	case repoindex.ToolDAGGrep, repoindex.ToolDAGGrepLegacy:
		return e.executeDagGrep(ctx, args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// List implements ToolExecutor.
func (e *RepoIndexToolExecutor) List() []ToolDef {
	return repoIndexToolDefs()
}

type repoIndexSearchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

const (
	defaultRepoIndexDepth      = 1
	defaultRepoIndexBudget     = 50
	defaultRepoIndexPerNodeCap = 50
)

type repoIndexExpandInput struct {
	Seeds      []string `json:"seeds"`
	EdgeTypes  []string `json:"edge_types,omitempty"`
	Direction  string   `json:"direction,omitempty"`
	Depth      int      `json:"depth,omitempty"`
	Budget     int      `json:"budget,omitempty"`
	PerNodeCap int      `json:"per_node_cap,omitempty"`
}

type repoIndexOpenInput struct {
	ID string `json:"id"`
}

type repoIndexDagGrepInput struct {
	Query          string   `json:"query"`
	Mode           string   `json:"mode,omitempty"`
	K              int      `json:"k,omitempty"`
	NodeKinds      []string `json:"node_kinds,omitempty"`
	EdgeSets       []string `json:"edge_sets,omitempty"`
	EdgeTypes      []string `json:"edge_types,omitempty"`
	Direction      string   `json:"direction,omitempty"`
	Depth          int      `json:"depth,omitempty"`
	Budget         int      `json:"budget,omitempty"`
	PerNodeCap     int      `json:"per_node_cap,omitempty"`
	IncludeAnchors *bool    `json:"include_anchors,omitempty"`
	Render         string   `json:"render,omitempty"`
}

// executeSearch runs repo_index_search with validation and observability.
//
// Index:
// - Purpose: Execute repo index search and format tool output
// - Flow: parse args → validate query → search engine → emit event → marshal output
// - SideEffects: repo index queries; observability events
// - FailureModes: parse errors, missing query, engine errors
// - Observability: emits repo_index events
// - Related: observability.WriteRepoIndexEvent, repoindex.QueryEngine.Search
// - Keywords: repo_index_search, query, result_count, repo_index, observability
func (e *RepoIndexToolExecutor) executeSearch(ctx context.Context, args json.RawMessage) (string, error) {
	start := time.Now()
	var input repoIndexSearchInput
	if err := json.Unmarshal(args, &input); err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolSearch,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", fmt.Errorf("parse search args: %w", err)
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		err := errors.New("query is required")
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolSearch,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", err
	}
	if input.Limit <= 0 {
		input.Limit = 20
	}

	results, err := e.engine.Search(ctx, query, input.Limit)
	ev := observability.RepoIndexEvent{
		Command:     repoindex.ToolSearch,
		Source:      "tool",
		QueryHash:   observability.HashQuestion(query),
		ResultCount: len(results),
		DurationMS:  time.Since(start).Milliseconds(),
	}
	if err != nil {
		ev.Error = err.Error()
	}
	defer e.writeEvent(ctx, ev)
	if err != nil {
		return "", err
	}

	output := map[string]any{
		"count":   len(results),
		"results": results,
	}
	return marshalToolOutput(output)
}

// executeExpand runs repo_index_expand with validation and observability.
//
// Index:
// - Purpose: Expand repo index graph from seed nodes
// - Flow: parse args → validate seeds/edges → expand graph → emit event → marshal output
// - SideEffects: repo index queries; observability events
// - FailureModes: parse errors, invalid edges, engine errors
// - Observability: emits repo_index events
// - Related: repoindex.QueryEngine.Expand, parseRepoIndexEdgeTypes
// - Keywords: repo_index_expand, seeds, edge_types, direction, repo_index
func (e *RepoIndexToolExecutor) executeExpand(ctx context.Context, args json.RawMessage) (string, error) {
	start := time.Now()
	var input repoIndexExpandInput
	if err := json.Unmarshal(args, &input); err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolExpand,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", fmt.Errorf("parse expand args: %w", err)
	}
	if len(input.Seeds) == 0 {
		err := errors.New("seeds are required")
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolExpand,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", err
	}

	edgeTypes := normalizeRepoIndexEdgeTypes(input.EdgeTypes)
	parsedTypes, err := parseRepoIndexEdgeTypes(edgeTypes)
	if err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolExpand,
			Source:     "tool",
			SeedCount:  len(input.Seeds),
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", err
	}

	direction := repoindex.DirOut
	if input.Direction != "" {
		direction = repoindex.Direction(strings.ToLower(strings.TrimSpace(input.Direction)))
		if direction != repoindex.DirOut && direction != repoindex.DirIn {
			err := fmt.Errorf("invalid direction: %s", input.Direction)
			e.writeEvent(ctx, observability.RepoIndexEvent{
				Command:    repoindex.ToolExpand,
				Source:     "tool",
				SeedCount:  len(input.Seeds),
				EdgeTypes:  edgeTypesFrom(parsedTypes),
				Direction:  string(direction),
				DurationMS: time.Since(start).Milliseconds(),
				Error:      err.Error(),
			})
			return "", err
		}
	}

	depth := input.Depth
	if depth <= 0 {
		depth = defaultRepoIndexDepth
	}
	budget := input.Budget
	if budget <= 0 {
		budget = defaultRepoIndexBudget
	}
	perNodeCap := input.PerNodeCap
	if perNodeCap <= 0 {
		perNodeCap = defaultRepoIndexPerNodeCap
	}

	opts := repoindex.ExpandOptions{
		Direction:  direction,
		EdgeTypes:  parsedTypes,
		Depth:      depth,
		Budget:     budget,
		PerNodeCap: perNodeCap,
	}

	result, err := e.engine.Expand(ctx, input.Seeds, opts)
	ev := observability.RepoIndexEvent{
		Command:     repoindex.ToolExpand,
		Source:      "tool",
		SeedCount:   len(input.Seeds),
		EdgeTypes:   edgeTypesFrom(parsedTypes),
		Direction:   string(direction),
		Depth:       opts.Depth,
		Budget:      opts.Budget,
		PerNodeCap:  opts.PerNodeCap,
		ResultCount: len(result.Nodes),
		DurationMS:  time.Since(start).Milliseconds(),
	}
	if err != nil {
		ev.Error = err.Error()
	}
	defer e.writeEvent(ctx, ev)
	if err != nil {
		return "", err
	}

	output := map[string]any{
		"result": result,
	}
	return marshalToolOutput(output)
}

// executeOpen runs repo_index_open with validation and observability.
//
// Index:
// - Purpose: Open a repo index node by ID
// - Flow: parse args → validate ID → fetch node → emit event → marshal output
// - SideEffects: repo index queries; observability events
// - FailureModes: parse errors, missing ID, engine errors
// - Observability: emits repo_index events
// - Related: repoindex.QueryEngine.Open
// - Keywords: repo_index_open, node_id, repo_index, observability
func (e *RepoIndexToolExecutor) executeOpen(ctx context.Context, args json.RawMessage) (string, error) {
	start := time.Now()
	var input repoIndexOpenInput
	if err := json.Unmarshal(args, &input); err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolOpen,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", fmt.Errorf("parse open args: %w", err)
	}
	if strings.TrimSpace(input.ID) == "" {
		err := errors.New("id is required")
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolOpen,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", err
	}

	node, err := e.engine.Open(ctx, input.ID)
	ev := observability.RepoIndexEvent{
		Command:     repoindex.ToolOpen,
		Source:      "tool",
		NodeID:      input.ID,
		ResultCount: 1,
		DurationMS:  time.Since(start).Milliseconds(),
	}
	if err != nil {
		ev.Error = err.Error()
		if errors.Is(err, repoindex.ErrNotFound) {
			ev.ResultCount = 0
		}
	}
	defer e.writeEvent(ctx, ev)
	if err != nil {
		return "", err
	}

	output := map[string]any{
		"node": node,
	}
	return marshalToolOutput(output)
}

// executeDagGrep runs repo_index_dag_grep with validation and observability.
//
// Index:
// - Purpose: Produce a compact explanation subgraph for a query
// - Flow: parse args → search seeds → expand weighted graph → build DAG view → marshal output
// - SideEffects: repo index queries; observability events
// - FailureModes: parse errors, missing query, engine errors
// - Observability: emits repo_index events
// - Related: repoindex.QueryEngine.DAGGrep
// - Keywords: repo_index_dag_grep, query, dag, graph, repo_index
func (e *RepoIndexToolExecutor) executeDagGrep(ctx context.Context, args json.RawMessage) (string, error) {
	start := time.Now()
	var input repoIndexDagGrepInput
	if err := json.Unmarshal(args, &input); err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolDAGGrep,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", fmt.Errorf("parse dag_grep args: %w", err)
	}
	query := strings.TrimSpace(input.Query)
	if query == "" {
		err := errors.New("query is required")
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolDAGGrep,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", err
	}

	nodeKinds, err := parseRepoIndexNodeKinds(input.NodeKinds)
	if err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolDAGGrep,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", err
	}

	edgeTypes, err := mergeEdgeTypes(input.EdgeSets, input.EdgeTypes)
	if err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolDAGGrep,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return "", err
	}

	direction := repoindex.DirOut
	if input.Direction != "" {
		direction = repoindex.Direction(strings.ToLower(strings.TrimSpace(input.Direction)))
		if direction != repoindex.DirOut && direction != repoindex.DirIn {
			err := fmt.Errorf("invalid direction: %s", input.Direction)
			e.writeEvent(ctx, observability.RepoIndexEvent{
				Command:    repoindex.ToolDAGGrep,
				Source:     "tool",
				DurationMS: time.Since(start).Milliseconds(),
				Error:      err.Error(),
			})
			return "", err
		}
	}

	includeAnchors := true
	if input.IncludeAnchors != nil {
		includeAnchors = *input.IncludeAnchors
	}
	if input.Depth <= 0 {
		input.Depth = 2
	}
	if input.Budget <= 0 {
		input.Budget = 80
	}
	if input.PerNodeCap <= 0 {
		input.PerNodeCap = 20
	}
	k := input.K
	if k <= 0 {
		k = 1
	}

	req := repoindex.DAGGrepRequest{
		Query:          query,
		Mode:           input.Mode,
		K:              k,
		NodeKinds:      nodeKinds,
		EdgeTypes:      edgeTypes,
		Direction:      direction,
		Depth:          input.Depth,
		Budget:         input.Budget,
		PerNodeCap:     input.PerNodeCap,
		IncludeAnchors: includeAnchors,
	}

	result, err := e.engine.DAGGrep(ctx, req)
	ev := observability.RepoIndexEvent{
		Command:     repoindex.ToolDAGGrep,
		Source:      "tool",
		QueryHash:   observability.HashQuestion(query),
		ResultCount: len(result.Graph.Nodes),
		DurationMS:  time.Since(start).Milliseconds(),
	}
	if err != nil {
		ev.Error = err.Error()
	}
	defer e.writeEvent(ctx, ev)
	if err != nil {
		return "", err
	}

	output := map[string]any{
		"result": result,
	}
	if rendered := renderRepoIndexDAG(result, input.Render); rendered != "" {
		output["rendered"] = rendered
	}

	return marshalToolOutput(output)
}

func repoIndexToolDefs() []ToolDef {
	return []ToolDef{
		{
			Name:        repoindex.ToolSearch,
			Description: "Search the repo index for nodes that match a text query.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"FTS query string"},"limit":{"type":"integer","description":"Maximum results","default":20}},"required":["query"]}`),
		},
		{
			Name:        repoindex.ToolExpand,
			Description: "Expand the repo index graph from seed node IDs.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"seeds":{"type":"array","items":{"type":"string"},"description":"Seed node IDs"},"edge_types":{"type":"array","items":{"type":"string"},"description":"Edge types to traverse"},"direction":{"type":"string","enum":["out","in"],"description":"Traversal direction"},"depth":{"type":"integer","description":"Traversal depth","default":1},"budget":{"type":"integer","description":"Max nodes to return","default":50},"per_node_cap":{"type":"integer","description":"Max edges per node per hop","default":50}},"required":["seeds"]}`),
		},
		{
			Name:        repoindex.ToolOpen,
			Description: "Open a repo index node by ID.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Node ID"}},"required":["id"]}`),
		},
		{
			Name:        repoindex.ToolDAGGrep,
			Description: "Search and expand the repo index into a compact explanation subgraph.",
			Parameters: json.RawMessage(`{"type":"object","properties":{
				"query":{"type":"string","description":"Search query"},
				"mode":{"type":"string","enum":["fts","semantic","hybrid"]},
				"k":{"type":"integer","description":"Number of seed nodes (default 10)"},
				"node_kinds":{"type":"array","items":{"type":"string","enum":["symbol","file","package","concept"]}},
				"edge_sets":{"type":"array","items":{"type":"string","enum":["structural","doc","all"]}},
				"edge_types":{"type":"array","items":{"type":"string"}},
				"direction":{"type":"string","enum":["out","in"]},
				"depth":{"type":"integer","description":"Expansion depth"},
				"budget":{"type":"integer","description":"Max nodes to return"},
				"per_node_cap":{"type":"integer","description":"Max edges per node"},
				"include_anchors":{"type":"boolean","description":"Include file/package anchors"},
				"render":{"type":"string","enum":["none","tree","mermaid"]}
			},"required":["query"]}`),
		},
	}
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
	parsedTypes, err := parseRepoIndexEdgeTypes(normalizeRepoIndexEdgeTypes(edgeTypes))
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

func normalizeRepoIndexEdgeTypes(values []string) []string {
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

func marshalToolOutput(output map[string]any) (string, error) {
	b, err := json.Marshal(output)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func edgeTypesFrom(types []repoindex.EdgeType) []string {
	if len(types) == 0 {
		return nil
	}
	out := make([]string, 0, len(types))
	for _, edgeType := range types {
		out = append(out, string(edgeType))
	}
	return out
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

func (e *RepoIndexToolExecutor) writeEvent(ctx context.Context, ev observability.RepoIndexEvent) {
	if ev.Ts.IsZero() {
		ev.Ts = time.Now().UTC()
	}
	if ev.WorkspaceID == "" {
		ev.WorkspaceID = e.workspaceID
	}
	_ = observability.WriteRepoIndexEvent(ctx, ev)
}

const RepoIndexAskPrompt = "You are a repo index assistant. Use repo_index_search to find multiple relevant nodes, repo_index_expand to map relationships, and repo_index_open for details. Edge types include structural edges (CONTAINS, IMPORTS, REFERS_TO, CALLS, IMPLEMENTS, EMBEDS, TESTS) and doc/comment edges (HAS_KEYWORD, HAS_OUTPUT_FIELD, TOUCHES_RESOURCE, EMITS_EVENT, DOC_RELATED, DOC_FLOW). When answering, list up to 5 relevant files or symbols with node IDs and file paths, plus a 1-2 sentence summary (use node summaries when available). If a tool call fails, retry with valid arguments; if unsure, say so."

type RepoIndexAskConfig struct {
	Store         *repoindex.Store
	Question      string
	Provider      string
	Model         string
	APIKey        string
	MaxIterations int
	Timeout       time.Duration
	SystemPrompt  string
}

type RepoIndexAskResult struct {
	Output EngineOutput
}

// RunRepoIndexAsk runs a single-turn repo index query with a stateless LLM engine.
//
// Index:
// - Purpose: Execute a stateless repo index query workflow
// - Flow: validate config → build tool runner → configure engine → run LLM → return output
// - SideEffects: LLM API calls; repo index queries
// - FailureModes: invalid config, engine init errors, LLM/tool errors
// - Related: NewLLMChatEngine, NewRepoIndexToolExecutor, ToolRunner.Execute
// - Keywords: repo_index_ask, stateless, tool_runner, llm_chat, repo_index_search
func RunRepoIndexAsk(ctx context.Context, cfg RepoIndexAskConfig) (RepoIndexAskResult, error) {
	question := strings.TrimSpace(cfg.Question)
	if question == "" {
		return RepoIndexAskResult{}, errors.New("question is required")
	}
	if cfg.Store == nil {
		return RepoIndexAskResult{}, errors.New("repo index store is required")
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 12
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	systemPrompt := strings.TrimSpace(cfg.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = RepoIndexAskPrompt
	}

	toolExecutor := NewRepoIndexToolExecutor(cfg.Store)
	toolRunner := NewToolRunner(toolExecutor, nil, DefaultToolRunnerConfig())

	engineCfg := LLMChatConfig{
		Provider:      cfg.Provider,
		APIKey:        cfg.APIKey,
		Model:         cfg.Model,
		MaxIterations: cfg.MaxIterations,
		Timeout:       cfg.Timeout,
		StatelessMode: true,
	}

	llmEngine, err := NewLLMChatEngine(engineCfg)
	if err != nil {
		return RepoIndexAskResult{}, err
	}
	llmEngine.SetToolRunner(toolRunner)

	input := EngineInput{
		SystemPrompt: systemPrompt,
		Messages: []Message{
			NewUserMessage(question),
		},
		Tools: toolExecutor.List(),
	}

	output, err := llmEngine.Run(ctx, input)
	if err != nil {
		return RepoIndexAskResult{}, err
	}
	return RepoIndexAskResult{Output: output}, nil
}
