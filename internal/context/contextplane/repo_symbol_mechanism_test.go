package contextplane

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
)

func TestBuildRepoSymbolMechanismCandidatesJoinsRepoShapeAndExistingEmbedding(t *testing.T) {
	ctx := context.Background()
	workspaceID := "ws-repo"

	repoStore, err := repoindex.Open(ctx, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("open repoindex: %v", err)
	}
	defer func() { _ = repoStore.Close() }()

	key := repoStore.RepoKey()
	targetPkg := "go:github.com/acme/project/internal/alpha"
	target := repoindex.Node{
		ID:        repoindex.SymbolID(key, targetPkg, "Build"),
		Kind:      repoindex.NodeSymbol,
		Pkg:       targetPkg,
		File:      "internal/alpha/builder.go",
		Name:      "Build",
		Signature: "func Build() Runner",
		SpanStart: 10,
		SpanEnd:   20,
		UpdatedAt: time.Now().UTC(),
	}
	callee := repoindex.Node{
		ID:        repoindex.SymbolID(key, targetPkg, "Run"),
		Kind:      repoindex.NodeSymbol,
		Pkg:       targetPkg,
		File:      "internal/alpha/runner.go",
		Name:      "Run",
		UpdatedAt: time.Now().UTC(),
	}
	ref := repoindex.Node{
		ID:        repoindex.SymbolID(key, targetPkg, "Config"),
		Kind:      repoindex.NodeSymbol,
		Pkg:       targetPkg,
		File:      "internal/alpha/config.go",
		Name:      "Config",
		UpdatedAt: time.Now().UTC(),
	}
	caller := repoindex.Node{
		ID:        repoindex.SymbolID(key, "go:github.com/acme/project/cmd/app", "main"),
		Kind:      repoindex.NodeSymbol,
		Pkg:       "go:github.com/acme/project/cmd/app",
		File:      "cmd/app/main.go",
		Name:      "main",
		UpdatedAt: time.Now().UTC(),
	}
	if err := repoStore.ReplaceAll(ctx, []repoindex.Node{target, callee, ref, caller}, []repoindex.Edge{
		{Src: target.ID, Dst: callee.ID, Type: repoindex.EdgeCalls, Weight: 1},
		{Src: target.ID, Dst: ref.ID, Type: repoindex.EdgeRefersTo, Weight: 1},
		{Src: caller.ID, Dst: target.ID, Type: repoindex.EdgeCalls, Weight: 1},
	}); err != nil {
		t.Fatalf("replace repo graph: %v", err)
	}

	embeddingStore, err := embedding.OpenStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open embedding store: %v", err)
	}
	defer func() { _ = embeddingStore.Close() }()

	// The embedding queue uses path-derived package IDs for this corpus, while
	// repoindex may use full Go import-path package IDs. The adapter should try
	// both forms and find this existing vector.
	const storedSymbolID = "go:internal/alpha::Build"
	enqueued, err := embeddingStore.Enqueue(ctx, embedding.EnqueueRequest{
		WorkspaceID: workspaceID,
		Symbols: []embedding.SymbolInput{{
			SymbolID:   storedSymbolID,
			FilePath:   target.File,
			SymbolName: target.Name,
			PackageID:  "go:internal/alpha",
			SymbolKey:  "Build",
			Content:    "func Build() Runner { return Run() }",
		}},
	})
	if err != nil {
		t.Fatalf("enqueue embedding: %v", err)
	}
	if enqueued.Queued != 1 {
		t.Fatalf("queued=%d want 1", enqueued.Queued)
	}
	job, err := embeddingStore.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim embedding job: %v", err)
	}
	if err := embeddingStore.Complete(ctx, job.ID, []float32{0.1, 0.2, 0.3}, "test-model"); err != nil {
		t.Fatalf("complete embedding job: %v", err)
	}

	result, err := BuildRepoSymbolMechanismCandidates(ctx, repoStore, embeddingStore, RepoSymbolMechanismBuildOptions{
		WorkspaceID: workspaceID,
		MaxSymbols:  10,
		PerNodeCap:  10,
		EdgeTypes:   []repoindex.EdgeType{repoindex.EdgeCalls, repoindex.EdgeRefersTo},
	})
	if err != nil {
		t.Fatalf("BuildRepoSymbolMechanismCandidates: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidates=%d want 1 (skipped unembedded=%d invalid=%d)", len(result.Candidates), result.SkippedUnembedded, result.SkippedInvalid)
	}

	candidate := result.Candidates[0]
	if candidate.SymbolID != storedSymbolID {
		t.Fatalf("candidate.SymbolID=%q want %q", candidate.SymbolID, storedSymbolID)
	}
	if got := candidate.StructuralShape.Graph.Outgoing[string(repoindex.EdgeCalls)]; got != 1 {
		t.Fatalf("outgoing CALLS=%d want 1", got)
	}
	if got := candidate.StructuralShape.Graph.Outgoing[string(repoindex.EdgeRefersTo)]; got != 1 {
		t.Fatalf("outgoing REFERS_TO=%d want 1", got)
	}
	if got := candidate.StructuralShape.Graph.Incoming[string(repoindex.EdgeCalls)]; got != 1 {
		t.Fatalf("incoming CALLS=%d want 1", got)
	}
	if len(candidate.LiteralVector) != 3 {
		t.Fatalf("literal vector len=%d want 3", len(candidate.LiteralVector))
	}
	if len(candidate.StructuralVector) == 0 {
		t.Fatalf("expected structural vector")
	}
	for _, literal := range []string{"Build", "internal/alpha", "github.com/acme"} {
		if strings.Contains(candidate.Projection.AbstractSchema, literal) {
			t.Fatalf("abstract schema leaked literal detail %q:\n%s", literal, candidate.Projection.AbstractSchema)
		}
	}
	if !strings.Contains(candidate.Projection.LiteralText, "Build") {
		t.Fatalf("literal text should preserve symbol detail:\n%s", candidate.Projection.LiteralText)
	}

	memory := candidate.MechanismMemory()
	if memory.ID != candidate.Projection.ID || len(memory.SourceRefs) == 0 {
		t.Fatalf("mechanism memory missing identity/source refs: %+v", memory)
	}
}

func TestRepoSymbolEmbeddingIDCandidatesPrefersPathDerivedPackage(t *testing.T) {
	node := repoindex.Node{
		ID:   "repo::sym:go:github.com/acme/project/internal/alpha:Build",
		Kind: repoindex.NodeSymbol,
		Pkg:  "go:github.com/acme/project/internal/alpha",
		File: "internal/alpha/builder.go",
		Name: "Build",
	}
	got := repoSymbolEmbeddingIDCandidates(node)
	if len(got) == 0 || got[0] != "go:internal/alpha::Build" {
		t.Fatalf("candidates=%v, first should be path-derived package id", got)
	}
}
