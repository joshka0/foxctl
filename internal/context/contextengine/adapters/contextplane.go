package adapters

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
)

// ConvertTopOfMind converts a contextplane.TopOfMind to a contextengine.ContextPacket.
func ConvertTopOfMind(src contextplane.TopOfMind) contextengine.ContextPacket {
	refs := ParseOrInferRefs(src.RelevantRefs)
	decisions := make([]contextengine.RecentDecision, 0, len(src.RecentDecisions))
	for _, d := range src.RecentDecisions {
		decisions = append(decisions, contextengine.RecentDecision{
			ID:   d.ID,
			Text: d.Text,
			Ref:  d.Ref,
		})
	}
	return contextengine.ContextPacket{
		WorkspaceID:     src.WorkspaceID,
		Objective:       src.Objective,
		Phase:           src.Phase,
		HardConstraints: src.HardConstraints,
		Blockers:        src.Blockers,
		RecentDecisions: decisions,
		OpenLoops:       src.OpenLoops,
		NextActions:     src.NextActions,
		RelevantRefs:    refs,
		Metadata: map[string]any{
			"active_task_ids": src.ActiveTaskIDs,
			"updated_at":      src.UpdatedAt,
		},
	}
}

// ConvertHandoff converts a contextplane.Handoff to a contextengine.ContextPacket.
func ConvertHandoff(src contextplane.Handoff) contextengine.ContextPacket {
	refs := ParseOrInferRefs(src.EvidenceRefs)
	fileRefs := ParseOrInferRefs(src.FilesTouched)
	allRefs := make([]contextengine.EvidenceRef, 0, len(refs)+len(fileRefs))
	allRefs = append(allRefs, refs...)
	allRefs = append(allRefs, fileRefs...)

	return contextengine.ContextPacket{
		WorkspaceID: src.TaskID,
		TaskID:      src.TaskID,
		Objective:   src.Summary,
		Phase:       src.Phase,
		NextActions: src.NextActions,
		RelevantRefs: allRefs,
		Metadata: map[string]any{
			"outcome":             src.Outcome,
			"observations":        src.Observations,
			"tensions":            src.Tensions,
			"promotion_candidates": src.PromotionCandidates,
			"created_at":          src.CreatedAt,
		},
	}
}

// ConvertObservation converts a contextplane.Observation to a contextengine.EvidenceNode.
func ConvertObservation(workspaceID string, src contextplane.Observation) contextengine.EvidenceNode {
	refs := ParseOrInferRefs(src.EvidenceRefs)
	ref := contextengine.EvidenceRef{Type: contextengine.RefTypeNote, Ref: src.ID}
	if len(refs) > 0 {
		ref = refs[0]
	}
	return contextengine.EvidenceNode{
		ID:          src.ID,
		WorkspaceID: workspaceID,
		NodeType:    contextengine.EvidenceNodeTypeObservation,
		Ref:         ref,
		Statement:   src.Statement,
		Confidence:  src.Confidence,
		Count:       src.Count,
		FirstSeen:   src.FirstSeen,
		LastSeen:    src.LastSeen,
		Metadata: map[string]any{
			"project":       src.Project,
			"area":          src.Area,
			"evidence_refs": FormatStringRefs(refs),
		},
	}
}

// ConvertTension converts a contextplane.Tension to a contextengine.EvidenceNode.
func ConvertTension(workspaceID string, src contextplane.Tension) contextengine.EvidenceNode {
	refs := ParseOrInferRefs(src.RelatedRefs)
	ref := contextengine.EvidenceRef{Type: contextengine.RefTypeNote, Ref: src.ID}
	if len(refs) > 0 {
		ref = refs[0]
	}
	return contextengine.EvidenceNode{
		ID:          src.ID,
		WorkspaceID: workspaceID,
		NodeType:    contextengine.EvidenceNodeTypeTension,
		Ref:         ref,
		Statement:   src.Statement,
		Count:       src.Count,
		FirstSeen:   src.CreatedAt,
		LastSeen:    src.LastSeen,
		Metadata: map[string]any{
			"kind":         src.Kind,
			"impact":       src.Impact,
			"status":       src.Status,
			"related_refs": FormatStringRefs(refs),
		},
	}
}

// ConvertMemoryProposal converts a contextplane.MemoryProposal to a contextengine.MemoryClaim.
func ConvertMemoryProposal(workspaceID string, src contextplane.MemoryProposal) contextengine.MemoryClaim {
	refs := ParseOrInferRefs(src.SourceRefs)
	return contextengine.MemoryClaim{
		ID:          src.ID,
		WorkspaceID: workspaceID,
		ClaimType:   src.Kind,
		Status:      mapProposalStatus(src.Status),
		Scope: contextengine.ClaimScope{
			Path: src.BlastRadius,
		},
		Summary:     src.Summary,
		Confidence:  src.Confidence,
		BlastRadius: src.BlastRadius,
		SourceRefs:  refs,
		Reason:      src.EvaluationStatus,
		CreatedAt:   src.CreatedAt,
		UpdatedAt:   src.UpdatedAt,
	}
}

