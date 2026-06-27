package memoryrecall

import "strings"

// DefaultLifecycleAllows applies the named-memory recall default lifecycle
// gate. Explicit lifecycle filters live at the caller layer.
func DefaultLifecycleAllows(state string, score float64, query string) bool {
	return LifecycleAllowsThreshold(state, score, query, defaultStandardCandidateThreshold)
}

// LifecycleAllowsThreshold applies the named-memory recall lifecycle gate
// using the provided candidate threshold instead of the default standard
// threshold. Callers that classify queries as "deep" (exploratory,
// aggregation, categorical) should pass defaultDeepCandidateThreshold.
func LifecycleAllowsThreshold(state string, score float64, query string, candidateThreshold float64) bool {
	switch strings.TrimSpace(state) {
	case "", "active":
		return true
	case "candidate", "stale":
		if strings.TrimSpace(query) == "" {
			return false
		}
		return score >= candidateThreshold
	default:
		return false
	}
}

// QueryClassDeep returns true for queries that should use the deep-mode
// candidate threshold (wider recall). Deep queries include aggregations,
// broad categories, recommendations, and multi-session questions — patterns
// adapted from Quarq's search_mode heuristic but implemented as a pure
// string classifier without keyword heuristics on classification decisions.
// The classification is intentionally conservative: only queries that clearly
// ask for broad/exploratory recall are classified as deep.
func QueryClassDeep(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	// Multi-clause questions (contain "and" joining two distinct question
	// clauses) are exploratory.
	return strings.Count(q, "?") > 1
}

// CandidateThresholdForQuery returns the appropriate candidate threshold
// for the given query.
func CandidateThresholdForQuery(query string) float64 {
	if QueryClassDeep(query) {
		return defaultDeepCandidateThreshold
	}
	return defaultStandardCandidateThreshold
}

// QuerySimilarityAllows applies the default named-memory query similarity gate.
func QuerySimilarityAllows(score float64, query string, minSimilarity float64) bool {
	if strings.TrimSpace(query) == "" {
		return true
	}
	if minSimilarity <= 0 {
		minSimilarity = DefaultMinSimilarity
	}
	return score >= minSimilarity
}
