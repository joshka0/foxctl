package generalsolver

import (
	"fmt"
	"strings"
)

// BraidToWorkItems converts a braid graph into generalsolver WorkItems and
// populates the SolverState. Each BraidNode becomes a WorkItem with an
// archetype inferred from its kind and helper policy.
//
// This is a one-way conversion for populating state. The caller retains
// ownership of the BraidGraph for rendering and telemetry.
func BraidToWorkItems(state *SolverState, nodes []BraidNodeLike, finalNodeID string) error {
	if state == nil {
		return fmt.Errorf("generalsolver: state is nil")
	}
	for _, node := range nodes {
		archetype := braidKindToArchetype(node.Kind)
		item := WorkItem{
			ID:              node.ID,
			Goal:            node.Question,
			Archetype:       archetype,
			DependsOn:       append([]string(nil), node.DependsOn...),
			Status:          StatusPending,
			Priority:        braidKindPriority(node.Kind),
			Risk:            braidKindRisk(node.Kind, finalNodeID),
			MaxSummaryChars: node.MaxSummaryChars,
			Payload: map[string]any{
				"braid_kind":        node.Kind,
				"braid_helper_policy": node.HelperPolicy,
			},
		}
		if item.ID == finalNodeID {
			item.Priority += 0.1
		}
		if err := AddWorkItem(state, item); err != nil {
			return fmt.Errorf("generalsolver: braid bridge add node %q: %w", node.ID, err)
		}
	}
	// Mark root nodes (no deps) as ready.
	for _, node := range nodes {
		if len(node.DependsOn) == 0 {
			item := state.Items[node.ID]
			item.Status = StatusReady
			state.Items[node.ID] = item
		}
	}
	return nil
}

// BraidSummaryToArtifact converts a braid node execution summary into a
// generalsolver WorkArtifact.
func BraidSummaryToArtifact(nodeID string, summary string) WorkArtifact {
	status := braidSummaryToArtifactStatus(summary)
	answer := extractBraidAnswerFromSummary(summary)

	artifact := WorkArtifact{
		WorkItemID: nodeID,
		Status:     status,
		Confidence: braidSummaryConfidence(summary),
	}
	if answer != "" {
		artifact.Answer = answer
	}
	if status == ArtifactStatusFailed || status == ArtifactStatusBlocked {
		artifact.Evidence = map[string]any{
			"raw_summary": truncateString(summary, 500),
		}
	}
	return artifact
}

// ArtifactToBraidSummary converts a generalsolver WorkArtifact back into a
// braid-compatible execution summary string.
func ArtifactToBraidSummary(artifact WorkArtifact) string {
	if artifact.Answer == nil {
		if artifact.Status == ArtifactStatusBlocked {
			return "status: blocked summary: no answer produced checks: " + truncateString(evidenceString(artifact.Evidence), 200)
		}
		return "status: blocked summary: no answer"
	}
	answer, ok := artifact.Answer.(string)
	if !ok {
		answer = fmt.Sprintf("%v", artifact.Answer)
	}
	status := "solved"
	if artifact.Status == ArtifactStatusPartial {
		status = "partial"
	}
	return fmt.Sprintf("status: completed summary: status: %s answer: %s confidence: %.2f", status, answer, artifact.Confidence)
}

// StateFromBraidEvents reconstructs a SolverState from braid execution events.
// This is used for post-hoc analysis where we have event telemetry but not the
// live state.
//
// NOTE: This is currently a partial reconstruction. It seeds work items from
// the node count (using synthetic IDs) and records success/failure from
// events, but does not reconstruct full artifacts or dependency edges. Callers
// should not rely on artifacts or reverse deps being populated from this path.
func StateFromBraidEvents(events []BraidEventLike, nodeCount int, finalNodeID string) *SolverState {
	state := NewSolverState()
	executedNodes := map[string]string{} // nodeID -> summary
	failedNodes := map[string]string{}   // nodeID -> failure message

	for _, evt := range events {
		switch evt.Status {
		case "accepted":
			// Graph accepted event — already seeded.
		case "ready", "runtime_shortcut", "helper_first":
			// Progress events, not terminal.
		case "completed", "helper_first_completed", "helper_recovered":
			if evt.NodeID != "" {
				executedNodes[evt.NodeID] = evt.Message
			}
		case "repairing":
			// Repair cycle, not terminal.
		case "rejected", "helper_first_failed", "helper_first_rejected":
			if evt.NodeID != "" {
				failedNodes[evt.NodeID] = "status: blocked " + truncateString(evt.Message, 300)
			}
		}
	}

	// Seed work items for any node referenced in events.
	seen := map[string]bool{}
	for id := range executedNodes {
		seen[id] = true
	}
	for id := range failedNodes {
		seen[id] = true
	}
	for id := range seen {
		summary, ok := executedNodes[id]
		if ok {
			artifact := BraidSummaryToArtifact(id, summary)
			_ = AddWorkItem(state, WorkItem{
				ID:        id,
				Goal:      "reconstructed from events",
				Archetype: ArchetypeMixed,
				DependsOn: []string{},
				Status:    StatusSolved,
				Priority:  1.0,
				Risk:      0.5,
			})
			_ = CommitArtifact(state, id, artifact)
		} else {
			msg := failedNodes[id]
			_ = AddWorkItem(state, WorkItem{
				ID:        id,
				Goal:      "reconstructed from events",
				Archetype: ArchetypeMixed,
				DependsOn: []string{},
				Status:    StatusFailed,
				Priority:  1.0,
				Risk:      0.5,
			})
			_ = RecordFailure(state, id, msg, map[string]any{"reconstructed": true})
		}
	}

	_ = nodeCount
	_ = finalNodeID
	return state
}

