package repoindex

import (
	"context"
	"testing"
	"time"
)

func TestApplyCoChangeEdgesRequiresExplicitBuildOption(t *testing.T) {
	ctx := context.Background()
	store, qe, seedID := setupEdgeNormalizationStore(t)
	defer store.Close()

	defaultResult, err := qe.Expand(ctx, []string{seedID}, ExpandOptions{EdgeTypes: EdgeSetEmpirical})
	if err != nil {
		t.Fatalf("default empirical expand: %v", err)
	}
	assertNoExpandedEdge(t, defaultResult.Edges, EdgeCoChangesWith)

	repoKey := store.RepoKey()
	pkg := "go:test/pkg"
	fileSeedID := FileID(repoKey, pkg, "seed.go")
	fileTargetID := FileID(repoKey, pkg, "target.go")
	nodes := map[string]Node{
		fileSeedID: {
			ID:        fileSeedID,
			Kind:      NodeFile,
			Pkg:       pkg,
			File:      "seed.go",
			Name:      "seed.go",
			UpdatedAt: time.Now().UTC(),
		},
		fileTargetID: {
			ID:        fileTargetID,
			Kind:      NodeFile,
			Pkg:       pkg,
			File:      "target.go",
			Name:      "target.go",
			UpdatedAt: time.Now().UTC(),
		},
	}
	edges := map[string]Edge{}
	applyCoChangeNeighborsForTest(nodes, edges, map[string][]cochangeTestNeighbor{
		"seed.go": {{Path: "target.go", Score: 0.75}},
	})
	if err := store.ReplaceAll(ctx, mapValues(nodes), mapValues(edges)); err != nil {
		t.Fatalf("replace all: %v", err)
	}
	got, err := qe.Expand(ctx, []string{fileSeedID}, ExpandOptions{EdgeTypes: EdgeSetEmpirical})
	if err != nil {
		t.Fatalf("empirical expand: %v", err)
	}
	assertHasExpandedEdge(t, got.Edges, EdgeCoChangesWith)
}

type cochangeTestNeighbor struct {
	Path  string
	Score float64
}

func applyCoChangeNeighborsForTest(nodes map[string]Node, edges map[string]Edge, neighbors map[string][]cochangeTestNeighbor) {
	pathNodes := fileNodesByPath(nodes)
	for srcPath, items := range neighbors {
		src := pathNodes[srcPath]
		for _, item := range items {
			dst := pathNodes[item.Path]
			addEdge(edges, Edge{Src: src.ID, Dst: dst.ID, Type: EdgeCoChangesWith, Weight: item.Score})
		}
	}
}

func mapValues[T any](values map[string]T) []T {
	out := make([]T, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
