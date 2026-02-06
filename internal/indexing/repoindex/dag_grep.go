package repoindex

import (
	"container/heap"
	"context"
	"math"
	"sort"
	"strings"
)

// DAGGrepRequest configures repo index DAG_grep.
type DAGGrepRequest struct {
	Query          string
	Mode           string
	K              int
	NodeKinds      []NodeKind
	EdgeTypes      []EdgeType
	Direction      Direction
	Depth          int
	Budget         int
	PerNodeCap     int
	IncludeAnchors bool
}

// DAGView captures a layered DAG view plus back edges.
type DAGView struct {
	Layers    map[string]int `json:"layers"`
	Edges     []Edge         `json:"edges"`
	BackEdges []Edge         `json:"back_edges,omitempty"`
}

// DAGGrepResult captures seeds, graph, and DAG view.
type DAGGrepResult struct {
	Query    string       `json:"query"`
	Mode     string       `json:"mode"`
	ModeUsed string       `json:"mode_used"`
	Seeds    []ScoredNode `json:"seeds"`
	Graph    ExpandResult `json:"graph"`
	DAG      DAGView      `json:"dag"`
	Stats    struct {
		SeedCount int `json:"seed_count"`
		NodeCount int `json:"node_count"`
		EdgeCount int `json:"edge_count"`
	} `json:"stats"`
	Warnings []string `json:"warnings,omitempty"`
}

// DAGGrep runs a scored search and returns a small explanation subgraph.
func (q *QueryEngine) DAGGrep(ctx context.Context, req DAGGrepRequest) (DAGGrepResult, error) {
	var result DAGGrepResult
	if q == nil || q.store == nil {
		return result, ErrNotFound
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		return result, nil
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "hybrid"
	}
	result.Query = query
	result.Mode = mode
	result.ModeUsed = "fts"

	if req.K <= 0 {
		req.K = 10
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
	if req.Direction == "" {
		req.Direction = DirOut
	}

	kindFilter := make(map[NodeKind]struct{})
	for _, kind := range req.NodeKinds {
		kindFilter[kind] = struct{}{}
	}

	searchLimit := req.K * 3
	scored, err := q.SearchScored(ctx, query, searchLimit)
	if err != nil {
		return result, err
	}

	var seeds []ScoredNode
	for _, item := range scored {
		if len(kindFilter) > 0 {
			if _, ok := kindFilter[item.Node.Kind]; !ok {
				continue
			}
		}
		item.Score = normalizeBM25(item.Score)
		seeds = append(seeds, item)
		if len(seeds) >= req.K {
			break
		}
	}
	if len(seeds) == 0 {
		result.Seeds = nil
		result.Graph = ExpandResult{}
		result.DAG = DAGView{Layers: map[string]int{}}
		return result, nil
	}

	sort.Slice(seeds, func(i, j int) bool { return seeds[i].Score > seeds[j].Score })
	result.Seeds = seeds

	nodes, edges, err := q.expandWeighted(ctx, seeds, req)
	if err != nil {
		return result, err
	}

	if req.IncludeAnchors {
		nodes, edges = q.addAnchorNodes(ctx, nodes, edges)
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Src != edges[j].Src {
			return edges[i].Src < edges[j].Src
		}
		if edges[i].Dst != edges[j].Dst {
			return edges[i].Dst < edges[j].Dst
		}
		return string(edges[i].Type) < string(edges[j].Type)
	})

	result.Graph = ExpandResult{Nodes: nodes, Edges: edges}
	result.DAG = buildDAGView(seeds, edges, req.Direction)
	result.Stats.SeedCount = len(seeds)
	result.Stats.NodeCount = len(nodes)
	result.Stats.EdgeCount = len(edges)

	if req.Mode != "" && mode != "fts" {
		result.Warnings = append(result.Warnings, "semantic/hybrid modes currently use FTS fallback")
	}

	return result, nil
}

func normalizeBM25(score float64) float64 {
	if score < 0 {
		score = 0
	}
	return 1.0 / (1.0 + score)
}

type frontierItem struct {
	id    string
	score float64
	depth int
}

type frontierHeap []frontierItem

