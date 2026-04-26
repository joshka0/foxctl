package runtime

const CounterexampleRepairKind = "counterexample_repair"

// CounterexampleRepairHarness keeps verifier-driven repair state independent of
// a specific scaffold. Callers add failed candidates and receive compact repair
// feedback suitable for the next helper draft/repair prompt.
type CounterexampleRepairHarness struct {
	BeamWidth          int
	candidateBeam      []map[string]any
	bestCandidate      map[string]any
	lastCounterexample map[string]any
}

func NewCounterexampleRepairHarness(beamWidth int) *CounterexampleRepairHarness {
	return &CounterexampleRepairHarness{BeamWidth: beamWidth}
}

func (h *CounterexampleRepairHarness) RecordVerifierFailure(attempt int, answer string, diagnostic map[string]any) map[string]any {
	if h == nil {
		return helperFactoryVerifierFeedbackMap(diagnostic, nil, nil, 0)
	}
	candidate := map[string]any{
		"attempt":    attempt,
		"answer":     compactHelperFactoryString(answer),
		"diagnostic": cloneMapAny(diagnostic),
	}
	h.candidateBeam = append(h.candidateBeam, candidate)
	h.bestCandidate = bestHelperFactoryVerifierCandidate(h.bestCandidate, candidate)
	h.lastCounterexample = helperFactoryCounterexamplePacket(diagnostic)
	return h.VerifierFeedback(diagnostic)
}

func (h *CounterexampleRepairHarness) RecordSelfReportedFailure(output map[string]any) map[string]any {
	feedback := helperFactoryFinalizeFeedbackMap(output)
	if h != nil && len(feedback) > 0 {
		if counterexample, ok := feedback["counterexample"].(map[string]any); ok {
			h.lastCounterexample = cloneMapAny(counterexample)
		}
	}
	return feedback
}

func (h *CounterexampleRepairHarness) VerifierFeedback(current map[string]any) map[string]any {
	if h == nil {
		return helperFactoryVerifierFeedbackMap(current, nil, nil, 0)
	}
	return helperFactoryVerifierFeedbackMap(current, h.bestCandidate, h.candidateBeam, h.BeamWidth)
}

func (h *CounterexampleRepairHarness) CandidateBeam() []map[string]any {
	if h == nil || len(h.candidateBeam) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(h.candidateBeam))
	for _, candidate := range h.candidateBeam {
		out = append(out, cloneMapAny(candidate))
	}
	return out
}

func (h *CounterexampleRepairHarness) HasCandidates() bool {
	return h != nil && len(h.candidateBeam) > 0
}

func (h *CounterexampleRepairHarness) Telemetry() map[string]any {
	if h == nil {
		return nil
	}
	out := map[string]any{
		"kind": CounterexampleRepairKind,
	}
	if h.BeamWidth > 0 {
		out["beam_width"] = h.BeamWidth
	}
	if len(h.candidateBeam) > 0 {
		out["candidate_count"] = len(h.candidateBeam)
		if h.bestCandidate != nil {
			out["best_candidate"] = compactHelperFactoryVerifierCandidate(h.bestCandidate)
		}
		if top := helperFactoryTopVerifierCandidates(h.candidateBeam, h.BeamWidth); len(top) > 0 {
			out["candidate_frontier"] = compactHelperFactoryCandidateBeam(top)
		}
	}
	if len(h.lastCounterexample) > 0 {
		out["latest_counterexample"] = cloneMapAny(h.lastCounterexample)
	}
	if len(out) == 1 {
		return nil
	}
	return out
}
