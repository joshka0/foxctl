package companion

import (
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/agent"
)

// MemoryBehavior controls how the companion chat loop uses memory during reply generation.
type MemoryBehavior struct {
	// HistoryTurnLimit is the default number of prior turns injected when the request does not override it.
	HistoryTurnLimit int

	// ImplicitRecallLimit controls how many query-matched memory artifacts are injected into the prompt.
	ImplicitRecallLimit int

	// SemanticRecallLimit controls how many workspace-level semantic memories can be injected alongside
	// conversation-local companion recall.
	SemanticRecallLimit int

	// SessionRecallLimit controls how many related prior sessions can be injected into the prompt.
	SessionRecallLimit int

	// SessionTimelineSummaryLimit controls how many recent chunk-summary lines per recalled session
	// can be injected to ground the agent in prior execution flow.
	SessionTimelineSummaryLimit int

	// SessionTimelineLearningLimit controls how many learned items per recalled session can be injected
	// from session-scoped memory entries (decision, gotcha, preference, anti_pattern, learning).
	SessionTimelineLearningLimit int

	// ImplicitRecallMinScore filters weak memory matches.
	ImplicitRecallMinScore float64

	// SessionRecallMinScore filters weak session recall matches.
	SessionRecallMinScore float64

	// RequireContextQueryWhenMemorySparse enforces a context query when there is no useful history or recall.
	RequireContextQueryWhenMemorySparse bool

	// AutoCompressMinTurns is the minimum total turns required before background hybrid compression is considered.
	AutoCompressMinTurns int

	// AutoCompressEveryTurns triggers background hybrid compression on matching total-turn boundaries.
	AutoCompressEveryTurns int
}

// DefaultMemoryBehavior preserves the historical companion defaults.
func DefaultMemoryBehavior() MemoryBehavior {
	return MemoryBehavior{
		HistoryTurnLimit:                    defaultMaxHistoryTurns,
		ImplicitRecallLimit:                 0,
		SemanticRecallLimit:                 0,
		SessionRecallLimit:                  0,
		SessionTimelineSummaryLimit:         0,
		SessionTimelineLearningLimit:        0,
		ImplicitRecallMinScore:              0.2,
		SessionRecallMinScore:               0.3,
		RequireContextQueryWhenMemorySparse: false,
		AutoCompressMinTurns:                2,
		AutoCompressEveryTurns:              2,
	}
}

// MemoryBehaviorForRetention maps agent retention presets onto reply-time memory behavior.
func MemoryBehaviorForRetention(retention agent.MemoryRetention) MemoryBehavior {
	switch agent.NormalizeMemoryRetention(retention) {
	case agent.MemoryRetentionCompanion:
		return MemoryBehavior{
			HistoryTurnLimit:                    80,
			ImplicitRecallLimit:                 6,
			SemanticRecallLimit:                 4,
			SessionRecallLimit:                  3,
			SessionTimelineSummaryLimit:         3,
			SessionTimelineLearningLimit:        6,
			ImplicitRecallMinScore:              0.15,
			SessionRecallMinScore:               0.25,
			RequireContextQueryWhenMemorySparse: true,
			AutoCompressMinTurns:                2,
			AutoCompressEveryTurns:              2,
		}
	case agent.MemoryRetentionTask:
		return MemoryBehavior{
			HistoryTurnLimit:                    24,
			ImplicitRecallLimit:                 2,
			SemanticRecallLimit:                 1,
			SessionRecallLimit:                  1,
			SessionTimelineSummaryLimit:         1,
			SessionTimelineLearningLimit:        2,
			ImplicitRecallMinScore:              0.35,
			SessionRecallMinScore:               0.35,
			RequireContextQueryWhenMemorySparse: false,
			AutoCompressMinTurns:                4,
			AutoCompressEveryTurns:              6,
		}
	case agent.MemoryRetentionEphemeral:
		return MemoryBehavior{
			HistoryTurnLimit:                    12,
			ImplicitRecallLimit:                 1,
			SemanticRecallLimit:                 0,
			SessionRecallLimit:                  0,
			SessionTimelineSummaryLimit:         0,
			SessionTimelineLearningLimit:        0,
			ImplicitRecallMinScore:              0.45,
			SessionRecallMinScore:               0.4,
			RequireContextQueryWhenMemorySparse: false,
			AutoCompressMinTurns:                6,
			AutoCompressEveryTurns:              10,
		}
	case agent.MemoryRetentionDurable:
		fallthrough
	default:
		return MemoryBehavior{
			HistoryTurnLimit:                    defaultMaxHistoryTurns,
			ImplicitRecallLimit:                 4,
			SemanticRecallLimit:                 2,
			SessionRecallLimit:                  2,
			SessionTimelineSummaryLimit:         2,
			SessionTimelineLearningLimit:        4,
			ImplicitRecallMinScore:              0.25,
			SessionRecallMinScore:               0.3,
			RequireContextQueryWhenMemorySparse: true,
			AutoCompressMinTurns:                2,
			AutoCompressEveryTurns:              4,
		}
	}
}

func normalizeMemoryBehavior(behavior MemoryBehavior) MemoryBehavior {
	defaults := DefaultMemoryBehavior()
	if behavior.HistoryTurnLimit <= 0 {
		behavior.HistoryTurnLimit = defaults.HistoryTurnLimit
	}
	if behavior.ImplicitRecallMinScore <= 0 {
		behavior.ImplicitRecallMinScore = defaults.ImplicitRecallMinScore
	}
	if behavior.SessionRecallMinScore <= 0 {
		behavior.SessionRecallMinScore = defaults.SessionRecallMinScore
	}
	if behavior.SemanticRecallLimit < 0 {
		behavior.SemanticRecallLimit = defaults.SemanticRecallLimit
	}
	if behavior.SessionRecallLimit < 0 {
		behavior.SessionRecallLimit = defaults.SessionRecallLimit
	}
	if behavior.SessionTimelineSummaryLimit < 0 {
		behavior.SessionTimelineSummaryLimit = defaults.SessionTimelineSummaryLimit
	}
	if behavior.SessionTimelineLearningLimit < 0 {
		behavior.SessionTimelineLearningLimit = defaults.SessionTimelineLearningLimit
	}
	if behavior.AutoCompressMinTurns < 0 {
		behavior.AutoCompressMinTurns = defaults.AutoCompressMinTurns
	}
	if behavior.AutoCompressEveryTurns < 0 {
		behavior.AutoCompressEveryTurns = defaults.AutoCompressEveryTurns
	}
	return behavior
}

func shouldTriggerAutoCompress(behavior MemoryBehavior, totalTurns int) bool {
	behavior = normalizeMemoryBehavior(behavior)
	if totalTurns <= 0 {
		return false
	}
	if behavior.AutoCompressEveryTurns == 0 {
		return false
	}
	if totalTurns < behavior.AutoCompressMinTurns {
		return false
	}
	return totalTurns%behavior.AutoCompressEveryTurns == 0
}

func shouldInjectImplicitRecall(query string, behavior MemoryBehavior) bool {
	behavior = normalizeMemoryBehavior(behavior)
	if behavior.ImplicitRecallLimit <= 0 {
		return false
	}
	query = strings.TrimSpace(query)
	return len(query) >= 3
}

func shouldInjectSessionRecall(query string, behavior MemoryBehavior) bool {
	behavior = normalizeMemoryBehavior(behavior)
	if behavior.SessionRecallLimit <= 0 {
		return false
	}
	query = strings.TrimSpace(query)
	return len(query) >= 3
}
