package repoindex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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

func TestBuilderEmitsGitCoChangeEdgesOnlyWhenFlagged(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	writeCoChangeRepoFixture(t, workspace, now)

	store, err := Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, workspace)
	if _, err := builder.Build(ctx, BuildOptions{
		RepoRoot:  workspace,
		IncludeGo: true,
	}); err != nil {
		t.Fatalf("build without co-change: %v", err)
	}
	aFile := findNodeByName(t, store, NodeFile, "a.go")
	defaultEdges, err := store.GetOutgoingEdges(ctx, aFile.ID, []EdgeType{EdgeCoChangesWith}, 20)
	if err != nil {
		t.Fatalf("get default co-change edges: %v", err)
	}
	if len(defaultEdges) != 0 {
		t.Fatalf("default build emitted co-change edges: %+v", defaultEdges)
	}

	if _, err := builder.Build(ctx, BuildOptions{
		RepoRoot:         workspace,
		IncludeGo:        true,
		IncludeCoChange:  true,
		IncludeTests:     false,
		IncludeTerraform: false,
	}); err != nil {
		t.Fatalf("build with co-change: %v", err)
	}
	aFile = findNodeByName(t, store, NodeFile, "a.go")
	bFile := findNodeByName(t, store, NodeFile, "b.go")
	cochangeEdges, err := store.GetOutgoingEdges(ctx, aFile.ID, []EdgeType{EdgeCoChangesWith}, 20)
	if err != nil {
		t.Fatalf("get co-change edges: %v", err)
	}
	edge := findCoChangeEdge(t, cochangeEdges, bFile.ID)
	if edge.Weight <= 0 {
		t.Fatalf("co-change edge weight=%f want positive", edge.Weight)
	}
	var meta coChangeEdgeMeta
	if err := json.Unmarshal(edge.Meta, &meta); err != nil {
		t.Fatalf("decode co-change edge meta: %v", err)
	}
	if meta.Source != "git" {
		t.Fatalf("meta source=%q want git", meta.Source)
	}
	if meta.Count < 2 {
		t.Fatalf("meta count=%d want repeated co-change from git history", meta.Count)
	}
	if meta.WeightedCount <= 0 || meta.Freshness <= 0 || meta.Volatility <= 0 {
		t.Fatalf("expected scored co-change metadata, got %+v", meta)
	}

	expanded, err := NewQueryEngine(store).Expand(ctx, []string{aFile.ID}, ExpandOptions{})
	if err != nil {
		t.Fatalf("default expand: %v", err)
	}
	assertNoExpandedEdge(t, expanded.Edges, EdgeCoChangesWith)

	expanded, err = NewQueryEngine(store).Expand(ctx, []string{aFile.ID}, ExpandOptions{EdgeTypes: EdgeSetEmpirical})
	if err != nil {
		t.Fatalf("empirical expand: %v", err)
	}
	assertHasExpandedEdge(t, expanded.Edges, EdgeCoChangesWith)
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

func writeCoChangeRepoFixture(t *testing.T, workspace string, now time.Time) {
	t.Helper()
	runRepoGit(t, workspace, "init")
	writeRepoFile(t, workspace, "go.mod", "module example.com/cochangefixture\n\ngo 1.22\n")
	writeRepoFile(t, workspace, "README.md", "fixture\n")
	runRepoGit(t, workspace, "add", ".")
	commitRepoGitAt(t, workspace, now.AddDate(0, 0, -10), "initial")

	writeRepoFile(t, workspace, "a.go", "package cochangefixture\n\nfunc A() int { return 1 }\n")
	writeRepoFile(t, workspace, "b.go", "package cochangefixture\n\nfunc B() int { return A() }\n")
	runRepoGit(t, workspace, "add", ".")
	commitRepoGitAt(t, workspace, now.AddDate(0, 0, -5), "add pair")

	writeRepoFile(t, workspace, "a.go", "package cochangefixture\n\nfunc A() int { return 2 }\n")
	writeRepoFile(t, workspace, "b.go", "package cochangefixture\n\nfunc B() int { return A() + 1 }\n")
	writeRepoFile(t, workspace, "go.sum", "example.com/dep v0.0.0 h1:abc\n")
	runRepoGit(t, workspace, "add", ".")
	commitRepoGitAt(t, workspace, now.AddDate(0, 0, -1), "repeat pair with lockfile noise")
}

func writeRepoFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func commitRepoGitAt(t *testing.T, root string, at time.Time, message string) {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "-c", "user.name=Foxctl Test", "-c", "user.email=foxctl@example.invalid", "commit", "-m", message)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+at.Format(time.RFC3339),
		"GIT_COMMITTER_DATE="+at.Format(time.RFC3339),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git commit %q: %v\n%s", message, err, string(out))
	}
}

func runRepoGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}

func findCoChangeEdge(t *testing.T, edges []Edge, dst string) Edge {
	t.Helper()
	for _, edge := range edges {
		if edge.Type == EdgeCoChangesWith && edge.Dst == dst {
			return edge
		}
	}
	t.Fatalf("co-change edge to %s not found in %+v", dst, edges)
	return Edge{}
}
