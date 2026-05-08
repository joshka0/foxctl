package filesummary

import (
	"context"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/storage"
)

type fakeSearchStore struct {
	searchCalled      bool
	vectorWorkspace   string
	vectorType        string
	vectorLimit       int
	vectorResults     []storage.ScoredEntry
	textSearchResults []storage.ScoredEntry
}

func (f *fakeSearchStore) Search(_ context.Context, _, _ string, _ int) ([]storage.ScoredEntry, error) {
	f.searchCalled = true
	return f.textSearchResults, nil
}

func (f *fakeSearchStore) SearchSimilarByType(_ context.Context, workspace, entryType string, _ []float32, limit int) ([]storage.ScoredEntry, error) {
	f.vectorWorkspace = workspace
	f.vectorType = entryType
	f.vectorLimit = limit
	return f.vectorResults, nil
}

func TestSearchFileSummariesUsesTypeSpecificVectorSearchAndFiltersFileEmbeddings(t *testing.T) {
	payload, err := symbol.MarshalFileSummaryResult(symbol.FileSummaryResult{
		FilePath: "internal/intelligence/indexing/filesummary/search.go",
		Digest:   "digest",
	})
	if err != nil {
		t.Fatalf("marshal file summary result: %v", err)
	}
	store := &fakeSearchStore{
		vectorResults: []storage.ScoredEntry{
			{
				Entry: storage.NamedEntry{
					Name:      "file://workspace/internal/intelligence/indexing/filesummary/search.go#chunk-0",
					Type:      "file_embedding_chunk",
					Workspace: "workspace",
					Summary:   "polluting chunk summary",
				},
				Score: 0.99,
			},
			{
				Entry: storage.NamedEntry{
					Name:      "file://workspace/internal/intelligence/indexing/filesummary/search.go",
					Type:      "file_embedding",
					Workspace: "workspace",
					Summary:   "polluting raw file summary",
				},
				Score: 0.98,
			},
			{
				Entry: storage.NamedEntry{
					Name:      "file://workspace/internal/intelligence/indexing/filesummary/search.go",
					Type:      symbol.FileSummaryType,
					Workspace: "workspace",
					Summary:   "retrieval-safe file summary",
					Result:    payload,
				},
				Score: 0.72,
			},
		},
	}

	entries, err := SearchFileSummaries(context.Background(), store, "workspace", "file summary retrieval", []float32{1, 0}, 10)
	if err != nil {
		t.Fatalf("SearchFileSummaries: %v", err)
	}
	if store.vectorWorkspace != "workspace" {
		t.Fatalf("workspace=%q want workspace", store.vectorWorkspace)
	}
	if store.vectorType != symbol.FileSummaryType {
		t.Fatalf("vector type=%q want %q", store.vectorType, symbol.FileSummaryType)
	}
	if store.vectorLimit == 0 {
		t.Fatal("expected vector search limit to be passed")
	}
	if store.searchCalled {
		t.Fatal("did not expect BM25 fallback when vector file_summary entries are found")
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1: %#v", len(entries), entries)
	}
	if entries[0].Path != "internal/intelligence/indexing/filesummary/search.go" {
		t.Fatalf("path=%q", entries[0].Path)
	}
	if entries[0].Summary != "retrieval-safe file summary" {
		t.Fatalf("summary=%q", entries[0].Summary)
	}
}

func TestSearchFileSummariesFallbackNameParsingKeepsNestedPath(t *testing.T) {
	store := &fakeSearchStore{
		vectorResults: []storage.ScoredEntry{
			{
				Entry: storage.NamedEntry{
					Name:      "file://workspace/internal/intelligence/indexing/filesummary/search.go",
					Type:      symbol.FileSummaryType,
					Workspace: "workspace",
					Summary:   "summary without structured payload",
				},
				Score: 0.88,
			},
		},
	}

	entries, err := SearchFileSummaries(context.Background(), store, "workspace", "nested path", []float32{1, 0}, 10)
	if err != nil {
		t.Fatalf("SearchFileSummaries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1: %#v", len(entries), entries)
	}
	if entries[0].Path != "internal/intelligence/indexing/filesummary/search.go" {
		t.Fatalf("path=%q want full nested path", entries[0].Path)
	}
}

func TestSearchFileSummariesPrefersStructuredFilePath(t *testing.T) {
	payload, err := symbol.MarshalFileSummaryResult(symbol.FileSummaryResult{
		FilePath: "cmd/foxctl/main.go",
		Digest:   "digest",
	})
	if err != nil {
		t.Fatalf("marshal file summary result: %v", err)
	}
	store := &fakeSearchStore{
		vectorResults: []storage.ScoredEntry{
			{
				Entry: storage.NamedEntry{
					Name:      "file://workspace/truncated/main.go",
					Type:      symbol.FileSummaryType,
					Workspace: "workspace",
					Summary:   "structured payload wins",
					Result:    payload,
				},
				Score: 0.88,
			},
		},
	}

	entries, err := SearchFileSummaries(context.Background(), store, "workspace", "structured path", []float32{1, 0}, 10)
	if err != nil {
		t.Fatalf("SearchFileSummaries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1: %#v", len(entries), entries)
	}
	if entries[0].Path != "cmd/foxctl/main.go" {
		t.Fatalf("path=%q want structured payload path", entries[0].Path)
	}
}
