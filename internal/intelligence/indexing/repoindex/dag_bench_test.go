package repoindex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkDAGGrepStructuralExpansion(b *testing.B) {
	_, qe, ids := setupGraphBenchStore(b, 96)
	req := DAGGrepRequest{
		Query:          "runtime dag benchmark",
		K:              4,
		EdgeTypes:      EdgeSetStructural,
		Direction:      DirOut,
		Depth:          3,
		Budget:         48,
		PerNodeCap:     8,
		IncludeAnchors: false,
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := qe.DAGGrep(ctx, req)
		if err != nil {
			b.Fatalf("DAGGrep() error = %v", err)
		}
		if len(ids) > 0 && result.Stats.NodeCount == 0 {
			b.Fatal("DAGGrep() returned no nodes")
		}
		if result.Stats.NodeCount > req.Budget {
			b.Fatalf("DAGGrep() returned %d nodes, want at most budget %d", result.Stats.NodeCount, req.Budget)
		}
		if len(result.DAG.Layers) != result.Stats.NodeCount {
			b.Fatalf("DAGGrep() layers=%d nodes=%d", len(result.DAG.Layers), result.Stats.NodeCount)
		}
	}
}

func BenchmarkDAGGrepBudgetScaling(b *testing.B) {
	for _, budget := range []int{16, 64, 128} {
		b.Run(fmt.Sprintf("budget_%d", budget), func(b *testing.B) {
			_, qe, _ := setupGraphBenchStore(b, 160)
			req := DAGGrepRequest{
				Query:      "repoindex graph benchmark",
				K:          5,
				EdgeTypes:  EdgeSetStructural,
				Direction:  DirOut,
				Depth:      4,
				Budget:     budget,
				PerNodeCap: 12,
			}
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := qe.DAGGrep(ctx, req)
				if err != nil {
					b.Fatalf("DAGGrep() error = %v", err)
				}
				if result.Stats.NodeCount > budget {
					b.Fatalf("DAGGrep() returned %d nodes, want at most budget %d", result.Stats.NodeCount, budget)
				}
				if result.Stats.EdgeCount != len(result.Graph.Edges) {
					b.Fatalf("DAGGrep() edge stats=%d graph edges=%d", result.Stats.EdgeCount, len(result.Graph.Edges))
				}
			}
		})
	}
}

func BenchmarkTracePathStructuralHit(b *testing.B) {
	_, qe, ids := setupGraphBenchStore(b, 96)
	ctx := context.Background()
	opts := TracePathOptions{
		SrcID:      ids[0],
		DstID:      ids[30],
		MaxDepth:   6,
		PerNodeCap: 8,
		EdgeTypes:  []EdgeType{EdgeCalls, EdgeDescribedBy},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := qe.TracePath(ctx, opts)
		if err != nil {
			b.Fatalf("TracePath() error = %v", err)
		}
		if !result.Found {
			b.Fatal("TracePath() did not find expected path")
		}
	}
}

func BenchmarkBlastRadiusStructural(b *testing.B) {
	_, qe, ids := setupGraphBenchStore(b, 128)
	ctx := context.Background()
	opts := BlastRadiusOptions{
		NodeID:     ids[0],
		MaxDepth:   3,
		Limit:      64,
		PerNodeCap: 10,
		EdgeTypes:  []EdgeType{EdgeCalls, EdgeDescribedBy},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := qe.BlastRadius(ctx, opts)
		if err != nil {
			b.Fatalf("BlastRadius() error = %v", err)
		}
		if len(result.Graph.Nodes) == 0 {
			b.Fatal("BlastRadius() returned no graph nodes")
		}
	}
}

func setupGraphBenchStore(b *testing.B, n int) (*Store, *QueryEngine, []string) {
	b.Helper()
	ctx := context.Background()
	root := b.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		b.Fatalf("mkdir repo root: %v", err)
	}
	store, err := Open(ctx, storageRoot, repoRoot)
	if err != nil {
		b.Fatalf("open store: %v", err)
	}
	b.Cleanup(func() { store.Close() })

	key := repoKey(repoRoot)
	pkg := "go:bench/graph"
	now := time.Now().UTC()
	nodes := make([]Node, 0, n)
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := SymbolID(key, pkg, fmt.Sprintf("node%d", i))
		ids = append(ids, id)
		nodes = append(nodes, Node{
			ID:        id,
			Kind:      NodeSymbol,
			Pkg:       pkg,
			File:      fmt.Sprintf("graph/node_%03d.go", i),
			Name:      fmt.Sprintf("node%d", i),
			Doc:       fmt.Sprintf("runtime dag benchmark repoindex graph node %d", i),
			UpdatedAt: now,
		})
	}
	edges := make([]Edge, 0, n*3)
	for i := 0; i < n; i++ {
		for _, step := range []int{1, 2, 5} {
			dst := i + step
			if dst >= n {
				continue
			}
			edgeType := EdgeCalls
			if step == 5 {
				edgeType = EdgeDescribedBy
			}
			edges = append(edges, Edge{
				Src:    ids[i],
				Dst:    ids[dst],
				Type:   edgeType,
				Weight: 1,
			})
		}
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		b.Fatalf("replace all: %v", err)
	}
	return store, NewQueryEngine(store), ids
}
