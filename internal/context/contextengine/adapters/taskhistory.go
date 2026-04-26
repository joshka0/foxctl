package adapters

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/context/contextplane/taskhistory"
)

// ConvertPack converts a taskhistory.Pack to a contextengine.ContextPacket.
func ConvertPack(src taskhistory.Pack) contextengine.ContextPacket {
	task := src.Task
	tp := src.TaskPacket
	refs := ParseOrInferRefs(src.FilesTouched)
	extRefs := ParseOrInferRefs(src.ExternalRefs)
	allRefs := make([]contextengine.EvidenceRef, 0, len(refs)+len(extRefs))
	allRefs = append(allRefs, refs...)
	allRefs = append(allRefs, extRefs...)

	decisions := make([]contextengine.RecentDecision, 0, len(tp.RecentDecisions))
	for _, d := range tp.RecentDecisions {
		decisions = append(decisions, contextengine.RecentDecision{
			ID:   d.ID,
			Text: d.Text,
			Ref:  d.Ref,
		})
	}

	metadata := map[string]any{
		"workspace_path": src.WorkspacePath,
		"generated_at":   src.GeneratedAt,
		"summary":        src.Summary,
	}
	if src.Transcript != nil {
		metadata["transcript_overview"] = src.Transcript.Overview
		metadata["transcript_objective"] = src.Transcript.ObjectiveLabel
	}

	return contextengine.ContextPacket{
		WorkspaceID:     src.WorkspaceID,
		TaskID:          task.ID,
		Objective:       tp.Objective,
		Phase:           tp.Phase,
		HardConstraints: tp.HardConstraints,
		Blockers:        tp.Blockers,
		RecentDecisions: decisions,
		NextActions:     tp.NextActions,
		RelevantRefs:    allRefs,
		Metadata:        metadata,
	}
}

// ConvertSessionSummary converts a taskhistory.SessionSummary to a contextengine.EvidenceNode.
func ConvertSessionSummary(workspaceID string, src taskhistory.SessionSummary) contextengine.EvidenceNode {
	ref := contextengine.EvidenceRef{
		Type: contextengine.RefTypeSession,
		Ref:  src.ID,
	}
	return contextengine.EvidenceNode{
		ID:          src.ID,
		WorkspaceID: workspaceID,
		NodeType:    contextengine.EvidenceNodeTypeContext,
		Ref:         ref,
		Statement:   src.Summary,
		Metadata: map[string]any{
			"reason":              src.Reason,
			"project_name":        src.ProjectName,
			"accomplished":        src.Accomplished,
			"decisions":           src.Decisions,
			"gotchas":             src.Gotchas,
			"key_files":           src.KeyFiles,
			"timeline_summaries":  src.TimelineSummaries,
			"timeline_tools":      src.TimelineTools,
			"timeline_files":      src.TimelineFiles,
			"recent_files_touched": src.RecentFilesTouched,
			"started_at":          src.StartedAt,
			"ended_at":            src.EndedAt,
		},
	}
}

// ExtractContextplaneHandoff extracts contextplane handoff records from a Pack.
func ExtractContextplaneHandoff(handoff contextplane.HandoffRecord) contextengine.ContextPacket {
	return ConvertHandoff(handoff.Handoff)
}

// ExtractSessionEvidence extracts session summaries as evidence nodes.
func ExtractSessionEvidence(workspaceID string, sessions []taskhistory.SessionSummary) []contextengine.EvidenceNode {
	nodes := make([]contextengine.EvidenceNode, 0, len(sessions))
	for _, s := range sessions {
		nodes = append(nodes, ConvertSessionSummary(workspaceID, s))
	}
	return nodes
}

// fmtRef generates a deterministic ID from workspace and index.
func fmtRef(workspaceID string, idx int) string {
	return fmt.Sprintf("node_%s_%d", workspaceID, idx)
}