func (h frontierHeap) Len() int           { return len(h) }
func (h frontierHeap) Less(i, j int) bool { return h[i].score > h[j].score }
func (h frontierHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *frontierHeap) Push(x any)        { *h = append(*h, x.(frontierItem)) }
func (h *frontierHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func (q *QueryEngine) expandWeighted(ctx context.Context, seeds []ScoredNode, req DAGGrepRequest) ([]Node, []Edge, error) {
	seenNodes := make(map[string]float64)
	seenEdges := make(map[string]Edge)
	queue := make(frontierHeap, 0, len(seeds))

	for _, seed := range seeds {
		if seed.Node.ID == "" {
			continue
		}
		seenNodes[seed.Node.ID] = seed.Score
		queue = append(queue, frontierItem{id: seed.Node.ID, score: seed.Score, depth: 0})
	}
	heap.Init(&queue)

	for queue.Len() > 0 && len(seenNodes) < req.Budget {
		item := heap.Pop(&queue).(frontierItem)
		if item.depth >= req.Depth {
			continue
		}

		edges, err := q.fetchEdges(ctx, item.id, ExpandOptions{
			Direction:  req.Direction,
			EdgeTypes:  req.EdgeTypes,
			Depth:      req.Depth,
			Budget:     req.Budget,
			PerNodeCap: req.PerNodeCap,
		})
		if err != nil {
			return nil, nil, err
		}

		for _, edge := range edges {
			key := edgeKey(edge)
			if _, ok := seenEdges[key]; !ok {
				seenEdges[key] = edge
			}
			neighbor := edgeNeighbor(edge, item.id, req.Direction)
			if neighbor == "" {
				continue
			}
			if _, ok := seenNodes[neighbor]; ok {
				continue
			}
			if len(seenNodes) >= req.Budget {
				break
			}
			weight := edge.Weight
			if weight <= 0 {
				weight = 1.0
			}
			nextScore := item.score * weight * math.Pow(0.85, float64(item.depth+1))
			seenNodes[neighbor] = nextScore
			heap.Push(&queue, frontierItem{id: neighbor, score: nextScore, depth: item.depth + 1})
		}
	}

	nodeIDs := make([]string, 0, len(seenNodes))
	for id := range seenNodes {
		nodeIDs = append(nodeIDs, id)
	}
	nodes, err := q.store.GetNodes(ctx, nodeIDs)
	if err != nil {
		return nil, nil, err
	}

	edges := make([]Edge, 0, len(seenEdges))
	for _, edge := range seenEdges {
		edges = append(edges, edge)
	}

	return nodes, edges, nil
}

func (q *QueryEngine) addAnchorNodes(ctx context.Context, nodes []Node, edges []Edge) ([]Node, []Edge) {
	if q == nil || q.store == nil {
		return nodes, edges
	}
	repoKey := q.store.RepoKey()
	if repoKey == "" {
		return nodes, edges
	}

	nodeByID := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}
	edgeMap := make(map[string]Edge, len(edges))
	for _, edge := range edges {
		edgeMap[edgeKey(edge)] = edge
	}

	var anchorIDs []string
	for _, node := range nodes {
		switch node.Kind {
		case NodeSymbol:
			if node.File == "" || node.Pkg == "" {
				continue
			}
			fileID := FileID(repoKey, node.Pkg, node.File)
			if _, ok := nodeByID[fileID]; !ok {
				anchorIDs = append(anchorIDs, fileID)
			}
			edge := Edge{Src: fileID, Dst: node.ID, Type: EdgeContains, Weight: 1.0}
			edgeMap[edgeKey(edge)] = edge
		case NodeFile:
			if node.Pkg == "" {
				continue
			}
			pkgID := PackageID(repoKey, node.Pkg)
			if _, ok := nodeByID[pkgID]; !ok {
				anchorIDs = append(anchorIDs, pkgID)
			}
			edge := Edge{Src: pkgID, Dst: node.ID, Type: EdgeContains, Weight: 1.0}
			edgeMap[edgeKey(edge)] = edge
		}
	}

	if len(anchorIDs) > 0 {
		anchors, err := q.store.GetNodes(ctx, anchorIDs)
		if err == nil {
			for _, node := range anchors {
				if _, ok := nodeByID[node.ID]; ok {
					continue
				}
				nodeByID[node.ID] = node
			}
		}
	}

	nodes = nodes[:0]
	for _, node := range nodeByID {
		nodes = append(nodes, node)
	}
	edges = edges[:0]
	for _, edge := range edgeMap {
		edges = append(edges, edge)
	}

	return nodes, edges
}

func buildDAGView(seeds []ScoredNode, edges []Edge, direction Direction) DAGView {
	layers := make(map[string]int)
	queue := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		if seed.Node.ID == "" {
			continue
		}
		if _, ok := layers[seed.Node.ID]; ok {
			continue
		}
		layers[seed.Node.ID] = 0
		queue = append(queue, seed.Node.ID)
	}

	adj := make(map[string][]string)
	for _, edge := range edges {
		src := edge.Src
		dst := edge.Dst
		if direction == DirIn {
			src, dst = dst, src
		}
		adj[src] = append(adj[src], dst)
	}

	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		currentLayer := layers[nodeID]
		for _, next := range adj[nodeID] {
			if _, ok := layers[next]; ok {
				continue
			}
			layers[next] = currentLayer + 1
			queue = append(queue, next)
		}
	}

	var forward []Edge
	var back []Edge
	for _, edge := range edges {
		src := edge.Src
		dst := edge.Dst
		if direction == DirIn {
			src, dst = dst, src
		}
		srcLayer, okSrc := layers[src]
		dstLayer, okDst := layers[dst]
		if okSrc && okDst && srcLayer < dstLayer {
			forward = append(forward, edge)
			continue
		}
		back = append(back, edge)
	}

	return DAGView{
		Layers:    layers,
		Edges:     forward,
		BackEdges: back,
	}
}
