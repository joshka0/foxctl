package repoindex

import (
	"context"
	"sort"
	"strings"
)

const (
	defaultTracePathMaxDepth    = 5
	defaultNavigationLimit      = 50
	defaultNavigationPerNodeCap = 50
	defaultBlastRadiusMaxDepth  = 3
)

// TracePathOptions controls shortest directed path search between two nodes.
type TracePathOptions struct {
	SrcID      string
	DstID      string
	MaxDepth   int
	PerNodeCap int
	EdgeTypes  []EdgeType
}

// TracePathResult contains the first shortest path found by breadth-first traversal.
type TracePathResult struct {
	Found   bool   `json:"found"`
	PathLen int    `json:"path_len"`
	Nodes   []Node `json:"nodes,omitempty"`
	Edges   []Edge `json:"edges,omitempty"`
}

// ContextSection is one named slice of repo graph context.
type ContextSection struct {
	Name  string `json:"name"`
	Nodes []Node `json:"nodes,omitempty"`
	Edges []Edge `json:"edges,omitempty"`
}

// SmartContextOptions controls one-hop context sections around a node.
type SmartContextOptions struct {
	NodeID string
	Limit  int
}

// SmartContextResult contains a node plus typed relationship sections.
type SmartContextResult struct {
	Node     Node             `json:"node"`
	Sections []ContextSection `json:"sections,omitempty"`
}

// BlastRadiusOptions controls bounded forward impact expansion from a node.
type BlastRadiusOptions struct {
	NodeID     string
	MaxDepth   int
	Limit      int
	PerNodeCap int
	EdgeTypes  []EdgeType
}

// BlastRadiusResult contains forward impact graph plus supporting direct sections.
type BlastRadiusResult struct {
	Origin   Node             `json:"origin"`
	Graph    ExpandResult     `json:"graph"`
	Layers   map[string]int   `json:"layers,omitempty"`
	Sections []ContextSection `json:"sections,omitempty"`
}

// DefaultTracePathEdgeTypes returns the explicit edge set used by trace path.
func DefaultTracePathEdgeTypes() []EdgeType {
	return DeduplicateEdgeTypes(ConcatEdgeSets(
		EdgeSetStructural,
		[]EdgeType{EdgeDescribedBy, EdgeVerifiedBy, EdgeEnforces},
		EdgeSetEmpirical,
	))
}

// DefaultBlastRadiusEdgeTypes returns the explicit edge set used by blast radius.
func DefaultBlastRadiusEdgeTypes() []EdgeType {
	return []EdgeType{EdgeContains, EdgeCalls, EdgeTests, EdgeDescribedBy, EdgeCoChangesWith}
}

// TracePath finds a shortest directed path from SrcID to DstID using stored repoindex edges.
func (q *QueryEngine) TracePath(ctx context.Context, opts TracePathOptions) (TracePathResult, error) {
	if q == nil || q.store == nil {
		return TracePathResult{}, ErrNotFound
	}
	opts = normalizeTracePathOptions(opts)
	if opts.SrcID == "" || opts.DstID == "" {
		return TracePathResult{}, ErrNotFound
	}
	srcNode, err := q.store.GetNode(ctx, opts.SrcID)
	if err != nil {
		return TracePathResult{}, err
	}
	if opts.SrcID == opts.DstID {
		return TracePathResult{Found: true, Nodes: []Node{srcNode}}, nil
	}
	if _, err := q.store.GetNode(ctx, opts.DstID); err != nil {
		return TracePathResult{}, err
	}

	type pathState struct {
		nodeID string
		ids    []string
		edges  []Edge
	}
	queue := []pathState{{nodeID: opts.SrcID, ids: []string{opts.SrcID}}}
	seen := map[string]struct{}{opts.SrcID: {}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if len(current.edges) >= opts.MaxDepth {
			continue
		}
		edges, err := q.store.GetOutgoingEdges(ctx, current.nodeID, opts.EdgeTypes, opts.PerNodeCap)
		if err != nil {
			return TracePathResult{}, err
		}
		for _, edge := range edges {
			if edge.Src != current.nodeID {
				continue
			}
			nextIDs := append(append([]string(nil), current.ids...), edge.Dst)
			nextEdges := append(append([]Edge(nil), current.edges...), edge)
			if edge.Dst == opts.DstID {
				nodes, err := q.nodesInOrder(ctx, nextIDs)
				if err != nil {
					return TracePathResult{}, err
				}
				return TracePathResult{
					Found:   true,
					PathLen: len(nextEdges),
					Nodes:   nodes,
					Edges:   nextEdges,
				}, nil
			}
			if _, ok := seen[edge.Dst]; ok {
				continue
			}
			seen[edge.Dst] = struct{}{}
			queue = append(queue, pathState{nodeID: edge.Dst, ids: nextIDs, edges: nextEdges})
		}
	}

	return TracePathResult{}, nil
}

