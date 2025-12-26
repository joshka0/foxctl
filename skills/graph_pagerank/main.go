// Package main implements the graph/pagerank skill for computing PageRank scores.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"gonum.org/v1/gonum/graph/network"
	"gonum.org/v1/gonum/graph/simple"
)

type input struct {
	Workspace     string  `json:"workspace"`
	DampingFactor float64 `json:"damping_factor"` // Default: 0.85
	Tolerance     float64 `json:"tolerance"`      // Default: 1e-6
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "graph_pagerank skill error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		return fmt.Errorf("runner context: %w", err)
	}
	defer errs.Ignore(rc.Close(), "close runner context")

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return fmt.Errorf("decode input: %w", err)
	}

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
	store, err := graph.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open graph store: %w", err)
	}
	defer errs.Ignore(store.Close(), "close graph store")

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
		return rc.Emit("graph.pagerank", data, "", envelope.Meta{})
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
	return rc.Emit("graph.pagerank", data, "", envelope.Meta{})
}
