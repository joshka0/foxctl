package vector

import (
	"context"
	"testing"
)

func TestTurbovecSearcherFilteredFallbackPreservesAllowlist(t *testing.T) {
	t.Parallel()

	searcher := &TurbovecSearcher{
		fallback:    &BruteForceSearcher{},
		populated:   make(map[string]bool),
		idMaps:      make(map[string]map[string]uint64),
		reverseMaps: make(map[string]map[uint64]string),
	}
	query := []float32{1, 0}
	candidates := []IDEmbedding{
		{ID: "best", Embedding: []float32{1, 0}},
		{ID: "allowed-diagonal", Embedding: []float32{1, 1}},
		{ID: "allowed-orthogonal", Embedding: []float32{0, 1}},
	}

	got, err := searcher.SearchFiltered(context.Background(), query, candidates, 3, map[string]bool{
		"allowed-diagonal":   true,
		"allowed-orthogonal": true,
	})
	if err != nil {
		t.Fatalf("SearchFiltered() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(SearchFiltered())=%d want 2: %+v", len(got), got)
	}
	for _, result := range got {
		if result.ID == "best" {
			t.Fatalf("SearchFiltered() fallback returned filtered-out candidate: %+v", got)
		}
	}
	if got[0].ID != "allowed-diagonal" || got[1].ID != "allowed-orthogonal" {
		t.Fatalf("SearchFiltered() fallback order=%+v want allowed-diagonal, allowed-orthogonal", got)
	}
}