// BraidNodeLike is the read-only interface that the braid bridge needs from
// a braid graph node. The caller satisfies this from BraidNode.
type BraidNodeLike struct {
	ID              string
	Kind            string
	Question        string
	DependsOn       []string
	MaxSummaryChars int
	HelperPolicy    string
	Archetype       string
	ScaffoldClass   string
	ScaffoldID      string
	InputSchema     map[string]any
}

// BraidEventLike is the read-only interface for braid events.
type BraidEventLike struct {
	NodeID  string
	Status  string
	Message string
}

// ExtractBraidNodeLikes converts BraidNode slices into BraidNodeLike slices
// for the bridge. This avoids importing the runtime package directly.
func ExtractBraidNodeLikes(ids []string, kinds []string, questions []string, deps [][]string, maxSummaryChars []int, helperPolicies []string, archetypes []string, scaffoldClasses []string, scaffoldIDs []string) []BraidNodeLike {
	n := len(ids)
	out := make([]BraidNodeLike, n)
	for i := 0; i < n; i++ {
		out[i] = BraidNodeLike{
			ID:              ids[i],
			Kind:            kinds[i],
			Question:        questions[i],
			MaxSummaryChars: intAt(maxSummaryChars, i),
			HelperPolicy:    stringAt(helperPolicies, i),
			Archetype:       stringAt(archetypes, i),
			ScaffoldClass:   stringAt(scaffoldClasses, i),
			ScaffoldID:      stringAt(scaffoldIDs, i),
		}
		if i < len(deps) {
			out[i].DependsOn = deps[i]
		}
	}
	return out
}

func intAt(s []int, i int) int {
	if i < len(s) {
		return s[i]
	}
	return 0
}

func stringAt(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

func braidKindToArchetype(kind string) ProblemArchetype {
	switch strings.TrimSpace(kind) {
	case "extract":
		return ArchetypeExplicitDAG
	case "solve":
		return ArchetypeExplicitDAG
	case "cycle_solve":
		return ArchetypeConstraintSolve
	case "verify":
		return ArchetypeCandidateVerify
	case "reduce":
		return ArchetypeExplicitDAG
	default:
		return ArchetypeMixed
	}
}

func braidKindPriority(kind string) float64 {
	switch strings.TrimSpace(kind) {
	case "extract":
		return 3.0
	case "solve":
		return 2.0
	case "cycle_solve":
		return 2.0
	case "verify":
		return 1.5
	case "reduce":
		return 0.5
	default:
		return 1.0
	}
}

func braidKindRisk(kind string, finalNodeID string) float64 {
	switch strings.TrimSpace(kind) {
	case "verify":
		return 0.8
	case "reduce":
		return 1.0
	case "cycle_solve":
		return 0.7
	default:
		return 0.5
	}
}

func braidSummaryToArtifactStatus(summary string) string {
	lower := strings.ToLower(summary)
	if strings.Contains(lower, "status: solved") || strings.Contains(lower, "status: completed") {
		return ArtifactStatusSolved
	}
	if strings.Contains(lower, "status: pass") || strings.Contains(lower, "pass: true") {
		return ArtifactStatusSolved
	}
	if strings.Contains(lower, "status: partial") {
		return ArtifactStatusPartial
	}
	if strings.Contains(lower, "status: blocked") {
		return ArtifactStatusBlocked
	}
	if strings.Contains(lower, "status: failed") || strings.Contains(lower, "status: rejected") {
		return ArtifactStatusFailed
	}
	return ArtifactStatusBlocked
}

func extractBraidAnswerFromSummary(summary string) string {
	lower := strings.ToLower(summary)
	idx := strings.Index(lower, "answer:")
	if idx < 0 {
		return ""
	}
	answer := strings.TrimSpace(summary[idx+len("answer:"):])
	if end := strings.Index(answer, "checks:"); end >= 0 {
		answer = strings.TrimSpace(answer[:end])
	}
	if end := strings.Index(answer, "\n"); end >= 0 {
		answer = strings.TrimSpace(answer[:end])
	}
	return answer
}

func braidSummaryConfidence(summary string) float64 {
	if strings.Contains(strings.ToLower(summary), "verified") {
		return 0.95
	}
	if strings.Contains(strings.ToLower(summary), "pass: true") {
		return 0.9
	}
	if strings.Contains(strings.ToLower(summary), "status: solved") {
		return 0.8
	}
	if strings.Contains(strings.ToLower(summary), "status: completed") {
		return 0.7
	}
	return 0.0
}

func evidenceString(e map[string]any) string {
	if e == nil {
		return ""
	}
	s, ok := e["raw_summary"].(string)
	if ok {
		return s
	}
	return ""
}
