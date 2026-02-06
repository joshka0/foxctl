package repoindex

import (
	"context"
	"strings"
)

// QueryEngine provides search and expansion operations over the repo index.
type QueryEngine struct {
	store *Store
}

// NewQueryEngine returns a new query engine.
func NewQueryEngine(store *Store) *QueryEngine {
	return &QueryEngine{store: store}
}

// Search performs an FTS search over nodes.
//
// Index:
// - Purpose: Find repo graph nodes using FTS with syntax fallback
// - Flow: run FTS → on syntax error retry with quoted query
// - FailureModes: FTS query errors, store errors
// - Related: Store.SearchFTS, quoteFTSQuery
// - Keywords: repo_index_search, fts5, query, nodes, SearchFTS
func (q *QueryEngine) Search(ctx context.Context, query string, limit int) ([]Node, error) {
	if q == nil || q.store == nil {
		return nil, ErrNotFound
	}
	results, err := q.store.SearchFTS(ctx, query, limit)
	if err == nil || !isFTSSyntaxError(err) {
		return results, err
	}
	fallback := quoteFTSQuery(query)
	if fallback == "" || fallback == query {
		return nil, err
	}
	results, retryErr := q.store.SearchFTS(ctx, fallback, limit)
	if retryErr != nil {
		return nil, retryErr
	}
	return results, nil
}

// SearchScored performs an FTS search over nodes and returns BM25 scores.
// Lower BM25 is better; callers should normalize as needed.
func (q *QueryEngine) SearchScored(ctx context.Context, query string, limit int) ([]ScoredNode, error) {
	if q == nil || q.store == nil {
		return nil, ErrNotFound
	}
	results, err := q.store.SearchFTSScored(ctx, query, limit)
	if err == nil || !isFTSSyntaxError(err) {
		return results, err
	}
	fallback := quoteFTSQuery(query)
	if fallback == "" || fallback == query {
		return nil, err
	}
	results, retryErr := q.store.SearchFTSScored(ctx, fallback, limit)
	if retryErr != nil {
		return nil, retryErr
	}
	return results, nil
}

func isFTSSyntaxError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "fts5") || strings.Contains(msg, "syntax error")
}

func quoteFTSQuery(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"") {
		return trimmed
	}
	trimmed = strings.ReplaceAll(trimmed, "\"", " ")
	return "\"" + trimmed + "\""
}

// Open returns a node by ID.
func (q *QueryEngine) Open(ctx context.Context, id string) (Node, error) {
	if q == nil || q.store == nil {
		return Node{}, ErrNotFound
	}
	return q.store.GetNode(ctx, id)
}

// Expand traverses the graph starting from seed node IDs.
//
// Index:
// - Purpose: Expand the repo graph from seeds with depth/budget limits
// - Flow: normalize options → BFS by depth → fetch edges → collect nodes/edges
// - FailureModes: edge fetch errors, store errors
// - Related: GetOutgoingEdges, GetIncomingEdges, Store.GetNodes
// - Keywords: repo_index_expand, seeds, depth, budget, edges, nodes
func (q *QueryEngine) Expand(ctx context.Context, seeds []string, opts ExpandOptions) (ExpandResult, error) {
	if q == nil || q.store == nil {
		return ExpandResult{}, ErrNotFound
	}
	if len(seeds) == 0 {
		return ExpandResult{}, nil
	}
	if opts.Depth <= 0 {
		opts.Depth = 1
	}
	if opts.Budget <= 0 {
		opts.Budget = 50
	}
	if opts.PerNodeCap <= 0 {
		opts.PerNodeCap = 50
	}
	if opts.Direction == "" {
		opts.Direction = DirOut
	}

	seenNodes := make(map[string]struct{})
	seenEdges := make(map[string]Edge)

	frontier := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		if seed == "" {
			continue
		}
		if _, ok := seenNodes[seed]; ok {
			continue
		}
		seenNodes[seed] = struct{}{}
		frontier = append(frontier, seed)
	}

	for depth := 0; depth < opts.Depth; depth++ {
		if len(seenNodes) >= opts.Budget {
			break
		}
		var next []string
		for _, nodeID := range frontier {
			if len(seenNodes) >= opts.Budget {
				break
			}
			edges, err := q.fetchEdges(ctx, nodeID, opts)
			if err != nil {
				return ExpandResult{}, err
			}
			for _, edge := range edges {
				key := edgeKey(edge)
				if _, ok := seenEdges[key]; !ok {
					seenEdges[key] = edge
				}
				neighbor := edgeNeighbor(edge, nodeID, opts.Direction)
				if neighbor == "" {
					continue
				}
				if _, ok := seenNodes[neighbor]; ok {
					continue
				}
				if len(seenNodes) >= opts.Budget {
					break
				}
				seenNodes[neighbor] = struct{}{}
				next = append(next, neighbor)
			}
		}
		frontier = next
		if len(frontier) == 0 {
			break
		}
	}

	nodeIDs := make([]string, 0, len(seenNodes))
	for id := range seenNodes {
		nodeIDs = append(nodeIDs, id)
	}
	nodes, err := q.store.GetNodes(ctx, nodeIDs)
	if err != nil {
		return ExpandResult{}, err
	}

	edges := make([]Edge, 0, len(seenEdges))
	for _, edge := range seenEdges {
		edges = append(edges, edge)
	}

	return ExpandResult{Nodes: nodes, Edges: edges}, nil
}

func (q *QueryEngine) fetchEdges(ctx context.Context, nodeID string, opts ExpandOptions) ([]Edge, error) {
	if opts.Direction == DirIn {
		return q.store.GetIncomingEdges(ctx, nodeID, opts.EdgeTypes, opts.PerNodeCap)
	}
	return q.store.GetOutgoingEdges(ctx, nodeID, opts.EdgeTypes, opts.PerNodeCap)
}

func edgeNeighbor(edge Edge, nodeID string, dir Direction) string {
	if dir == DirIn {
		if edge.Src == nodeID {
			return edge.Dst
		}
		return edge.Src
	}
	if edge.Dst == nodeID {
		return edge.Src
	}
	return edge.Dst
}

func edgeKey(edge Edge) string {
	return edge.Src + "|" + edge.Dst + "|" + string(edge.Type)
}
