package main

import (
	"context"
	"errors"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/branchimpact"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
)

func TestParseNameStatusHandlesRename(t *testing.T) {
	raw := "M\x00internal/a.go\x00R100\x00internal/old.go\x00internal/new.go\x00"

	got := parseNameStatus(raw)
	if len(got) != 2 {
		t.Fatalf("change count=%d want 2: %#v", len(got), got)
	}
	if got[0].Status != "M" || got[0].Path != "internal/a.go" {
		t.Fatalf("first change=%+v want modified internal/a.go", got[0])
	}
	if got[1].Status != "R100" || got[1].OldPath != "internal/old.go" || got[1].Path != "internal/new.go" {
		t.Fatalf("rename change=%+v want old->new", got[1])
	}
}

func TestParseNumstatHandlesNormalAndBinaryFiles(t *testing.T) {
	raw := "10\t2\tinternal/a.go\x00-\t-\tassets/image.png\x00"

	got := parseNumstat(raw)
	if got["internal/a.go"].additions != 10 || got["internal/a.go"].deletions != 2 {
		t.Fatalf("stat for a.go=%+v want 10/2", got["internal/a.go"])
	}
	if got["assets/image.png"].additions != 0 || got["assets/image.png"].deletions != 0 {
		t.Fatalf("stat for binary=%+v want 0/0", got["assets/image.png"])
	}
}

func TestParseNumstatHandlesRenameNULFormat(t *testing.T) {
	raw := "3\t1\t\x00internal/old.go\x00internal/new.go\x001\t0\tinternal/next.go\x00"

	got := parseNumstat(raw)
	for _, path := range []string{"internal/old.go", "internal/new.go"} {
		if got[path].additions != 3 || got[path].deletions != 1 {
			t.Fatalf("stat for %s=%+v want 3/1", path, got[path])
		}
	}
	if got["internal/next.go"].additions != 1 || got["internal/next.go"].deletions != 0 {
		t.Fatalf("stat for next=%+v want 1/0", got["internal/next.go"])
	}
}

func TestDiffRangeArgsUsesCanonicalRefs(t *testing.T) {
	got := diffRangeArgs(Input{BaseRef: "main", HeadRef: "HEAD"})
	if len(got) != 1 || got[0] != "main...HEAD" {
		t.Fatalf("range args=%v want main...HEAD", got)
	}
}

func TestCandidatesFromContextSectionsIncludesCallers(t *testing.T) {
	got := candidatesFromContextSections([]repoindex.ContextSection{
		{
			Name: "callers",
			Nodes: []repoindex.Node{
				{ID: "changed", Kind: repoindex.NodeSymbol, File: "internal/core.go", Name: "Plan", SpanStart: 10},
				{ID: "caller", Kind: repoindex.NodeSymbol, File: "skills/tool/main.go", Name: "run", SpanStart: 20},
			},
			Edges: []repoindex.Edge{{Src: "caller", Dst: "changed", Type: repoindex.EdgeCalls}},
		},
		{
			Name:  "contains_in",
			Nodes: []repoindex.Node{{ID: "file", Kind: repoindex.NodeFile, File: "internal/core.go", Name: "core.go"}},
		},
	}, 1)

	for _, candidate := range got {
		if candidate.Path != "skills/tool/main.go" {
			continue
		}
		if candidate.Symbol != "run" || candidate.LineHint != 20 || candidate.Depth != 1 {
			t.Fatalf("caller candidate=%+v want run at depth 1", candidate)
		}
		if len(candidate.EdgeTypes) != 1 || candidate.EdgeTypes[0] != string(repoindex.EdgeCalls) {
			t.Fatalf("caller edge types=%v want CALLS", candidate.EdgeTypes)
		}
		return
	}
	t.Fatalf("candidates=%+v missing caller file", got)
}

func TestChangedFileSymbolsSortsAndCaps(t *testing.T) {
	blast := repoindex.BlastRadiusResult{
		Origin: repoindex.Node{ID: "file", Kind: repoindex.NodeFile, File: "internal/core.go"},
		Graph: repoindex.ExpandResult{Nodes: []repoindex.Node{
			{ID: "file", Kind: repoindex.NodeFile, File: "internal/core.go", Name: "core.go"},
			{ID: "later", Kind: repoindex.NodeSymbol, File: "internal/core.go", Name: "Later", SpanStart: 30},
			{ID: "other", Kind: repoindex.NodeSymbol, File: "internal/other.go", Name: "Other", SpanStart: 1},
			{ID: "earlier", Kind: repoindex.NodeSymbol, File: "internal/core.go", Name: "Earlier", SpanStart: 5},
		}},
	}

	got := changedFileSymbols(blast, 1)
	if len(got) != 1 || got[0].ID != "earlier" {
		t.Fatalf("symbols=%+v want earliest changed-file symbol only", got)
	}
}

