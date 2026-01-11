// Package main implements the graph skill for dependency graph operations.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/graph"
)

const command = "graph"

type input struct {
	Operation  string          `json:"operation"`
	Workspace  string          `json:"workspace"`
	AddNode    *addNodeRequest `json:"add_node"`
	AddEdge    *addEdgeRequest `json:"add_edge"`
	Query      *queryRequest   `json:"query"`
	Neighbors  *neighborsReq   `json:"neighbors"`
	TopNodes   *topNodesReq    `json:"top_nodes"`
	DeleteNode *deleteNodeReq  `json:"delete_node"`
	DeleteEdge *deleteEdgeReq  `json:"delete_edge"`
	Cleanup    *cleanupReq     `json:"cleanup"`
}

type addNodeRequest struct {
	NodeID      string            `json:"node_id"`
	NodeType    string            `json:"node_type"`
	Title       string            `json:"title"`
	CurrentPath string            `json:"current_path"`
	Metadata    map[string]string `json:"metadata"`
}

type addEdgeRequest struct {
	FromID   string            `json:"from_id"`
	FromType string            `json:"from_type"`
	ToID     string            `json:"to_id"`
	ToType   string            `json:"to_type"`
	EdgeType string            `json:"edge_type"`
	Weight   float64           `json:"weight"`
	TTLDays  *int              `json:"ttl_days"`
	Metadata map[string]string `json:"metadata"`
}

type queryRequest struct {
	NodeID    string   `json:"node_id"`
	EdgeTypes []string `json:"edge_types"`
	Direction string   `json:"direction"` // "from", "to", "both"
}

type neighborsReq struct {
	NodeID    string   `json:"node_id"`
	Direction string   `json:"direction"` // "in", "out", "both"
	EdgeTypes []string `json:"edge_types"`
	Limit     int      `json:"limit"`
}

type topNodesReq struct {
	NodeType string  `json:"node_type"`
	MinRank  float64 `json:"min_rank"`
	Limit    int     `json:"limit"`
}

type deleteNodeReq struct {
	NodeID string `json:"node_id"`
}

type deleteEdgeReq struct {
	EdgeID string `json:"edge_id"`
}

