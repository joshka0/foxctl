package vector

import (
	"context"
	"fmt"
	"testing"
	"testing/quick"
)

func TestBruteForceSearcherRanksByCosineAndHonorsK(t *testing.T) {
	t.Parallel()

	searcher := &BruteForceSearcher{}
	query := []float32{1, 0}
	candidates := []IDEmbedding{
		{ID: "orthogonal", Embedding: []float32{0, 1}},
		{ID: "opposite", Embedding: []float32{-1, 0}},
		{ID: "diagonal", Embedding: []float32{1, 1}},
		{ID: "best", Embedding: []float32{1, 0}},
	}

	got, err := searcher.Search(context.Background(), query, candidates, 2)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(Search())=%d want 2: %+v", len(got), got)
	}
	if got[0].ID != "best" || got[1].ID != "diagonal" {
		t.Fatalf("Search() order=%+v want best, diagonal", got)
	}
	if got[0].Score < got[1].Score {
		t.Fatalf("Search() scores not descending: %+v", got)
	}
}

func TestBruteForceSearcherFilteredResultsStayWithinAllowlist(t *testing.T) {
	t.Parallel()

	searcher := &BruteForceSearcher{}
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
			t.Fatalf("SearchFiltered() returned filtered-out candidate: %+v", got)
		}
	}
	if got[0].ID != "allowed-diagonal" || got[1].ID != "allowed-orthogonal" {
		t.Fatalf("SearchFiltered() order=%+v want allowed-diagonal, allowed-orthogonal", got)
	}
}

func TestBruteForceSearcherBreaksEqualScoreTiesByID(t *testing.T) {
	t.Parallel()

	searcher := &BruteForceSearcher{}
	query := []float32{1, 0}
	candidates := []IDEmbedding{
		{ID: "zeta", Embedding: []float32{1, 0}},
		{ID: "alpha", Embedding: []float32{1, 0}},
		{ID: "middle", Embedding: []float32{1, 0}},
	}

	got, err := searcher.Search(context.Background(), query, candidates, 3)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	wantIDs := []string{"alpha", "middle", "zeta"}
	if len(got) != len(wantIDs) {
		t.Fatalf("len(Search())=%d want %d: %+v", len(got), len(wantIDs), got)
	}
	for i, wantID := range wantIDs {
		if got[i].ID != wantID {
			t.Fatalf("Search() tie order=%+v want IDs %v", got, wantIDs)
		}
	}
}

func TestBruteForceSearchPropertyReturnsAllowedTopKInDescendingOrder(t *testing.T) {
	t.Parallel()

	property := func(rawCandidates []uint8, rawK uint8, rawAllow uint8) bool {
		candidates := generatedVectorCandidates(rawCandidates)
		k := int(rawK % 8)
		allowlist := generatedAllowlist(candidates, rawAllow)

		got := bruteForceSearch([]float32{1, 0}, candidates, k, allowlist)
		if k == 0 {
			return len(got) == 0
		}
		if len(got) > k {
			t.Logf("len(got)=%d exceeds k=%d: %+v", len(got), k, got)
			return false
		}

		allowedScores := make(map[string]float64, len(candidates))
		for _, candidate := range candidates {
			if allowlist != nil && !allowlist[candidate.ID] {
				continue
			}
			allowedScores[candidate.ID] = Cosine([]float32{1, 0}, candidate.Embedding)
		}
		if len(got) > len(allowedScores) {
			t.Logf("len(got)=%d exceeds allowed candidates=%d", len(got), len(allowedScores))
			return false
		}

		for i, result := range got {
			wantScore, ok := allowedScores[result.ID]
			if !ok {
				t.Logf("result %q not allowed; allowlist=%v got=%+v", result.ID, allowlist, got)
				return false
			}
			if result.Score != wantScore {
				t.Logf("result %q score=%v want %v", result.ID, result.Score, wantScore)
				return false
			}
			if i > 0 {
				previous := got[i-1]
				if previous.Score < result.Score {
					t.Logf("scores not descending: %+v", got)
					return false
				}
				if previous.Score == result.Score && previous.ID > result.ID {
					t.Logf("equal scores not ordered by ID: %+v", got)
					return false
				}
			}
		}

		if len(got) == 0 || len(got) < k {
			return true
		}
		tailScore := got[len(got)-1].Score
		return noAllowedCandidateOutranksTail(candidates, allowlist, got, tailScore)
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func generatedVectorCandidates(raw []uint8) []IDEmbedding {
	if len(raw) == 0 {
		return nil
	}
	out := make([]IDEmbedding, 0, len(raw))
	for i, seed := range raw {
		out = append(out, IDEmbedding{
			ID:        fmt.Sprintf("id-%02d", i),
			Embedding: generatedVectorEmbedding(seed),
		})
		if len(out) >= 16 {
			break
		}
	}
	return out
}

func generatedVectorEmbedding(seed uint8) []float32 {
	switch seed % 6 {
	case 0:
		return []float32{1, 0}
	case 1:
		return []float32{1, 1}
	case 2:
		return []float32{0, 1}
	case 3:
		return []float32{-1, 0}
	case 4:
		return []float32{0, 0}
	default:
		return []float32{1, -1}
	}
}

func generatedAllowlist(candidates []IDEmbedding, seed uint8) map[string]bool {
	if seed%5 == 0 {
		return nil
	}
	allowlist := make(map[string]bool, len(candidates))
	for i, candidate := range candidates {
		if (i+int(seed))%3 != 0 {
			allowlist[candidate.ID] = true
		}
	}
	return allowlist
}

func noAllowedCandidateOutranksTail(candidates []IDEmbedding, allowlist map[string]bool, results []ScoredID, tailScore float64) bool {
	returned := make(map[string]bool, len(results))
	for _, result := range results {
		returned[result.ID] = true
	}
	for _, candidate := range candidates {
		if returned[candidate.ID] {
			continue
		}
		if allowlist != nil && !allowlist[candidate.ID] {
			continue
		}
		if score := Cosine([]float32{1, 0}, candidate.Embedding); score > tailScore {
			return false
		}
	}
	return true
}