func TestSemanticProviderBatchesEmbeddingsAndOversamplesBeforeFilteringChangedPaths(t *testing.T) {
	embedder := &recordingBranchImpactEmbedder{}
	store := &recordingBranchImpactStore{
		hits: []searchindex.SearchHit{
			{Doc: searchindex.Document{Path: "internal/changed.go", SymbolName: "Changed"}, Score: 0.99},
			{Doc: searchindex.Document{Path: "internal/neighbor.go", SymbolName: "Neighbor", Summary: "nearby"}, Score: 0.98},
		},
	}
	provider := &semanticProvider{
		workspaceRoot: t.TempDir(),
		workspaceID:   "ws-test",
		store:         store,
		embedder:      embedder,
		model:         "test-model",
		source:        "test-vector",
	}

	got, err := provider.Neighbors(context.Background(), []branchimpact.Change{
		{Path: "internal/changed.go", Status: "M"},
	}, branchimpact.SemanticOptions{Limit: 5, PerFileCap: 1})
	if err != nil {
		t.Fatalf("Neighbors returned error: %v", err)
	}
	if !got.Available {
		t.Fatalf("semantic result unavailable: %+v", got)
	}
	if embedder.embedCalls != 0 || embedder.batchCalls != 1 {
		t.Fatalf("embed calls=%d batch calls=%d want 0/1", embedder.embedCalls, embedder.batchCalls)
	}
	if store.vectorLimit != 100 {
		t.Fatalf("vector limit=%d want 100-document prefilter window", store.vectorLimit)
	}
	if len(got.Candidates) != 1 || got.Candidates[0].Path != "internal/neighbor.go" {
		t.Fatalf("candidates=%+v want non-changed neighbor only", got.Candidates)
	}
}

type recordingBranchImpactEmbedder struct {
	embedCalls int
	batchCalls int
}

func (e *recordingBranchImpactEmbedder) Embed(context.Context, string) ([]float32, error) {
	e.embedCalls++
	return nil, errors.New("Embed should not be called")
}

func (e *recordingBranchImpactEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	e.batchCalls++
	embeddings := make([][]float32, 0, len(texts))
	for range texts {
		embeddings = append(embeddings, []float32{1, 0, 0})
	}
	return embeddings, nil
}

func (e *recordingBranchImpactEmbedder) Model() string { return "test-model" }

func (e *recordingBranchImpactEmbedder) Dimensions() int { return 3 }

type recordingBranchImpactStore struct {
	hits        []searchindex.SearchHit
	vectorLimit int
}

func (s *recordingBranchImpactStore) Close() error { return nil }

func (s *recordingBranchImpactStore) Upsert(context.Context, searchindex.Document) error { return nil }

func (s *recordingBranchImpactStore) Delete(context.Context, string) error { return nil }

func (s *recordingBranchImpactStore) DeleteWorkspace(context.Context, string) error { return nil }

func (s *recordingBranchImpactStore) CountWorkspace(context.Context, string) (int, error) {
	return 0, nil
}

func (s *recordingBranchImpactStore) WorkspaceStats(context.Context, string) (searchindex.WorkspaceStats, error) {
	return searchindex.WorkspaceStats{}, nil
}

func (s *recordingBranchImpactStore) GetEmbeddingMetadata(context.Context, string) (*searchindex.EmbeddingMetadata, error) {
	return nil, nil
}

func (s *recordingBranchImpactStore) ValidateEmbeddingMetadata(context.Context, string, string, int) error {
	return nil
}

func (s *recordingBranchImpactStore) LexicalRecall(context.Context, string, string, searchindex.RecallOptions) ([]searchindex.SearchHit, error) {
	return nil, nil
}

func (s *recordingBranchImpactStore) ExactRecall(context.Context, string, string, searchindex.ExactRecallOptions) ([]searchindex.SearchHit, error) {
	return nil, nil
}

func (s *recordingBranchImpactStore) VectorRecall(_ context.Context, _ string, _ []float32, opts searchindex.VectorRecallOptions) ([]searchindex.SearchHit, error) {
	s.vectorLimit = opts.Limit
	return s.hits, nil
}

func (s *recordingBranchImpactStore) GetEmbeddingsByIDs(context.Context, []string) (map[string][]float32, error) {
	return nil, nil
}

func (s *recordingBranchImpactStore) GetDocumentsByIDs(context.Context, []string) (map[string]searchindex.Document, error) {
	return nil, nil
}
