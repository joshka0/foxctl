package retrievalv2

import (
	"github.com/jkatigb/agentctl/internal/intelligence/searchindex"
	"github.com/jkatigb/agentctl/internal/intelligence/searchrank"
)

// Fuse combines per-source results into cross-source ranked hits.
func Fuse(sourceHits map[SourceID][]SourceHit, opts FuseOptions) ([]FusedHit, []FusedHit) {
	fused := searchrank.Fuse(toSearchRankSourceHits(sourceHits), toSearchRankFuseOptions(opts))
	out := fromSearchRankFusedHits(fused)
	return out, out
}

func toSearchRankSourceHits(sourceHits map[SourceID][]SourceHit) map[searchrank.SourceID][]searchrank.SourceHit[searchindex.Document] {
	out := make(map[searchrank.SourceID][]searchrank.SourceHit[searchindex.Document], len(sourceHits))
	for sourceID, hits := range sourceHits {
		converted := make([]searchrank.SourceHit[searchindex.Document], 0, len(hits))
		for _, hit := range hits {
			converted = append(converted, searchrank.SourceHit[searchindex.Document]{
				Source:   searchrank.SourceID(hit.Source),
				ID:       hit.ID,
				Document: hit.Document,
				Score:    hit.Score,
				Rank:     hit.Rank,
			})
		}
		out[searchrank.SourceID(sourceID)] = converted
	}
	return out
}

func toSearchRankFuseOptions(opts FuseOptions) searchrank.FuseOptions {
	out := searchrank.FuseOptions{
		Mode:            searchrank.FuseMode(opts.Mode),
		TopK:            opts.TopK,
		RRFK:            opts.RRFK,
		MaxContributors: opts.MaxContributors,
	}
	if opts.SourceWeights != nil {
		out.SourceWeights = make(map[searchrank.SourceID]float64, len(opts.SourceWeights))
		for sourceID, weight := range opts.SourceWeights {
			out.SourceWeights[searchrank.SourceID(sourceID)] = weight
		}
	}
	return out
}

func fromSearchRankFusedHits(in []searchrank.FusedHit[searchindex.Document]) []FusedHit {
	out := make([]FusedHit, 0, len(in))
	for _, hit := range in {
		converted := FusedHit{
			ID:            hit.ID,
			Document:      hit.Document,
			Score:         hit.Score,
			Sources:       make([]SourceID, 0, len(hit.Sources)),
			SourceScores:  make(map[SourceID]float64, len(hit.SourceScores)),
			Contributions: make([]SourceContribution, 0, len(hit.Contributions)),
		}
		for _, sourceID := range hit.Sources {
			converted.Sources = append(converted.Sources, SourceID(sourceID))
		}
		for sourceID, score := range hit.SourceScores {
			converted.SourceScores[SourceID(sourceID)] = score
		}
		for _, contribution := range hit.Contributions {
			converted.Contributions = append(converted.Contributions, SourceContribution{
				Source: SourceID(contribution.Source),
				Score:  contribution.Score,
				Rank:   contribution.Rank,
			})
		}
		out = append(out, converted)
	}
	return out
}
