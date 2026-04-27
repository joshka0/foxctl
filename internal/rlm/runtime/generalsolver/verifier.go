package generalsolver

import (
	"fmt"
	"strings"
)

const defaultVerifierConfidenceThreshold = 0.5

type VerifierTier int

const (
	VerifierTier1 VerifierTier = 1
	VerifierTier2 VerifierTier = 2
	VerifierTier3 VerifierTier = 3
)

type VerifierCheck func(artifact WorkArtifact, item WorkItem) VerifierResult

type VerifierResult struct {
	Pass       bool           `json:"pass"`
	Tier       VerifierTier   `json:"tier"`
	Confidence float64        `json:"confidence"`
	Details    []string       `json:"details,omitempty"`
	Feedback   map[string]any `json:"feedback,omitempty"`
}

type VerifierStack struct {
	checks     []VerifierCheck
	tier1Count int
	tier2Count int
	tier3Count int
}

func NewVerifierStack() *VerifierStack {
	return &VerifierStack{}
}

func (s *VerifierStack) AddCheck(tier VerifierTier, check VerifierCheck) {
	s.checks = append(s.checks, check)
	switch tier {
	case VerifierTier1:
		s.tier1Count++
	case VerifierTier2:
		s.tier2Count++
	case VerifierTier3:
		s.tier3Count++
	}
}

func (s *VerifierStack) Verify(artifact WorkArtifact, item WorkItem) WorkVerdict {
	if len(s.checks) == 0 {
		return WorkVerdict{
			Accept:     true,
			Repairable: false,
			Confidence: 1.0,
		}
	}

	var minConfidence float64 = 1.0
	var allFeedback = make(map[string]any)
	var allDetails []string
	allPassed := true
	anyRepairable := false

	for _, check := range s.checks {
		result := check(artifact, item)
		if result.Confidence < minConfidence {
			minConfidence = result.Confidence
		}
		allDetails = append(allDetails, result.Details...)
		for k, v := range result.Feedback {
			allFeedback[k] = v
		}
		if !result.Pass {
			allPassed = false
			if len(result.Details) > 0 {
				anyRepairable = true
			}
		}
	}

	return WorkVerdict{
		Accept:     allPassed && minConfidence >= defaultVerifierConfidenceThreshold,
		Repairable: !allPassed && anyRepairable,
		Confidence: minConfidence,
		Feedback:   allFeedback,
	}
}

func (s *VerifierStack) TierCounts() (t1, t2, t3 int) {
	return s.tier1Count, s.tier2Count, s.tier3Count
}

func SchemaCheck(artifact WorkArtifact, item WorkItem) VerifierResult {
	var details []string
	if artifact.WorkItemID != item.ID {
		details = append(details, fmt.Sprintf("artifact work_item_id %q does not match item id %q", artifact.WorkItemID, item.ID))
	}
	if artifact.Status != "solved" && artifact.Status != "partial" {
		if artifact.Status == "" {
			details = append(details, "artifact status is empty")
		} else {
			details = append(details, fmt.Sprintf("artifact status %q is not solved or partial", artifact.Status))
		}
	}
	if artifact.Status == "solved" && artifact.Answer == nil {
		details = append(details, "artifact status is solved but answer is nil")
	}
	return VerifierResult{
		Pass:       len(details) == 0,
		Tier:       VerifierTier1,
		Confidence: 1.0,
		Details:    details,
	}
}

func ConfidenceThresholdCheck(threshold float64) VerifierCheck {
	return func(artifact WorkArtifact, item WorkItem) VerifierResult {
		if artifact.Confidence >= threshold {
			return VerifierResult{
				Pass:       true,
				Tier:       VerifierTier1,
				Confidence: artifact.Confidence,
			}
		}
		return VerifierResult{
			Pass:       false,
			Tier:       VerifierTier1,
			Confidence: artifact.Confidence,
			Details:    []string{fmt.Sprintf("artifact confidence %.2f below threshold %.2f", artifact.Confidence, threshold)},
			Feedback:   map[string]any{"confidence": artifact.Confidence, "threshold": threshold},
		}
	}
}

func MaxAttemptsCheck(state *SolverState) VerifierCheck {
	return func(artifact WorkArtifact, item WorkItem) VerifierResult {
		if state == nil {
			return VerifierResult{Pass: true, Tier: VerifierTier1, Confidence: 1.0}
		}
		current, exists := state.Items[item.ID]
		if !exists {
			return VerifierResult{Pass: true, Tier: VerifierTier1, Confidence: 1.0}
		}
		if current.Attempts >= current.MaxAttempts {
			return VerifierResult{
				Pass:       false,
				Tier:       VerifierTier1,
				Confidence: 0.0,
				Details:    []string{fmt.Sprintf("work item %q has reached max attempts %d", item.ID, current.MaxAttempts)},
			}
		}
		return VerifierResult{Pass: true, Tier: VerifierTier1, Confidence: 1.0}
	}
}

func FormatCheck(requiredPrefix string) VerifierCheck {
	return func(artifact WorkArtifact, item WorkItem) VerifierResult {
		if requiredPrefix == "" {
			return VerifierResult{Pass: true, Tier: VerifierTier1, Confidence: 1.0}
		}
		answer, ok := artifact.Answer.(string)
		if !ok {
			return VerifierResult{
				Pass:       false,
				Tier:       VerifierTier1,
				Confidence: 0.0,
				Details:    []string{"answer is not a string for format check"},
			}
		}
		if strings.HasPrefix(strings.TrimSpace(answer), requiredPrefix) {
			return VerifierResult{Pass: true, Tier: VerifierTier1, Confidence: 1.0}
		}
		return VerifierResult{
			Pass:       false,
			Tier:       VerifierTier1,
			Confidence: 0.3,
			Details:    []string{fmt.Sprintf("answer does not start with required prefix %q", requiredPrefix)},
			Feedback:   map[string]any{"required_prefix": requiredPrefix, "answer_preview": truncateString(answer, 80)},
		}
	}
}
