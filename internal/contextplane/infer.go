package contextplane

import "strings"

// InferenceResult contains suggested observations and tensions for a summary.
type InferenceResult struct {
	Observations []Observation `json:"observations,omitempty"`
	Tensions     []Tension     `json:"tensions,omitempty"`
}

type inferenceRule struct {
	kind       string
	patterns   []string
	confidence float64
}

var inferenceRules = []inferenceRule{
	{
		kind:       "tension",
		patterns:   []string{"contradict", "conflict", "blocked", "drift", "stale", "mismatch"},
		confidence: 0.65,
	},
	{
		kind:       "observation",
		patterns:   []string{"work better", "works better", "better when", "prefer", "pattern", "should keep", "should preserve", "should use"},
		confidence: 0.55,
	},
}

// InferInsights converts a compact summary into structured ACA observation/tension suggestions.
func InferInsights(summary, project, area string, evidenceRefs []string) InferenceResult {
	summary = normalizeSummary(summary)
	if summary == "" {
		return InferenceResult{}
	}
	lowered := strings.ToLower(summary)
	result := InferenceResult{}
	for _, rule := range inferenceRules {
		if !matchesAny(lowered, rule.patterns) {
			continue
		}
		switch rule.kind {
		case "observation":
			result.Observations = append(result.Observations, Observation{
				Statement:    summary,
				Confidence:   rule.confidence,
				Count:        1,
				Project:      strings.TrimSpace(project),
				Area:         strings.TrimSpace(area),
				EvidenceRefs: uniqueStrings(evidenceRefs),
			})
		case "tension":
			result.Tensions = append(result.Tensions, Tension{
				Kind:        "contradiction",
				Statement:   summary,
				Impact:      "medium",
				Status:      "open",
				RelatedRefs: uniqueStrings(evidenceRefs),
			})
		}
	}
	return result
}

func normalizeSummary(summary string) string {
	summary = strings.ReplaceAll(summary, "\n", " ")
	summary = strings.Join(strings.Fields(summary), " ")
	return strings.TrimSpace(summary)
}

func matchesAny(text string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}
