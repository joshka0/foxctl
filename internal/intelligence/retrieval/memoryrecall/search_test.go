package memoryrecall

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/storage"
)

func TestSearchFusesVectorAndLexicalResults(t *testing.T) {
	store := &fakeStore{
		vector: []storage.ScoredEntry{
			scored("shared", 0.9),
			scored("vector-only", 0.8),
		},
		lexical: []storage.ScoredEntry{
			scored("shared", 0.7),
			scored("lexical-only", 0.9),
		},
	}

	got, err := Search(context.Background(), store, QueryRequest{
		Workspace:      "ws",
		Query:          "hydra recall",
		QueryEmbedding: []float32{1, 0},
		Limit:          5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got.Method != MethodHybrid {
		t.Fatalf("method = %q, want %q", got.Method, MethodHybrid)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(got.Entries))
	}
	if got.Entries[0].Entry.Name != "shared" {
		t.Fatalf("first entry = %q, want shared", got.Entries[0].Entry.Name)
	}
}

func TestSearchRanksLexicalExactAboveVectorOnlyDistractor(t *testing.T) {
	store := &fakeStore{
		vector: []storage.ScoredEntry{
			scored("vector-distractor", 1.0),
		},
		lexical: []storage.ScoredEntry{
			scored("lexical-answer", 1.0),
		},
	}

	got, err := Search(context.Background(), store, QueryRequest{
		Workspace:      "ws",
		Query:          "hydra exact answer",
		QueryEmbedding: []float32{1, 0},
		Limit:          5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got.Method != MethodHybrid {
		t.Fatalf("method = %q, want %q", got.Method, MethodHybrid)
	}
	if got.Entries[0].Entry.Name != "lexical-answer" {
		t.Fatalf("first entry = %q, want lexical-answer", got.Entries[0].Entry.Name)
	}
}

func TestDefaultLifecycleAllowsActiveAndStrongQueryCandidatesOnly(t *testing.T) {
	tests := []struct {
		name  string
		state string
		score float64
		query string
		want  bool
	}{
		{name: "active", state: "active", want: true},
		{name: "empty defaults active", state: "", want: true},
		{name: "strong candidate with query", state: "candidate", score: 0.9, query: "q", want: true},
		{name: "weak candidate", state: "candidate", score: 0.89, query: "q", want: false},
		{name: "strong stale without query", state: "stale", score: 1.0, want: false},
		{name: "quarantined", state: "quarantined", score: 1.0, query: "q", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultLifecycleAllows(tt.state, tt.score, tt.query); got != tt.want {
				t.Fatalf("DefaultLifecycleAllows() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQuerySimilarityAllowsDefaultAndCustomThresholds(t *testing.T) {
	tests := []struct {
		name          string
		score         float64
		query         string
		minSimilarity float64
		want          bool
	}{
		{name: "blank query bypasses similarity", score: 0.0, query: "", want: true},
		{name: "default rejects below threshold", score: 0.29, query: "qwen reranker", want: false},
		{name: "default allows threshold", score: DefaultMinSimilarity, query: "qwen reranker", want: true},
		{name: "custom rejects below threshold", score: 0.79, query: "qwen reranker", minSimilarity: 0.8, want: false},
		{name: "custom allows threshold", score: 0.8, query: "qwen reranker", minSimilarity: 0.8, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := QuerySimilarityAllows(tt.score, tt.query, tt.minSimilarity); got != tt.want {
				t.Fatalf("QuerySimilarityAllows() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNamedEntryTextUsesAtomicTextAndTags(t *testing.T) {
	got := NamedEntryText(storage.NamedEntry{
		Name:       "fallback-name",
		Summary:    "generic summary",
		AtomicText: "Use the local Qwen embedder for LongMem retrieval checks.",
		Entities:   []string{"LongMemEval", "Qwen"},
		Keywords:   []string{"hydra", "reranker", "qwen"},
	})

	for _, want := range []string{
		"Use the local Qwen embedder for LongMem retrieval checks.",
		"LongMemEval",
		"hydra",
		"reranker",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("NamedEntryText() = %q, want %q", got, want)
		}
	}
	if strings.Count(strings.ToLower(got), "qwen") != 2 {
		t.Fatalf("NamedEntryText() = %q, want qwen once in text and once as tag", got)
	}
}

func TestSearchFallsBackToLexicalWhenVectorUnavailable(t *testing.T) {
	store := &fakeStore{
		lexical: []storage.ScoredEntry{scored("lexical", 0.8)},
	}
	vectorErr := errors.New("embedder unavailable")

	got, err := Search(context.Background(), store, QueryRequest{
		Workspace:      "ws",
		Query:          "hydra recall",
		EmbeddingError: vectorErr,
		Limit:          5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got.Method != MethodBM25 {
		t.Fatalf("method = %q, want %q", got.Method, MethodBM25)
	}
	if !strings.Contains(got.Hint, vectorErr.Error()) {
		t.Fatalf("hint = %q, want vector error", got.Hint)
	}
	if got.Entries[0].Entry.Name != "lexical" {
		t.Fatalf("entry = %q, want lexical", got.Entries[0].Entry.Name)
	}
}

func TestSearchKeepsVectorResultsWhenLexicalFails(t *testing.T) {
	store := &fakeStore{
		vector:     []storage.ScoredEntry{scored("vector", 0.9)},
		lexicalErr: errors.New("lexical unavailable"),
	}

	got, err := Search(context.Background(), store, QueryRequest{
		Workspace:      "ws",
		Query:          "hydra recall",
		QueryEmbedding: []float32{1, 0},
		Limit:          5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got.Method != MethodVector {
		t.Fatalf("method = %q, want %q", got.Method, MethodVector)
	}
	if !strings.Contains(got.Hint, "BM25 search failed") {
		t.Fatalf("hint = %q, want BM25 failure", got.Hint)
	}
}

func TestSearchReturnsLexicalErrorWhenNoVectorFallbackExists(t *testing.T) {
	store := &fakeStore{
		lexicalErr: errors.New("lexical unavailable"),
	}

	_, err := Search(context.Background(), store, QueryRequest{
		Workspace:      "ws",
		Query:          "hydra recall",
		EmbeddingError: errors.New("embedder unavailable"),
		Limit:          5,
	})
	if err == nil || !strings.Contains(err.Error(), "lexical unavailable") {
		t.Fatalf("Search() error = %v, want lexical error", err)
	}
}

type fakeStore struct {
	vector     []storage.ScoredEntry
	lexical    []storage.ScoredEntry
	vectorErr  error
	lexicalErr error
}

func (s *fakeStore) Search(context.Context, string, string, int) ([]storage.ScoredEntry, error) {
	if s.lexicalErr != nil {
		return nil, s.lexicalErr
	}
	return s.lexical, nil
}

func (s *fakeStore) SearchSimilar(ctx context.Context, _ string, _ []float32, _ int) ([]storage.ScoredEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.vectorErr != nil {
		return nil, s.vectorErr
	}
	return s.vector, nil
}

func scored(name string, score float64) storage.ScoredEntry {
	return storage.ScoredEntry{
		Entry: storage.NamedEntry{Name: name, ID: name},
		Score: score,
	}
}