type cleanupReq struct {
	ExpiredEdges  bool `json:"expired_edges"`
	DanglingEdges bool `json:"dangling_edges"`
	RecalcDegrees bool `json:"recalc_degrees"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Use workspace from input or from runner context
	workspace := in.Workspace
	if workspace == "" {
		workspace = rc.Workspace
	}

	// Open graph store
	store, err := graph.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return fmt.Errorf("open graph store: %w", err)
	}
	defer func() { errs.Ignore(store.Close(), "close graph store") }()

	switch in.Operation {
	case "add_node":
		return handleAddNode(ctx, rc, store, workspace, in.AddNode)
	case "add_edge":
		return handleAddEdge(ctx, rc, store, workspace, in.AddEdge)
	case "query":
		return handleQuery(ctx, rc, store, workspace, in.Query)
	case "neighbors":
		return handleNeighbors(ctx, rc, store, workspace, in.Neighbors)
	case "top_nodes":
		return handleTopNodes(ctx, rc, store, workspace, in.TopNodes)
	case "delete_node":
		return handleDeleteNode(ctx, rc, store, workspace, in.DeleteNode)
	case "delete_edge":
		return handleDeleteEdge(ctx, rc, store, in.DeleteEdge)
	case "stats":
		return handleStats(ctx, rc, store, workspace)
	case "cleanup":
		return handleCleanup(ctx, rc, store, workspace, in.Cleanup)
	default:
		return fmt.Errorf("unknown operation: %s", in.Operation)
	}
}

func handleAddNode(ctx context.Context, rc *skillmain.RunContext, store *graph.SQLiteStore, workspace string, req *addNodeRequest) error {
	if req == nil {
		return fmt.Errorf("add_node request required")
	}

	node := graph.Node{
		Workspace:   workspace,
		NodeID:      req.NodeID,
		NodeType:    graph.NodeType(req.NodeType),
		Title:       req.Title,
		CurrentPath: req.CurrentPath,
		LastSeen:    time.Now().UTC(),
		Metadata:    req.Metadata,
	}

	if err := store.UpsertNode(ctx, node); err != nil {
		return fmt.Errorf("upsert node: %w", err)
	}

	data := map[string]any{
		"node_id":   node.NodeID,
		"node_type": node.NodeType,
		"workspace": workspace,
		"created":   true,
	}
	return skillout.Emit(rc, "graph.add_node", data)
}

func handleAddEdge(ctx context.Context, rc *skillmain.RunContext, store *graph.SQLiteStore, workspace string, req *addEdgeRequest) error {
	if req == nil {
		return fmt.Errorf("add_edge request required")
	}

	weight := req.Weight
	if weight == 0 {
		weight = 1.0
	}

	edge := graph.Edge{
		Workspace: workspace,
		FromID:    req.FromID,
		FromType:  graph.NodeType(req.FromType),
		ToID:      req.ToID,
		ToType:    graph.NodeType(req.ToType),
		EdgeType:  graph.EdgeType(req.EdgeType),
		Weight:    weight,
		TTLDays:   req.TTLDays,
		CreatedAt: time.Now().UTC(),
		Metadata:  req.Metadata,
	}

	if err := store.UpsertEdge(ctx, edge); err != nil {
		return fmt.Errorf("upsert edge: %w", err)
	}

	data := map[string]any{
		"from_id":   edge.FromID,
		"to_id":     edge.ToID,
		"edge_type": edge.EdgeType,
		"workspace": workspace,
		"created":   true,
	}
	return skillout.Emit(rc, "graph.add_edge", data)
}

func handleQuery(ctx context.Context, rc *skillmain.RunContext, store *graph.SQLiteStore, workspace string, req *queryRequest) error {
	if req == nil {
		return fmt.Errorf("query request required")
	}

	var edgeTypes []graph.EdgeType
	for _, et := range req.EdgeTypes {
		edgeTypes = append(edgeTypes, graph.EdgeType(et))
	}

	var edges []graph.Edge
	var err error

	switch req.Direction {
	case "from":
		edges, err = store.GetEdgesFrom(ctx, workspace, req.NodeID, edgeTypes)
	case "to":
		edges, err = store.GetEdgesTo(ctx, workspace, req.NodeID, edgeTypes)
	case "both", "":
		fromEdges, err1 := store.GetEdgesFrom(ctx, workspace, req.NodeID, edgeTypes)
		toEdges, err2 := store.GetEdgesTo(ctx, workspace, req.NodeID, edgeTypes)
		if err1 != nil {
			err = err1
		} else if err2 != nil {
			err = err2
		} else {
			edges = append(fromEdges, toEdges...)
		}
	default:
		return fmt.Errorf("invalid direction: %s", req.Direction)
	}

	if err != nil {
		return fmt.Errorf("query edges: %w", err)
	}

	// Convert to output format
	type edgeOutput struct {
		ID        string            `json:"id"`
		FromID    string            `json:"from_id"`
		FromType  string            `json:"from_type"`
		ToID      string            `json:"to_id"`
		ToType    string            `json:"to_type"`
		EdgeType  string            `json:"edge_type"`
		Weight    float64           `json:"weight"`
		CreatedAt time.Time         `json:"created_at"`
		Metadata  map[string]string `json:"metadata,omitempty"`
	}

	output := make([]edgeOutput, 0, len(edges))
	for _, e := range edges {
		output = append(output, edgeOutput{
			ID:        e.ID,
			FromID:    e.FromID,
			FromType:  string(e.FromType),
			ToID:      e.ToID,
			ToType:    string(e.ToType),
			EdgeType:  string(e.EdgeType),
			Weight:    e.Weight,
			CreatedAt: e.CreatedAt,
			Metadata:  e.Metadata,
		})
	}

	data := map[string]any{
		"node_id":   req.NodeID,
		"edges":     output,
		"count":     len(output),
		"workspace": workspace,
	}
	return skillout.Emit(rc, "graph.query", data)
}

func handleNeighbors(ctx context.Context, rc *skillmain.RunContext, store *graph.SQLiteStore, workspace string, req *neighborsReq) error {
	if req == nil {
		return fmt.Errorf("neighbors request required")
	}

	var edgeTypes []graph.EdgeType
	for _, et := range req.EdgeTypes {
		edgeTypes = append(edgeTypes, graph.EdgeType(et))
	}

	limit := req.Limit
	if limit == 0 {
		limit = 20
	}

	neighbors, err := store.GetNeighbors(ctx, workspace, req.NodeID, graph.NeighborOptions{
		Direction: req.Direction,
		EdgeTypes: edgeTypes,
		Limit:     limit,
	})
	if err != nil {
		return fmt.Errorf("get neighbors: %w", err)
	}

	type neighborOutput struct {
		NodeID    string            `json:"node_id"`
		NodeType  string            `json:"node_type"`
		Title     string            `json:"title"`
		PageRank  float64           `json:"pagerank"`
		EdgeType  string            `json:"edge_type"`
		EdgeID    string            `json:"edge_id"`
		Direction string            `json:"direction"`
		Metadata  map[string]string `json:"metadata,omitempty"`
	}

	output := make([]neighborOutput, 0, len(neighbors))
	for _, n := range neighbors {
		direction := "outgoing"
		if n.Edge.ToID == req.NodeID {
			direction = "incoming"
		}
		output = append(output, neighborOutput{
			NodeID:    n.Node.NodeID,
			NodeType:  string(n.Node.NodeType),
			Title:     n.Node.Title,
			PageRank:  n.Node.PageRank,
			EdgeType:  string(n.Edge.EdgeType),
			EdgeID:    n.Edge.ID,
			Direction: direction,
			Metadata:  n.Node.Metadata,
		})
	}

	data := map[string]any{
		"node_id":   req.NodeID,
		"neighbors": output,
		"count":     len(output),
		"workspace": workspace,
	}
	return skillout.Emit(rc, "graph.neighbors", data)
}

func handleTopNodes(ctx context.Context, rc *skillmain.RunContext, store *graph.SQLiteStore, workspace string, req *topNodesReq) error {
	if req == nil {
		req = &topNodesReq{}
	}

	limit := req.Limit
	if limit == 0 {
		limit = 10
	}

	opts := graph.TopNodesOptions{
		Workspace: workspace,
		Limit:     limit,
		MinRank:   req.MinRank,
	}

	if req.NodeType != "" {
		nt := graph.NodeType(req.NodeType)
		opts.NodeType = &nt
	}

	nodes, err := store.TopNodes(ctx, opts)
	if err != nil {
		return fmt.Errorf("get top nodes: %w", err)
	}

	type nodeOutput struct {
		NodeID    string            `json:"node_id"`
		NodeType  string            `json:"node_type"`
		Title     string            `json:"title"`
		PageRank  float64           `json:"pagerank"`
		InDegree  int               `json:"in_degree"`
		OutDegree int               `json:"out_degree"`
		Metadata  map[string]string `json:"metadata,omitempty"`
	}

	output := make([]nodeOutput, 0, len(nodes))
	for _, n := range nodes {
		output = append(output, nodeOutput{
			NodeID:    n.NodeID,
			NodeType:  string(n.NodeType),
			Title:     n.Title,
			PageRank:  n.PageRank,
			InDegree:  n.InDegree,
			OutDegree: n.OutDegree,
			Metadata:  n.Metadata,
		})
	}

	data := map[string]any{
		"nodes":     output,
		"count":     len(output),
		"workspace": workspace,
	}
	return skillout.Emit(rc, "graph.top_nodes", data)
}

func handleDeleteNode(ctx context.Context, rc *skillmain.RunContext, store *graph.SQLiteStore, workspace string, req *deleteNodeReq) error {
	if req == nil {
		return fmt.Errorf("delete_node request required")
	}

	if err := store.DeleteNode(ctx, workspace, req.NodeID); err != nil {
		return fmt.Errorf("delete node: %w", err)
	}

	data := map[string]any{
		"node_id":   req.NodeID,
		"workspace": workspace,
		"deleted":   true,
	}
	return skillout.Emit(rc, "graph.delete_node", data)
}

func handleDeleteEdge(ctx context.Context, rc *skillmain.RunContext, store *graph.SQLiteStore, req *deleteEdgeReq) error {
	if req == nil {
		return fmt.Errorf("delete_edge request required")
	}

	if err := store.DeleteEdge(ctx, req.EdgeID); err != nil {
		return fmt.Errorf("delete edge: %w", err)
	}

	data := map[string]any{
		"edge_id": req.EdgeID,
		"deleted": true,
	}
	return skillout.Emit(rc, "graph.delete_edge", data)
}

func handleStats(ctx context.Context, rc *skillmain.RunContext, store *graph.SQLiteStore, workspace string) error {
	stats, err := store.Stats(ctx, workspace)
	if err != nil {
		return fmt.Errorf("get stats: %w", err)
	}

	data := map[string]any{
		"workspace":       workspace,
		"total_nodes":     stats.Nodes.TotalNodes,
		"total_edges":     stats.Edges.TotalEdges,
		"nodes_by_type":   stats.Nodes.ByType,
		"edges_by_type":   stats.Edges.ByType,
		"avg_pagerank":    stats.Nodes.AvgPageRank,
		"max_pagerank":    stats.Nodes.MaxPageRank,
		"avg_in_degree":   stats.Nodes.AvgInDegree,
		"avg_out_degree":  stats.Nodes.AvgOutDegree,
		"avg_edge_weight": stats.Edges.AvgWeight,
		"database_path":   stats.Path,
	}
	return skillout.Emit(rc, "graph.stats", data)
}

func handleCleanup(ctx context.Context, rc *skillmain.RunContext, store *graph.SQLiteStore, workspace string, req *cleanupReq) error {
	if req == nil {
		req = &cleanupReq{}
	}

	var expiredCount, danglingCount int
	var err error

	if req.ExpiredEdges {
		expiredCount, err = store.CleanupExpiredEdges(ctx)
		if err != nil {
			return fmt.Errorf("cleanup expired edges: %w", err)
		}
	}

	if req.DanglingEdges {
		danglingCount, err = store.CleanupDanglingEdges(ctx, workspace)
		if err != nil {
			return fmt.Errorf("cleanup dangling edges: %w", err)
		}
	}

	if req.RecalcDegrees {
		if err := store.RecalculateDegrees(ctx, workspace); err != nil {
			return fmt.Errorf("recalculate degrees: %w", err)
		}
	}

	data := map[string]any{
		"workspace":              workspace,
		"expired_edges_removed":  expiredCount,
		"dangling_edges_removed": danglingCount,
		"degrees_recalculated":   req.RecalcDegrees,
	}
	return skillout.Emit(rc, "graph.cleanup", data)
}
