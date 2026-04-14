// Package main implements the graph/pagerank skill for computing PageRank scores.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/storage/graph"
	"gonum.org/v1/gonum/graph/network"
	"gonum.org/v1/gonum/graph/simple"
)

const command = "graph/pagerank"

// input defines the input parameters for graph/pagerank operations.
type input struct {
	Workspace     string  `json:"workspace"`
	DampingFactor float64 `json:"damping_factor"` // Default: 0.85
	Tolerance     float64 `json:"tolerance"`      // Default: 1e-6
}

// main is the skill entry point for graph/pagerank.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates PageRank computation for graph nodes using gonum library.
//
// Index:
// - Purpose: Compute PageRank scores for all nodes in a graph workspace using gonum algorithms
// - Flow: validate input → open store → load nodes/edges → build directed graph → compute PageRank → update store → emit results
// - SideEffects: graph database updates; degree recalculation; PageRank score modifications
// - FailureModes: database errors, graph building failures, computation errors
// - Observability: emits computation statistics, top nodes, performance metrics
// - Related: gonum PageRank algorithm, graph database operations, bulk updates
// - Keywords: graph/pagerank, pagerank_algorithm, graph_analysis, node_ranking, gonum
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Use workspace from input or from runner context
	workspace := in.Workspace
	if workspace == "" {
		workspace = rc.Workspace
	}

	// Set defaults
	dampingFactor := in.DampingFactor
	if dampingFactor == 0 {
		dampingFactor = 0.85
	}
	tolerance := in.Tolerance
	if tolerance == 0 {
		tolerance = 1e-6
	}

	// Open graph store
	store, err := graph.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return fmt.Errorf("open graph store: %w", err)
	}
	defer func() { errs.Ignore(store.Close(), "close graph store") }()

	// Get all nodes and edges
	nodes, err := store.GetAllNodes(ctx, workspace)
	if err != nil {
		return fmt.Errorf("get all nodes: %w", err)
	}

	if len(nodes) == 0 {
		data := map[string]any{
			"workspace":     workspace,
			"nodes_updated": 0,
			"message":       "no nodes in graph",
		}
		return skillout.Emit(rc, command, data)
	}

	edges, err := store.GetAllEdges(ctx, workspace)
	if err != nil {
		return fmt.Errorf("get all edges: %w", err)
	}

	// Build ID -> numeric ID mapping for gonum
	idToNode := make(map[string]int64)
	nodeToID := make(map[int64]string)
	for i, n := range nodes {
		nodeID := int64(i)
		idToNode[n.NodeID] = nodeID
		nodeToID[nodeID] = n.NodeID
	}

	// Build directed graph
	g := simple.NewDirectedGraph()

	// Add all nodes
	for _, n := range nodes {
		nodeID := idToNode[n.NodeID]
		g.AddNode(simple.Node(nodeID))
	}

	// Add edges
	for _, e := range edges {
		fromID, fromExists := idToNode[e.FromID]
		toID, toExists := idToNode[e.ToID]
		if !fromExists || !toExists {
			continue // Skip edges to non-existent nodes
		}
		if fromID != toID { // No self-loops
			g.SetEdge(g.NewEdge(simple.Node(fromID), simple.Node(toID)))
		}
	}

	// Compute PageRank
	startTime := time.Now()
	pageRanks := network.PageRank(g, dampingFactor, tolerance)
	computeTime := time.Since(startTime)

	// Build ranks map for bulk update
	ranks := make(map[string]float64)
	for nodeID, rank := range pageRanks {
		if stringID, exists := nodeToID[nodeID]; exists {
			ranks[stringID] = rank
		}
	}

	// Bulk update PageRank values
	if err := store.BulkUpdatePageRank(ctx, workspace, ranks); err != nil {
		return fmt.Errorf("bulk update pagerank: %w", err)
	}

	// Also recalculate degrees
	if err := store.RecalculateDegrees(ctx, workspace); err != nil {
		return fmt.Errorf("recalculate degrees: %w", err)
	}

	// Find top nodes
	type topNode struct {
		NodeID   string  `json:"node_id"`
		PageRank float64 `json:"pagerank"`
	}

	var topNodes []topNode
	topResults, err := store.TopNodes(ctx, graph.TopNodesOptions{
		Workspace: workspace,
		Limit:     10,
	})
	if err == nil {
		for _, n := range topResults {
			topNodes = append(topNodes, topNode{
				NodeID:   n.NodeID,
				PageRank: n.PageRank,
			})
		}
	}

	data := map[string]any{
		"workspace":       workspace,
		"nodes_updated":   len(ranks),
		"edges_processed": len(edges),
		"damping_factor":  dampingFactor,
		"tolerance":       tolerance,
		"compute_time_ms": computeTime.Milliseconds(),
		"top_nodes":       topNodes,
	}
	return skillout.Emit(rc, command, data)
}
