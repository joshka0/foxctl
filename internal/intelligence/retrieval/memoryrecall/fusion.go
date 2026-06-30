package memoryrecall

import (
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/searchrank"
	"github.com/joshka0/foxctl/internal/storage"
)

func fuseResults(vectorEntries, lexicalEntries []storage.ScoredEntry, limit int) []storage.ScoredEntry {
	sourceHits := map[searchrank.SourceID][]searchrank.SourceHit[storage.ScoredEntry]{
		sourceVector:  sourceHitsFromScoredEntries(sourceVector, vectorEntries),
		sourceLexical: sourceHitsFromScoredEntries(sourceLexical, lexicalEntries),
	}
	// BM25-dominant fusion weights: LongMemEval questions are mostly factual
	// with exact vocabulary overlap between question and evidence. Vector
	// search adds recall for synonymy but its lower precision on factual
	// queries causes regressions when weighted equally. BM25-dominant (0.75)
	// preserves exact-match ranking while still allowing vector results to
	// break ties and surface semantic near-misses.
	fused := searchrank.Fuse(sourceHits, searchrank.FuseOptions{
		Mode: searchrank.FuseModeWeighted,
		TopK: limit,
		RRFK: 60,
		SourceWeights: map[searchrank.SourceID]float64{
			sourceVector:  0.25,
			sourceLexical: 0.75,
		},
		MaxContributors: 2,
	})
	results := make([]storage.ScoredEntry, 0, len(fused))
	for _, hit := range fused {
		scored := hit.Document
		scored.Score = hit.Score
		results = append(results, scored)
	}
	return results
}

func sourceHitsFromScoredEntries(source searchrank.SourceID, entries []storage.ScoredEntry) []searchrank.SourceHit[storage.ScoredEntry] {
	hits := make([]searchrank.SourceHit[storage.ScoredEntry], 0, len(entries))
	for i, entry := range entries {
		id := strings.TrimSpace(entry.Entry.Name)
		if id == "" {
			id = strings.TrimSpace(entry.Entry.ID)
		}
		if id == "" {
			continue
		}
		hits = append(hits, searchrank.SourceHit[storage.ScoredEntry]{
			Source:   source,
			ID:       id,
			Document: entry,
			Score:    entry.Score,
			Rank:     i + 1,
		})
	}
	return hits
}
