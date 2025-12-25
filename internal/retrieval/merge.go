package retrieval

import (
	"sort"
)

// mergeCandidates merges candidates from multiple sources, deduplicates by path,
// and returns a ranked list.
//
// The merge process:
//  1. Apply source weights to scores
//  2. Group by file path
//  3. For each path, keep the best candidate (merge metadata)
//  4. Sort by final score
//  5. Apply limit
func mergeCandidates(sources [][]Candidate, weights map[string]float64, maxTotal int) []Candidate {
	if len(sources) == 0 {
		return nil
	}

	// Group candidates by path
	byPath := make(map[string]*mergedCandidate)

	for _, batch := range sources {
		for _, c := range batch {
			if c.Path == "" {
				continue
			}

			// Apply source weight
			weight := weights[c.Source]
			if weight == 0 {
				weight = 1.0
			}
			weightedScore := c.Score * weight

			existing, ok := byPath[c.Path]
			if !ok {
				// First occurrence of this path
				byPath[c.Path] = &mergedCandidate{
					candidate:   c,
					totalScore:  weightedScore,
					sourceCount: 1,
					sources:     []string{c.Source},
				}
			} else {
				// Merge with existing
				existing.merge(c, weightedScore)
			}
		}
	}

	// Convert to slice and finalize scores
	result := make([]Candidate, 0, len(byPath))
	for _, mc := range byPath {
		final := mc.finalize()
		result = append(result, final)
	}

	// Sort by score descending
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		// Stable tie-breaking by path
		return result[i].Path < result[j].Path
	})

	// Apply limit
	if maxTotal > 0 && len(result) > maxTotal {
		result = result[:maxTotal]
	}

	return result
}

// mergedCandidate tracks a candidate during the merge process.
type mergedCandidate struct {
	candidate   Candidate // Base candidate (first seen)
	totalScore  float64   // Sum of weighted scores
	sourceCount int       // Number of sources
	sources     []string  // List of source names
}

// merge incorporates another candidate for the same path.
func (mc *mergedCandidate) merge(other Candidate, weightedScore float64) {
	// Accumulate score (could use max instead of sum)
	mc.totalScore += weightedScore
	mc.sourceCount++

	// Track source for debugging
	mc.sources = append(mc.sources, other.Source)

	// Prefer symbol metadata over others
	if other.SymbolID != "" && mc.candidate.SymbolID == "" {
		mc.candidate.SymbolID = other.SymbolID
		mc.candidate.Name = other.Name
		mc.candidate.Kind = other.Kind
		mc.candidate.Line = other.Line
	}

	// Keep best raw score for reference
	if other.RawScore > mc.candidate.RawScore {
		mc.candidate.RawScore = other.RawScore
	}
}

// finalize produces the final candidate.
func (mc *mergedCandidate) finalize() Candidate {
	c := mc.candidate

	// Average score across sources (normalize by count for fairness)
	// This prevents candidates appearing in multiple sources from
	// dominating purely based on count.
	c.Score = mc.totalScore / float64(mc.sourceCount)

	// Boost candidates found in multiple sources (multi-source bonus)
	// This reflects higher confidence when multiple retrieval methods agree.
	if mc.sourceCount > 1 {
		// Logarithmic bonus to avoid runaway scores
		c.Score *= 1.0 + 0.2*float64(mc.sourceCount-1)
	}

	// Normalize final score to 0-1 range
	if c.Score > 1.0 {
		c.Score = 1.0
	}

	// Update source to indicate multiple sources
	if mc.sourceCount > 1 {
		c.Source = "merged"
	}

	return c
}

// MergeOptions controls merge behavior.
type MergeOptions struct {
	// ScoreMode determines how scores are combined
	//   "sum" - Add weighted scores (default)
	//   "max" - Take maximum weighted score
	//   "avg" - Average weighted scores
	ScoreMode string

	// MultiSourceBonus is multiplied for each additional source
	// Default: 1.2 (20% boost per additional source)
	MultiSourceBonus float64
}

// DefaultMergeOptions returns sensible merge defaults.
func DefaultMergeOptions() MergeOptions {
	return MergeOptions{
		ScoreMode:        "avg",
		MultiSourceBonus: 1.2,
	}
}