// SmartContext returns stable one-hop sections around a single node.
func (q *QueryEngine) SmartContext(ctx context.Context, opts SmartContextOptions) (SmartContextResult, error) {
	if q == nil || q.store == nil {
		return SmartContextResult{}, ErrNotFound
	}
	opts = normalizeSmartContextOptions(opts)
	if opts.NodeID == "" {
		return SmartContextResult{}, ErrNotFound
	}
	node, err := q.store.GetNode(ctx, opts.NodeID)
	if err != nil {
		return SmartContextResult{}, err
	}

	sections := []ContextSection{{Name: "self", Nodes: []Node{node}}}
	sectionSpecs := []struct {
		name      string
		direction Direction
		types     []EdgeType
	}{
		{name: "contains_in", direction: DirIn, types: []EdgeType{EdgeContains}},
		{name: "contains_children", direction: DirOut, types: []EdgeType{EdgeContains}},
		{name: "callees", direction: DirOut, types: []EdgeType{EdgeCalls}},
		{name: "callers", direction: DirIn, types: []EdgeType{EdgeCalls}},
		{name: "docs_concepts", direction: DirOut, types: []EdgeType{EdgeDescribedBy}},
		{name: "co_changes", direction: DirOut, types: []EdgeType{EdgeCoChangesWith}},
	}
	for _, spec := range sectionSpecs {
		edges, err := q.edgesForDirection(ctx, opts.NodeID, spec.direction, spec.types, opts.Limit)
		if err != nil {
			return SmartContextResult{}, err
		}
		section, err := q.contextSection(ctx, spec.name, edges)
		if err != nil {
			return SmartContextResult{}, err
		}
		sections = append(sections, section)
	}

	return SmartContextResult{Node: node, Sections: sections}, nil
}

// BlastRadius returns a bounded forward graph plus direct incoming-call context.
func (q *QueryEngine) BlastRadius(ctx context.Context, opts BlastRadiusOptions) (BlastRadiusResult, error) {
	if q == nil || q.store == nil {
		return BlastRadiusResult{}, ErrNotFound
	}
	opts = normalizeBlastRadiusOptions(opts)
	if opts.NodeID == "" {
		return BlastRadiusResult{}, ErrNotFound
	}
	origin, err := q.store.GetNode(ctx, opts.NodeID)
	if err != nil {
		return BlastRadiusResult{}, err
	}

	seenNodes := map[string]int{opts.NodeID: 0}
	seenEdges := make(map[string]Edge)
	frontier := []string{opts.NodeID}
	for depth := 0; depth < opts.MaxDepth && len(frontier) > 0 && len(seenNodes) < opts.Limit; depth++ {
		var next []string
		for _, nodeID := range frontier {
			if len(seenNodes) >= opts.Limit {
				break
			}
			edges, err := q.store.GetOutgoingEdges(ctx, nodeID, opts.EdgeTypes, opts.PerNodeCap)
			if err != nil {
				return BlastRadiusResult{}, err
			}
			for _, edge := range edges {
				if _, ok := seenNodes[edge.Dst]; ok {
					key := edgeKey(edge)
					if _, seenEdge := seenEdges[key]; !seenEdge {
						seenEdges[key] = edge
					}
					continue
				}
				if len(seenNodes) >= opts.Limit {
					break
				}
				seenNodes[edge.Dst] = depth + 1
				key := edgeKey(edge)
				if _, ok := seenEdges[key]; !ok {
					seenEdges[key] = edge
				}
				next = append(next, edge.Dst)
			}
		}
		frontier = next
	}

	nodeIDs := make([]string, 0, len(seenNodes))
	for id := range seenNodes {
		nodeIDs = append(nodeIDs, id)
	}
	nodes, err := q.store.GetNodes(ctx, nodeIDs)
	if err != nil {
		return BlastRadiusResult{}, err
	}
	sortNodesByLayer(nodes, seenNodes)
	edges := sortedEdges(seenEdges)

	var sections []ContextSection
	incoming, err := q.store.GetIncomingEdges(ctx, opts.NodeID, []EdgeType{EdgeCalls}, opts.Limit)
	if err != nil {
		return BlastRadiusResult{}, err
	}
	section, err := q.contextSection(ctx, "incoming_call", incoming)
	if err != nil {
		return BlastRadiusResult{}, err
	}
	sections = append(sections, section)

	return BlastRadiusResult{
		Origin:   origin,
		Graph:    ExpandResult{Nodes: nodes, Edges: edges},
		Layers:   seenNodes,
		Sections: sections,
	}, nil
}