// mapProposalStatus maps MemoryProposal status to ClaimStatus.
// Uses explicit mapping — no keyword heuristics.
func mapProposalStatus(status string) contextengine.ClaimStatus {
	switch status {
	case "active", "pending", "open":
		return contextengine.ClaimStatusCandidate
	case "current", "accepted", "approved":
		return contextengine.ClaimStatusCurrent
	case "rejected", "closed":
		return contextengine.ClaimStatusRejected
	case "superseded":
		return contextengine.ClaimStatusSuperseded
	default:
		return contextengine.ClaimStatusCandidate
	}
}

// ConvertTaskPacket converts a contextplane.TaskPacket to a contextengine.TaskContext.
func ConvertTaskPacket(src contextplane.TaskPacket) contextengine.TaskContext {
	refs := ParseOrInferRefs(src.RelevantRefs)

	projectionID := ""
	projectionType := "task_packet"
	if src.LatestHandoff != nil {
		projectionID = src.LatestHandoff.Path
	}
	if projectionID == "" {
		projectionID = fmt.Sprintf("task_packet_%s_%s", src.WorkspaceID, src.Task.ID)
	}

	// Extract decision texts for next-actions context.
	nextActions := make([]string, 0, len(src.NextActions)+len(src.RecentDecisions))
	nextActions = append(nextActions, src.NextActions...)
	for _, d := range src.RecentDecisions {
		if d.Text != "" {
			nextActions = append(nextActions, fmt.Sprintf("Decision: %s", d.Text))
		}
	}

	return contextengine.TaskContext{
		WorkspaceID: src.WorkspaceID,
		TaskID:      src.Task.ID,
		Objective:   src.Objective,
		Status:      src.Task.Status,
		Scope: contextengine.ClaimScope{
			Path:   src.Task.ScopePath,
			TaskID: src.Task.ID,
		},
		NextActions:     nextActions,
		RelatedCodeRefs: refs,
		OpenGaps:        src.Blockers,
		StaleWarnings:   nil,
		ProjectionMeta: contextengine.ProjectionMeta{
			ProjectionID:      projectionID,
			ProjectionType:    projectionType,
			ProjectionVersion: 1,
			WorkspaceID:       src.WorkspaceID,
			GeneratedAt:       src.GeneratedAt,
		},
		UpdatedAt: src.GeneratedAt,
	}
}

// ConvertRetrievalResult converts a contextplane.RetrievalResult to a contextengine.EvidencePack.
func ConvertRetrievalResult(src contextplane.RetrievalResult) contextengine.EvidencePack {
	nodes := make([]contextengine.EvidenceNode, 0)
	for _, obs := range src.Observations {
		nodes = append(nodes, ConvertObservation(src.WorkspaceID, obs))
	}
	for _, t := range src.Tensions {
		nodes = append(nodes, ConvertTension(src.WorkspaceID, t))
	}
	for _, hit := range src.VaultHits {
		nodes = append(nodes, ConvertRetrievalHit(src.WorkspaceID, hit))
	}

	return contextengine.EvidencePack{
		ID:          fmt.Sprintf("retrieval_%s_%d", src.WorkspaceID, src.GeneratedAt.Unix()),
		WorkspaceID: src.WorkspaceID,
		Query:       src.Query,
		Lane:        contextengine.LaneContext,
		Nodes:       nodes,
		Telemetry: contextengine.EvidenceTelemetry{
			TokensUsed: src.Weights.BaseIndexScore,
		},
		Metadata: map[string]any{
			"semantic_model": src.SemanticModel,
			"semantic_used":  src.SemanticUsed,
			"generated_at":   src.GeneratedAt,
		},
	}
}

// ConvertRetrievalHit converts a contextplane.RetrievalHit to a contextengine.EvidenceNode.
func ConvertRetrievalHit(workspaceID string, src contextplane.RetrievalHit) contextengine.EvidenceNode {
	ref := contextengine.EvidenceRef{
		Type: contextengine.RefTypePath,
		Ref:  src.Path,
	}
	if src.PrimaryAnchorPath != "" {
		ref = contextengine.EvidenceRef{
			Type: contextengine.RefTypePath,
			Ref:  src.PrimaryAnchorPath,
		}
	}
	return contextengine.EvidenceNode{
		ID:          fmt.Sprintf("hit_%s_%s", workspaceID, src.Path),
		WorkspaceID: workspaceID,
		NodeType:    contextengine.EvidenceNodeTypeRetrieval,
		Ref:         ref,
		Statement:   src.Snippet,
		Confidence:  float64(src.Score) / 100.0,
		Grounding:   contextengine.GroundingIndexed,
		Metadata: map[string]any{
			"title":               src.Title,
			"type":                src.Type,
			"trust":               src.Trust,
			"score":               src.Score,
			"primary_anchor_path": src.PrimaryAnchorPath,
			"repo_paths":          src.RepoPaths,
			"anchor_paths":        src.AnchorPaths,
			"anchor_roles":        src.AnchorRoles,
			"symbols":             src.Symbols,
		},
	}
}
