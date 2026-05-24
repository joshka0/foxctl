package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/refactor/impact"
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

func TestSemanticProviderBatchesOversamplesExcludesAndSorts(t *testing.T) {
	embedder := &recordingRefactorImpactEmbedder{}
	store := &recordingRefactorImpactStore{
		hits: []searchindex.SearchHit{
			{Doc: searchindex.Document{Path: "internal/changed.go", SymbolName: "Changed"}, Score: 0.99},
			{Doc: searchindex.Document{Path: "internal/b.go", SymbolName: "B", Summary: "b"}, Score: 0.80},
			{Doc: searchindex.Document{Path: "internal/a.go", SymbolName: "A", Summary: "a"}, Score: 0.80},
			{Doc: searchindex.Document{Path: "internal/b.go", SymbolName: "B", Summary: "better"}, Score: 0.91},
		},
	}
	provider := &semanticProvider{
		workspaceRoot: t.TempDir(),
		workspaceID:   "ws-test",
		store:         store,
		embedder:      embedder,
		model:         "test-model",
		source:        impact.SourceTurboVec,
	}

	got, err := provider.Neighbors(context.Background(), impact.SemanticNeighborRequest{
		Targets:       []impact.Target{{Kind: impact.TargetFile, Path: "internal/changed.go"}},
		ExcludePaths:  []string{"internal/changed.go"},
		Limit:         5,
		PerTargetCap:  3,
		WorkspaceRoot: provider.workspaceRoot,
	})
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
	paths := []string{}
	for _, candidate := range got.Candidates {
		paths = append(paths, candidate.Path+":"+candidate.Symbol)
	}
	want := []string{"internal/b.go:B", "internal/a.go:A"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("candidate order=%v want %v", paths, want)
	}
	if got.Candidates[0].Summary != "better" {
		t.Fatalf("dedup summary=%q want highest-similarity hit", got.Candidates[0].Summary)
	}
	if got.Source != impact.SourceTurboVec {
		t.Fatalf("source=%q want turbovec", got.Source)
	}
}

type recordingRefactorImpactEmbedder struct {
	embedCalls int
	batchCalls int
}

func (e *recordingRefactorImpactEmbedder) Embed(context.Context, string) ([]float32, error) {
	e.embedCalls++
	return nil, errors.New("Embed should not be called")
}

func (e *recordingRefactorImpactEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	e.batchCalls++
	embeddings := make([][]float32, 0, len(texts))
	for range texts {
		embeddings = append(embeddings, []float32{1, 0, 0})
	}
	return embeddings, nil
}

func (e *recordingRefactorImpactEmbedder) Model() string { return "test-model" }

func (e *recordingRefactorImpactEmbedder) Dimensions() int { return 3 }

type recordingRefactorImpactStore struct {
	hits        []searchindex.SearchHit
	vectorLimit int
}

func (s *recordingRefactorImpactStore) Close() error { return nil }

func (s *recordingRefactorImpactStore) Upsert(context.Context, searchindex.Document) error {
	return nil
}

func (s *recordingRefactorImpactStore) Delete(context.Context, string) error { return nil }

func (s *recordingRefactorImpactStore) DeleteWorkspace(context.Context, string) error {
	return nil
}

func (s *recordingRefactorImpactStore) CountWorkspace(context.Context, string) (int, error) {
	return 0, nil
}

func (s *recordingRefactorImpactStore) WorkspaceStats(context.Context, string) (searchindex.WorkspaceStats, error) {
	return searchindex.WorkspaceStats{}, nil
}

func (s *recordingRefactorImpactStore) GetEmbeddingMetadata(context.Context, string) (*searchindex.EmbeddingMetadata, error) {
	return nil, nil
}

func (s *recordingRefactorImpactStore) ValidateEmbeddingMetadata(context.Context, string, string, int) error {
	return nil
}

func (s *recordingRefactorImpactStore) LexicalRecall(context.Context, string, string, searchindex.RecallOptions) ([]searchindex.SearchHit, error) {
	return nil, nil
}

func (s *recordingRefactorImpactStore) ExactRecall(context.Context, string, string, searchindex.ExactRecallOptions) ([]searchindex.SearchHit, error) {
	return nil, nil
}

func (s *recordingRefactorImpactStore) VectorRecall(_ context.Context, _ string, _ []float32, opts searchindex.VectorRecallOptions) ([]searchindex.SearchHit, error) {
	s.vectorLimit = opts.Limit
	return s.hits, nil
}

func (s *recordingRefactorImpactStore) GetEmbeddingsByIDs(context.Context, []string) (map[string][]float32, error) {
	return nil, nil
}

func (s *recordingRefactorImpactStore) GetDocumentsByIDs(context.Context, []string) (map[string]searchindex.Document, error) {
	return nil, nil
}
