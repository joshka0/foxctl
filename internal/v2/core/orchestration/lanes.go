package orchestration

import "strings"

// LaneOptions configures deterministic lane projection behavior.
type LaneOptions struct {
	TerminalTrackerStates []string
	ReviewTrackerStates   []string
}

// DefaultLaneOptions returns the shared default lane mapping used by runtime and web composition.
func DefaultLaneOptions() LaneOptions {
	return LaneOptions{
		TerminalTrackerStates: []string{"done", "closed", "completed"},
		ReviewTrackerStates:   []string{"human review"},
	}
}

// DeriveLane applies precedence-ordered lane mapping and returns one lane.
func DeriveLane(card Card, opts LaneOptions) Lane {
	terminal := normalizeStateSet(opts.TerminalTrackerStates)
	review := normalizeStateSet(opts.ReviewTrackerStates)
	tracker := normalizeTrackerState(card.TrackerState)

	if tracker != "" && terminal[tracker] {
		return LaneDone
	}
	if tracker != "" && review[tracker] {
		return LaneReview
	}
	if isPolicyBlocked(card.PolicyStatus, card.LastOutcome) {
		return LaneBlocked
	}
	switch card.State {
	case StateRunning:
		return LaneRunning
	case StateRetryQueue:
		return LaneRetryQueue
	case StateClaimed:
		return LaneClaimed
	case StateUnclaimed, StateReleased:
		if card.Eligibility == EligibilityEligible {
			return LaneTodo
		}
		return LaneBlocked
	default:
		if card.Eligibility == EligibilityEligible {
			return LaneTodo
		}
		return LaneBlocked
	}
}

// EnsureLaneCounts returns a counts map containing all canonical lanes.
func EnsureLaneCounts(in map[Lane]int) map[Lane]int {
	out := map[Lane]int{}
	for _, lane := range LaneOrder() {
		out[lane] = 0
	}
	for lane, count := range in {
		out[lane] = count
	}
	return out
}

func normalizeTrackerState(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizeStateSet(states []string) map[string]bool {
	if len(states) == 0 {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(states))
	for _, s := range states {
		trimmed := normalizeTrackerState(s)
		if trimmed == "" {
			continue
		}
		out[trimmed] = true
	}
	return out
}

func isPolicyBlocked(status PolicyStatus, outcome Outcome) bool {
	switch status {
	case PolicyStatusDenied, PolicyStatusBlocked, PolicyStatusValidationError:
		return true
	}

	switch outcome {
	case OutcomeSpawnDenied, OutcomePolicyDenied, OutcomePreflightErr:
		return true
	}
	return false
}
