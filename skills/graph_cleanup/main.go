// Package main implements the graph/cleanup skill for graph maintenance.
// This skill handles TTL cleanup, dangling edge removal, and degree recalculation.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/storage/graph"
)

const command = "graph/cleanup"

var allowedOps = []string{"cleanup", "stats", "repair"}

// input defines the input parameters for graph/cleanup operations.
type input struct {
	Workspace string `json:"workspace"`
	Operation string `json:"operation"` // cleanup, stats, repair
	// Cleanup options
	CleanExpired  bool `json:"clean_expired"`  // Remove TTL-expired edges
	CleanDangling bool `json:"clean_dangling"` // Remove edges to non-existent nodes
	Recalculate   bool `json:"recalculate"`    // Recalculate node degrees
}

// cleanupResult contains the results of cleanup operations.
type cleanupResult struct {
	ExpiredEdgesRemoved  int  `json:"expired_edges_removed,omitempty"`
	DanglingEdgesRemoved int  `json:"dangling_edges_removed,omitempty"`
	DegreesRecalculated  bool `json:"degrees_recalculated,omitempty"`
}

// main is the skill entry point for graph/cleanup.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates graph maintenance and cleanup operations with workspace isolation.
//
// Index:
//
//	Purpose: Execute graph cleanup operations (expired edges, dangling edges, degree recalculation)
//	Keywords: graph/cleanup, graph_maintenance, database_cleanup, expired_edges, dangling_edges
//	Related: handleCleanup, handleRepair, handleStats
//	Flow: validate operation → open store → dispatch to handler → emit results
//	Resources: graph database (SQLite); workspace-scoped cleanup
//	Events: graph-cleanup, graph-repair, graph-stats
//	OutputFields: expired_edges_removed, dangling_edges_removed, degrees_recalculated
//
// [[domain:graph-database]]
// [[protocol:skill-dispatch]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	op := oputil.Op(oputil.DefaultOp(in.Operation, "cleanup"))
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOps, ", "))
	if err := oputil.Validate(op, allowedOps...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}

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

	var data map[string]any

	switch op {
	case "cleanup":
		result, err := handleCleanup(ctx, store, workspace, in)
		if err != nil {
			return err
		}
		data = map[string]any{
			"workspace": workspace,
			"result":    result,
			"summary":   fmt.Sprintf("cleanup complete: %d expired, %d dangling edges removed", result.ExpiredEdgesRemoved, result.DanglingEdgesRemoved),
		}

	case "stats":
		stats, err := store.Stats(ctx, workspace)
		if err != nil {
			return fmt.Errorf("get stats: %w", err)
		}
		data = map[string]any{
			"workspace": workspace,
			"stats":     stats,
			"summary":   fmt.Sprintf("%d nodes, %d edges", stats.Nodes.TotalNodes, stats.Edges.TotalEdges),
		}

	case "repair":
		result, err := handleRepair(ctx, store, workspace)
		if err != nil {
			return err
		}
		data = map[string]any{
			"workspace": workspace,
			"result":    result,
			"summary":   fmt.Sprintf("repair complete: %d expired, %d dangling edges removed, degrees recalculated", result.ExpiredEdgesRemoved, result.DanglingEdgesRemoved),
		}

	default:
		return skillerr.Arg("invalid operation", skillerr.WithHint(opHint))
	}

	return skillout.Emit(rc, command, data)
}

// handleCleanup performs selective cleanup operations based on input flags.
func handleCleanup(ctx context.Context, store graph.Store, workspace string, in input) (*cleanupResult, error) {
	result := &cleanupResult{}

	// Default: clean both expired and dangling if nothing specified
	cleanExpired := in.CleanExpired
	cleanDangling := in.CleanDangling
	recalculate := in.Recalculate

	if !cleanExpired && !cleanDangling && !recalculate {
		// Default to all cleanup operations
		cleanExpired = true
		cleanDangling = true
		recalculate = true
	}

	if cleanExpired {
		n, err := store.CleanupExpiredEdges(ctx)
		if err != nil {
			return nil, fmt.Errorf("cleanup expired edges: %w", err)
		}
		result.ExpiredEdgesRemoved = n
	}

	if cleanDangling {
		n, err := store.CleanupDanglingEdges(ctx, workspace)
		if err != nil {
			return nil, fmt.Errorf("cleanup dangling edges: %w", err)
		}
		result.DanglingEdgesRemoved = n
	}

	if recalculate {
		if err := store.RecalculateDegrees(ctx, workspace); err != nil {
			return nil, fmt.Errorf("recalculate degrees: %w", err)
		}
		result.DegreesRecalculated = true
	}

	return result, nil
}

// handleRepair performs comprehensive cleanup and repair operations.
func handleRepair(ctx context.Context, store graph.Store, workspace string) (*cleanupResult, error) {
	result := &cleanupResult{}

	// Step 1: Clean expired edges (global operation)
	n, err := store.CleanupExpiredEdges(ctx)
	if err != nil {
		return nil, fmt.Errorf("cleanup expired edges: %w", err)
	}
	result.ExpiredEdgesRemoved = n

	// Step 2: Clean dangling edges (workspace-scoped)
	n, err = store.CleanupDanglingEdges(ctx, workspace)
	if err != nil {
		return nil, fmt.Errorf("cleanup dangling edges: %w", err)
	}
	result.DanglingEdgesRemoved = n

	// Step 3: Recalculate degrees (workspace-scoped)
	if err := store.RecalculateDegrees(ctx, workspace); err != nil {
		return nil, fmt.Errorf("recalculate degrees: %w", err)
	}
	result.DegreesRecalculated = true

	return result, nil
}