func normalizeTracePathOptions(opts TracePathOptions) TracePathOptions {
	opts.SrcID = strings.TrimSpace(opts.SrcID)
	opts.DstID = strings.TrimSpace(opts.DstID)
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultTracePathMaxDepth
	}
	if opts.PerNodeCap <= 0 {
		opts.PerNodeCap = defaultNavigationPerNodeCap
	}
	if len(opts.EdgeTypes) == 0 {
		opts.EdgeTypes = DefaultTracePathEdgeTypes()
	} else {
		opts.EdgeTypes = DeduplicateEdgeTypes(opts.EdgeTypes)
	}
	return opts
}

func normalizeSmartContextOptions(opts SmartContextOptions) SmartContextOptions {
	opts.NodeID = strings.TrimSpace(opts.NodeID)
	if opts.Limit <= 0 {
		opts.Limit = defaultNavigationLimit
	}
	return opts
}

func normalizeBlastRadiusOptions(opts BlastRadiusOptions) BlastRadiusOptions {
	opts.NodeID = strings.TrimSpace(opts.NodeID)
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultBlastRadiusMaxDepth
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultNavigationLimit
	}
	if opts.PerNodeCap <= 0 {
		opts.PerNodeCap = defaultNavigationPerNodeCap
	}
	if len(opts.EdgeTypes) == 0 {
		opts.EdgeTypes = DefaultBlastRadiusEdgeTypes()
	} else {
		opts.EdgeTypes = DeduplicateEdgeTypes(opts.EdgeTypes)
	}
	return opts
}

func (q *QueryEngine) edgesForDirection(ctx context.Context, nodeID string, direction Direction, types []EdgeType, limit int) ([]Edge, error) {
	if direction == DirIn {
		return q.store.GetIncomingEdges(ctx, nodeID, types, limit)
	}
	return q.store.GetOutgoingEdges(ctx, nodeID, types, limit)
}

func (q *QueryEngine) contextSection(ctx context.Context, name string, edges []Edge) (ContextSection, error) {
	ids := make([]string, 0, len(edges)*2)
	seen := make(map[string]struct{}, len(edges)*2)
	for _, edge := range edges {
		for _, id := range []string{edge.Src, edge.Dst} {
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	nodes, err := q.store.GetNodes(ctx, ids)
	if err != nil {
		return ContextSection{}, err
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edgeSortLess(edges[i], edges[j]) })
	return ContextSection{Name: name, Nodes: nodes, Edges: edges}, nil
}

func (q *QueryEngine) nodesInOrder(ctx context.Context, ids []string) ([]Node, error) {
	nodes := make([]Node, 0, len(ids))
	for _, id := range ids {
		node, err := q.store.GetNode(ctx, id)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func sortedEdges(values map[string]Edge) []Edge {
	edges := make([]Edge, 0, len(values))
	for _, edge := range values {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool { return edgeSortLess(edges[i], edges[j]) })
	return edges
}

func edgeSortLess(a, b Edge) bool {
	if a.Src != b.Src {
		return a.Src < b.Src
	}
	if a.Dst != b.Dst {
		return a.Dst < b.Dst
	}
	return string(a.Type) < string(b.Type)
}

func sortNodesByLayer(nodes []Node, layers map[string]int) {
	sort.Slice(nodes, func(i, j int) bool {
		left, right := layers[nodes[i].ID], layers[nodes[j].ID]
		if left != right {
			return left < right
		}
		return nodes[i].ID < nodes[j].ID
	})
}
