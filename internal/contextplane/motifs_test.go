package contextplane

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

func TestBuildAndSearchRepoMotifArtifacts(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	repoStore, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open repoindex: %v", err)
	}
	defer func() { _ = repoStore.Close() }()

	key := repoStore.RepoKey()
	pkg := "elixir:jido"
	fileA := repoindex.Node{
		ID:        repoindex.FileID(key, pkg, "lib/jido/agent_server.ex"),
		Kind:      repoindex.NodeFile,
		Pkg:       pkg,
		File:      "lib/jido/agent_server.ex",
		Name:      "agent_server.ex",
		UpdatedAt: time.Now().UTC(),
	}
	fileB := repoindex.Node{
		ID:        repoindex.FileID(key, pkg, "lib/jido/agent_server/directive_exec.ex"),
		Kind:      repoindex.NodeFile,
		Pkg:       pkg,
		File:      "lib/jido/agent_server/directive_exec.ex",
		Name:      "directive_exec.ex",
		UpdatedAt: time.Now().UTC(),
	}
	fileC := repoindex.Node{
		ID:        repoindex.FileID(key, pkg, "lib/jido/agent_server/directive_executors.ex"),
		Kind:      repoindex.NodeFile,
		Pkg:       pkg,
		File:      "lib/jido/agent_server/directive_executors.ex",
		Name:      "directive_executors.ex",
		UpdatedAt: time.Now().UTC(),
	}
	symB := repoindex.Node{
		ID:        repoindex.SymbolID(key, pkg, "Jido.AgentServer.DirectiveExec"),
		Kind:      repoindex.NodeSymbol,
		Pkg:       pkg,
		File:      fileB.File,
		Name:      "Jido.AgentServer.DirectiveExec",
		UpdatedAt: time.Now().UTC(),
	}
	symC := repoindex.Node{
		ID:        repoindex.SymbolID(key, pkg, "Jido.AgentServer.DirectiveExecutors"),
		Kind:      repoindex.NodeSymbol,
		Pkg:       pkg,
		File:      fileC.File,
		Name:      "Jido.AgentServer.DirectiveExecutors",
		UpdatedAt: time.Now().UTC(),
	}
	nodes := []repoindex.Node{fileA, fileB, fileC, symB, symC}
	edges := []repoindex.Edge{
		{Src: fileA.ID, Dst: symB.ID, Type: repoindex.EdgeRefersTo, Weight: 1},
		{Src: fileA.ID, Dst: symC.ID, Type: repoindex.EdgeUsesSymbol, Weight: 1},
		{Src: fileB.ID, Dst: symC.ID, Type: repoindex.EdgeUsesSymbol, Weight: 1},
	}
	if err := repoStore.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace repo graph: %v", err)
	}

	memStore, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer func() { _ = memStore.Close() }()

	provider := testEmbedder{dims: 8}
	motifs, err := BuildRepoMotifArtifacts(ctx, workspace, repoStore, memStore, provider, RepoMotifBuildOptions{
		MaxSeeds:       10,
		MaxMotifs:      10,
		Depth:          2,
		Budget:         20,
		PerNodeCap:     10,
		MaxRelated:     3,
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("BuildRepoMotifArtifacts: %v", err)
	}
	if len(motifs) == 0 {
		t.Fatalf("expected motifs")
	}

	entries, total, err := memStore.ListFiltered(ctx, workspace, storage.MemoryListFilter{Types: []string{RepoMotifType}}, 20, 0)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	if total == 0 || len(entries) == 0 {
		t.Fatalf("expected persisted repo_motif entries")
	}

	hits, err := SearchRepoMotifArtifacts(ctx, workspace, "directive executors", 5, memStore, provider)
	if err != nil {
		t.Fatalf("SearchRepoMotifArtifacts: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected motif hits")
	}
	found := false
	for _, pathValue := range hits[0].Paths {
		if filepath.ToSlash(pathValue) == "lib/jido/agent_server/directive_executors.ex" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected directive_executors in top hit paths, got %v", hits[0].Paths)
	}
}
