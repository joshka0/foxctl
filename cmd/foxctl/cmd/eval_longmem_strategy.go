package cmd

import (
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/rlm"
)

func normalizeLongmemAnswerStrategy(value string) (longmemAnswerStrategy, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(longmemAnswerStrategyRetrieveMemory), "memory", "retrieve_memory":
		return longmemAnswerStrategyRetrieveMemory, nil
	case string(longmemAnswerStrategyGatherMemory), "gather_memory":
		return longmemAnswerStrategyGatherMemory, nil
	case string(longmemAnswerStrategyGatherMixed), "gather_mixed":
		return longmemAnswerStrategyGatherMixed, nil
	case string(longmemAnswerStrategyFullDebug), "full_debug":
		return longmemAnswerStrategyFullDebug, nil
	default:
		return "", fmt.Errorf("unknown longmem answer strategy %q", value)
	}
}

func (s longmemAnswerStrategy) defaultRouteProfile() rlm.RouteProfile {
	switch s {
	case longmemAnswerStrategyGatherMixed, longmemAnswerStrategyFullDebug:
		return rlm.RouteProfileMixed
	default:
		return rlm.RouteProfileMemoryRecall
	}
}

func (s longmemAnswerStrategy) defaultPlanMode() rlm.PlanMode {
	return rlm.PlanModeFree
}

func (s longmemAnswerStrategy) defaultToolProfile() rlm.ToolProfile {
	switch s {
	case longmemAnswerStrategyGatherMemory:
		return rlm.ToolProfileGatherContext
	case longmemAnswerStrategyGatherMixed:
		return rlm.ToolProfileMemoryContext
	case longmemAnswerStrategyFullDebug:
		return rlm.ToolProfileFullDebug
	default:
		return rlm.ToolProfileMemoryRecall
	}
}

func longmemAnswerPrompt(question string, strategy longmemAnswerStrategy) string {
	question = strings.TrimSpace(question)
	switch strategy {
	case longmemAnswerStrategyGatherMemory:
		return strings.TrimSpace(`Answer the LongMem recall question using the available context tools.
Start with gather_memory_context using the question as the query. Read evidence_digest claims and slots before loading refs. Then call aggregate_evidence_refs on the smallest candidate ref set from evidence_digest.load_refs or path_set, followed by evidence_ledger on that same small ref set. Answer only from evidence_ledger accepted_rows. If evidence_ledger reports needs_fallback=true, use gather_memory_context once more with the best fallback query or narrower required_evidence from the missing answer slot, then rebuild the ledger. Load only refs the ledger leaves ambiguous, using max_tokens around 1200 for verification. Do not mention tool internals.

Question: ` + question)
	case longmemAnswerStrategyGatherMixed:
		return strings.TrimSpace(`Answer the LongMem recall question using the available memory and ContextWiki tools.
Start with plan_context_query using the question, lanes=["memory","context"], and goal="recall". Then call gather_context with the returned gather_context fields. Build evidence_ledger from candidate refs in answer_seed/path_set/evidence_digest before answering. Use retrieve_memory or retrieve_context only as follow-up diagnostics when the planned gather, evidence ledger, and one fallback probe are missing decisive evidence. Do not mention tool internals.

Question: ` + question)
	case longmemAnswerStrategyFullDebug:
		return strings.TrimSpace(`Answer the LongMem recall question using the available retrieval tools.
Start with plan_context_query using the question and goal="recall". Then call gather_context with the returned gather_context fields. Build evidence_ledger from candidate refs before final synthesis. Use retrieve_memory, retrieve_context, retrieve_mixed, or load_evidence_ref only when needed to resolve ambiguity after the planned gather and ledger. Do not use code retrieval unless the question explicitly asks about foxctl implementation. Do not mention tool internals.

Question: ` + question)
	default:
		return strings.TrimSpace(`Answer the LongMem recall question using the available memory-recall tools.
Use retrieve_memory before answering. Do not mention tool internals.

Question: ` + question)
	}
}
