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
// - Purpose: Find repo graph nodes using FTS with syntax and OR fallback
// - Flow: run FTS → on syntax error retry quoted → on zero results retry OR
// - FailureModes: FTS query errors, store errors
// - Related: Store.SearchFTS, quoteFTSQuery, buildFallbackCandidates
// - Keywords: repo_index_search, fts5, query, nodes, SearchFTS
func (q *QueryEngine) Search(ctx context.Context, query string, limit int) ([]Node, error) {
	if q == nil || q.store == nil {
		return nil, ErrNotFound
	}
	candidates := buildFallbackCandidates(query)
	if len(candidates) == 0 {
		return nil, nil
	}
	return searchWithFallback(ctx, candidates, limit, q.store.SearchFTS, query)
}

// SearchScored performs an FTS search over nodes and returns BM25 scores.
// Lower BM25 is better; callers should normalize as needed.
func (q *QueryEngine) SearchScored(ctx context.Context, query string, limit int) ([]ScoredNode, error) {
	if q == nil || q.store == nil {
		return nil, ErrNotFound
	}
	candidates := buildFallbackCandidates(query)
	if len(candidates) == 0 {
		return nil, nil
	}
	return searchWithFallback(ctx, candidates, limit, q.store.SearchFTSScored, query)
}

// searchWithFallback tries each candidate query in order. It advances to the
// next candidate when the current one returns zero results (for multi-word
// queries) or a syntax error.
func searchWithFallback[T any](
	ctx context.Context,
	candidates []string,
	limit int,
	searchFn func(context.Context, string, int) ([]T, error),
	originalQuery string,
) ([]T, error) {
	var lastErr error
	isMulti := isMultiWordQuery(originalQuery)

	for i, candidate := range candidates {
		results, err := searchFn(ctx, candidate, limit)
		if err == nil {
			if len(results) > 0 || !isMulti || i == len(candidates)-1 {
				return results, nil
			}
			continue // zero results on multi-word, try next candidate
		}
		if !isFTSSyntaxError(err) {
			return nil, err // non-syntax errors fail fast
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

// buildFallbackCandidates returns candidate queries in priority order:
// 1. Raw trimmed query (existing AND behavior)
// 2. Quoted fallback (existing syntax-error repair)
// 3. OR fallback (for multi-word queries only)
func buildFallbackCandidates(query string) []string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil
	}
	candidates := make([]string, 0, 6)
	addCandidate := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		for _, existing := range candidates {
			if existing == candidate {
				return
			}
		}
		candidates = append(candidates, candidate)
	}
	addCandidate(trimmed)

	quoted := quoteFTSQuery(trimmed)
	addCandidate(quoted)

	if isMultiWordQuery(trimmed) {
		if orQuery := buildOrFallbackQuery(trimmed); orQuery != "" && orQuery != trimmed && orQuery != quoted {
			addCandidate(orQuery)
		}
	}

	sanitized := sanitizeFTSQuery(trimmed)
	if sanitized != "" && sanitized != trimmed {
		addCandidate(sanitized)
		addCandidate(quoteFTSQuery(sanitized))
		if isMultiWordQuery(sanitized) {
			addCandidate(buildOrFallbackQuery(sanitized))
		}
	}
	return candidates
}

func sanitizeFTSQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(query))
	for _, r := range query {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// isMultiWordQuery returns true if the query contains more than one word.
func isMultiWordQuery(query string) bool {
	return len(strings.Fields(strings.TrimSpace(query))) > 1
}

// buildOrFallbackQuery converts "a b c" to "a OR b OR c", stripping FTS5 operators.
func buildOrFallbackQuery(query string) string {
	raw := strings.Fields(strings.TrimSpace(query))
	terms := make([]string, 0, len(raw))
	for _, term := range raw {
		t := strings.TrimSpace(term)
		if t == "" {
			continue
		}
		upper := strings.ToUpper(t)
		if upper == "AND" || upper == "OR" || upper == "NOT" {
			continue
		}
		t = strings.Trim(t, "\"'")
		if t == "" {
			continue
		}
		terms = append(terms, quoteFTSQuery(t))
	}
	if len(terms) < 2 {
		return ""
	}
	return strings.Join(terms, " OR ")
}

func isFTSSyntaxError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "fts5") ||
		strings.Contains(msg, "syntax error") ||
		strings.Contains(msg, "no such column") ||
		strings.Contains(msg, "unterminated string")
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

// ResolveFileNodes resolves exact repo-relative file paths to file nodes.
func (q *QueryEngine) ResolveFileNodes(ctx context.Context, paths []string) ([]Node, error) {
	if q == nil || q.store == nil {
		return nil, ErrNotFound
	}
	return q.store.ResolveFileNodes(ctx, paths)
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
