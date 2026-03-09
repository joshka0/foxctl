package searchrank

import "sort"

// SourceID identifies a recall source in a fusion run.
type SourceID string

// FuseMode selects how per-source results are combined.
type FuseMode string

const (
	// FuseModeRRF uses reciprocal-rank fusion.
	FuseModeRRF FuseMode = "rrf"
	// FuseModeWeighted uses source-weighted score summation.
	FuseModeWeighted FuseMode = "weighted"
)

// FuseOptions controls cross-source fusion.
type FuseOptions struct {
	Mode            FuseMode
	TopK            int
	RRFK            float64
	SourceWeights   map[SourceID]float64
	MaxContributors int
}

// SourceHit carries one per-source candidate with rank and optional raw score.
type SourceHit[T any] struct {
	Source   SourceID `json:"source"`
	ID       string   `json:"id"`
	Document T        `json:"-"`
	Score    float64  `json:"score"`
	Rank     int      `json:"rank"`
}

// SourceContribution stores per-source influence on one fused hit.
type SourceContribution struct {
	Source SourceID `json:"source"`
	Score  float64  `json:"score"`
	Rank   int      `json:"rank"`
}

// FusedHit is the unit after cross-source fusion.
type FusedHit[T any] struct {
	ID            string               `json:"-"`
	Document      T                    `json:"document"`
	Score         float64              `json:"score"`
	Sources       []SourceID           `json:"sources"`
	SourceScores  map[SourceID]float64 `json:"source_scores"`
	Contributions []SourceContribution `json:"contributions"`
}

// Fuse combines per-source ranked hits into cross-source ranked hits.
func Fuse[T any](sourceHits map[SourceID][]SourceHit[T], opts FuseOptions) []FusedHit[T] {
	if len(sourceHits) == 0 {
		return nil
	}

	if opts.Mode == "" {
		opts.Mode = FuseModeRRF
	}
	if opts.TopK == 0 {
		opts.TopK = 60
	}
	if opts.RRFK == 0 {
		opts.RRFK = 60
	}
	if opts.SourceWeights == nil {
		opts.SourceWeights = map[SourceID]float64{}
	}

	fusedByID := map[string]FusedHit[T]{}

	for sourceID, hits := range sourceHits {
		hitsCopy := make([]SourceHit[T], len(hits))
		copy(hitsCopy, hits)
		sort.SliceStable(hitsCopy, func(i, j int) bool {
			if hitsCopy[i].Score == hitsCopy[j].Score {
				return hitsCopy[i].Rank < hitsCopy[j].Rank
			}
			return hitsCopy[i].Score > hitsCopy[j].Score
		})

		maxScore := 0.0
		for _, hit := range hitsCopy {
			if hit.Score > maxScore {
				maxScore = hit.Score
			}
		}
		if maxScore == 0 {
			maxScore = 1
		}
		weight := opts.SourceWeights[sourceID]
		if weight == 0 {
			weight = 1
		}

		seen := map[string]struct{}{}
		for i, hit := range hitsCopy {
			docID := hit.ID
			if docID == "" {
				continue
			}
			if _, ok := seen[docID]; ok {
				continue
			}
			seen[docID] = struct{}{}

			rank := i + 1
			base := hit.Score / maxScore

			contrib := 0.0
			switch opts.Mode {
			case FuseModeWeighted:
				contrib = weight * base
			default:
				contrib = weight / (opts.RRFK + float64(rank))
			}

			item, ok := fusedByID[docID]
			if !ok {
				item = FusedHit[T]{
					ID:           docID,
					Document:     hit.Document,
					Score:        0,
					SourceScores: map[SourceID]float64{},
				}
			}
			item.Score += contrib
			item.SourceScores[sourceID] = item.SourceScores[sourceID] + contrib
			item.Contributions = append(item.Contributions, SourceContribution{Source: sourceID, Score: contrib, Rank: rank})
			fusedByID[docID] = item
		}
	}

	fused := make([]FusedHit[T], 0, len(fusedByID))
	for _, hit := range fusedByID {
		sourceCount := len(hit.SourceScores)
		if opts.MaxContributors > 0 && sourceCount > opts.MaxContributors {
			hit.SourceScores = truncateScores(hit.SourceScores, opts.MaxContributors)
		}
		hit.Sources = make([]SourceID, 0, len(hit.SourceScores))
		for sourceID := range hit.SourceScores {
			hit.Sources = append(hit.Sources, sourceID)
		}
		sort.Slice(hit.Sources, func(i, j int) bool {
			return hit.Sources[i] < hit.Sources[j]
		})
		sort.Slice(hit.Contributions, func(i, j int) bool {
			if hit.Contributions[i].Source == hit.Contributions[j].Source {
				return hit.Contributions[i].Rank < hit.Contributions[j].Rank
			}
			return hit.Contributions[i].Source < hit.Contributions[j].Source
		})

		fused = append(fused, hit)
	}

	sort.SliceStable(fused, func(i, j int) bool {
		if fused[i].Score == fused[j].Score {
			if len(fused[i].Sources) != len(fused[j].Sources) {
				return len(fused[i].Sources) > len(fused[j].Sources)
			}
			if fused[i].ID != fused[j].ID {
				return fused[i].ID < fused[j].ID
			}
		}
		return fused[i].Score > fused[j].Score
	})

	if opts.TopK > 0 && len(fused) > opts.TopK {
		fused = fused[:opts.TopK]
	}

	return fused
}

func truncateScores(scores map[SourceID]float64, max int) map[SourceID]float64 {
	type kv struct {
		source SourceID
		value  float64
	}
	pairs := make([]kv, 0, len(scores))
	for s, v := range scores {
		pairs = append(pairs, kv{source: s, value: v})
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].value == pairs[j].value {
			return pairs[i].source < pairs[j].source
		}
		return pairs[i].value > pairs[j].value
	})

	if len(pairs) <= max {
		return scores
	}

	truncated := map[SourceID]float64{}
	for _, item := range pairs[:max] {
		truncated[item.source] = item.value
	}
	return truncated
}
