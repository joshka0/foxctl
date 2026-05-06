package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

// RepoIndexToolExecutor provides repo index tools for LLM access.
type RepoIndexToolExecutor struct {
	queryService *repoquery.QueryService
	workspaceID  string
}

// NewRepoIndexToolExecutor creates a new repo index tool executor.
//
// Index:
//   Purpose: Initialize repo index tool execution with workspace context
//   Keywords: repo_index, tool_executor, workspace_id, query_engine
//   Related: RepoIndexToolExecutor.Execute, repoindex.NewQueryEngine
//   Flow: create query engine → capture workspace ID → return executor
//   Resources: repoindex.Store
//   Events: none
//   OutputFields: RepoIndexToolExecutor
//
// [[domain:repoindex-tool-executor]]
func NewRepoIndexToolExecutor(store *repoindex.Store) *RepoIndexToolExecutor {
	var queryService *repoquery.QueryService
	workspaceID := ""
	if store != nil {
		queryService = repoquery.NewQueryService(repoindex.NewQueryEngine(store))
		workspaceID = store.RepoRoot()
	}
	return &RepoIndexToolExecutor{
		queryService: queryService,
		workspaceID:  workspaceID,
	}
}

// Execute implements ToolExecutor.
func (e *RepoIndexToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if e == nil || e.queryService == nil {
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

// executeSearch runs repo_index_search with validation and observability.
//
// Index:
//   Purpose: Execute repo index search and format tool output
//   Keywords: repo_index_search, query, result_count, repo_index, observability
//   Related: observability.WriteRepoIndexEvent, repoindex.QueryEngine.Search
//   Flow: parse args → validate query → search engine → emit event → marshal output
//   Resources: repoindex.QueryEngine
//   Events: repo_index events
//   OutputFields: JSON string
//
// [[domain:repoindex-search-tool]]
func (e *RepoIndexToolExecutor) executeSearch(ctx context.Context, args json.RawMessage) (string, error) {
	start := time.Now()
	input, err := repoquery.ParseSearchRequest(args)
	if err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolSearch,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		if strings.Contains(string(args), `"`) && err != nil {
			return "", fmt.Errorf("parse search args: %w", err)
		}
		return "", err
	}

	results, err := e.queryService.Search(ctx, input)
	ev := observability.RepoIndexEvent{
		Command:     repoindex.ToolSearch,
		Source:      "tool",
		QueryHash:   observability.HashQuestion(input.Query),
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
//   Purpose: Expand repo index graph from seed nodes
//   Keywords: repo_index_expand, seeds, edge_types, direction, repo_index
//   Related: repoindex.QueryEngine.Expand, parseRepoIndexEdgeTypes
//   Flow: parse args → validate seeds/edges → expand graph → emit event → marshal output
//   Resources: repoindex.QueryEngine
//   Events: repo_index events
//   OutputFields: JSON string
//
// [[domain:repoindex-expand-tool]]
func (e *RepoIndexToolExecutor) executeExpand(ctx context.Context, args json.RawMessage) (string, error) {
	start := time.Now()
	input, err := repoquery.ParseExpandRequest(args)
	if err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolExpand,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		if strings.Contains(string(args), `"`) && err != nil {
			return "", fmt.Errorf("parse expand args: %w", err)
		}
		return "", err
	}

	result, err := e.queryService.Expand(ctx, input)
	ev := observability.RepoIndexEvent{
		Command:     repoindex.ToolExpand,
		Source:      "tool",
		SeedCount:   len(input.Seeds),
		EdgeTypes:   repoquery.EdgeTypeValues(input.EdgeTypes),
		Direction:   string(input.Direction),
		Depth:       input.Depth,
		Budget:      input.Budget,
		PerNodeCap:  input.PerNodeCap,
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
//   Purpose: Open a repo index node by ID
//   Keywords: repo_index_open, node_id, repo_index, observability
//   Related: repoindex.QueryEngine.Open
//   Flow: parse args → validate ID → fetch node → emit event → marshal output
//   Resources: repoindex.QueryEngine
//   Events: repo_index events
//   OutputFields: JSON string
//
// [[domain:repoindex-open-tool]]
func (e *RepoIndexToolExecutor) executeOpen(ctx context.Context, args json.RawMessage) (string, error) {
	start := time.Now()
	input, err := repoquery.ParseOpenRequest(args)
	if err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolOpen,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		if strings.Contains(string(args), `"`) && err != nil {
			return "", fmt.Errorf("parse open args: %w", err)
		}
		return "", err
	}

	node, err := e.queryService.Open(ctx, input)
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
//   Purpose: Produce a compact explanation subgraph for a query
//   Keywords: repo_index_dag_grep, query, dag, graph, repo_index
//   Related: repoindex.QueryEngine.DAGGrep
//   Flow: parse args → search seeds → expand weighted graph → build DAG view → marshal output
//   Resources: repoindex.QueryEngine
//   Events: repo_index events
//   OutputFields: JSON string
//
// [[domain:repoindex-dag-grep-tool]]
func (e *RepoIndexToolExecutor) executeDagGrep(ctx context.Context, args json.RawMessage) (string, error) {
	start := time.Now()
	input, err := repoquery.ParseDAGGrepRequest(args)
	if err != nil {
		e.writeEvent(ctx, observability.RepoIndexEvent{
			Command:    repoindex.ToolDAGGrep,
			Source:     "tool",
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		if strings.Contains(string(args), `"`) && err != nil {
			return "", fmt.Errorf("parse dag_grep args: %w", err)
		}
		return "", err
	}

	result, err := e.queryService.DAGGrep(ctx, input)
	ev := observability.RepoIndexEvent{
		Command:     repoindex.ToolDAGGrep,
		Source:      "tool",
		QueryHash:   observability.HashQuestion(input.Query),
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
	if rendered := repoquery.RenderDAG(result, input.Render); rendered != "" {
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

func marshalToolOutput(output map[string]any) (string, error) {
	b, err := json.Marshal(output)
	if err != nil {
		return "", err
	}
	return string(b), nil
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
