package adapters

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

// ConvertTask converts a storage/tasks.Task to a contextengine.TaskContext.
func ConvertTask(src tasks.Task) contextengine.TaskContext {
	projectionID := fmt.Sprintf("task_%s", src.ID)
	if src.PlanFile != "" {
		projectionID = src.PlanFile
	}

	var relatedCodeRefs []contextengine.EvidenceRef
	if src.ScopePath != "" {
		relatedCodeRefs = []contextengine.EvidenceRef{{
			Type: contextengine.RefTypePath,
			Ref:  src.ScopePath,
		}}
	}

	return contextengine.TaskContext{
		WorkspaceID: src.WorkspaceID,
		TaskID:      src.ID,
		Objective:   src.Title,
		Status:      src.Status,
		Scope: contextengine.ClaimScope{
			Path:   src.ScopePath,
			TaskID: src.ID,
		},
		OpenGaps:        extractOpenGaps(src),
		NextActions:     extractNextActions(src),
		RelatedCodeRefs: relatedCodeRefs,
		ProjectionMeta: contextengine.ProjectionMeta{
			ProjectionID:      projectionID,
			ProjectionType:    "task_store",
			ProjectionVersion: 1,
			WorkspaceID:       src.WorkspaceID,
		},
		UpdatedAt: src.CreatedAt,
	}
}

// ConvertTaskToContextPacket converts a storage/tasks.Task to a ContextPacket.
func ConvertTaskToContextPacket(src tasks.Task) contextengine.ContextPacket {
	return contextengine.ContextPacket{
		WorkspaceID:     src.WorkspaceID,
		TaskID:          src.ID,
		Objective:       src.Title,
		Phase:           src.Status,
		HardConstraints: nil,
		Blockers:        extractBlockers(src),
		NextActions:     extractNextActions(src),
		Metadata: map[string]any{
			"description":        src.Description,
			"scope_path":         src.ScopePath,
			"assigned_actor_id":  src.AssignedActorID,
			"owner_actor_id":     src.OwnerActorID,
			"blocked_reason":     src.BlockedReason,
			"last_review_status": src.LastReviewStatus,
			"last_review_id":     src.LastReviewID,
			"plan_file":          src.PlanFile,
			"plan_section":       src.PlanSection,
			"session_id":         src.SessionID,
			"epic_id":            src.EpicID,
			"milestone_id":       src.MilestoneID,
		},
	}
}

// ConvertEpic converts a storage/tasks.Epic to a contextengine.EvidenceNode.
func ConvertEpic(workspaceID string, src tasks.Epic) contextengine.EvidenceNode {
	ref := contextengine.EvidenceRef{
		Type: contextengine.RefTypeTask,
		Ref:  src.ID,
	}
	return contextengine.EvidenceNode{
		ID:          src.ID,
		WorkspaceID: workspaceID,
		NodeType:    contextengine.EvidenceNodeTypeTask,
		Ref:         ref,
		Statement:   src.Goal,
		Metadata: map[string]any{
			"title":      src.Title,
			"goal":       src.Goal,
			"status":     src.Status,
			"session_id": src.SessionID,
		},
	}
}

// extractOpenGaps returns open gap items from a task.
func extractOpenGaps(t tasks.Task) []string {
	var gaps []string
	if t.BlockedReason != "" {
		gaps = append(gaps, t.BlockedReason)
	}
	if t.Notes != "" {
		gaps = append(gaps, t.Notes)
	}
	return gaps
}

// extractBlockers returns blockers from a task.
func extractBlockers(t tasks.Task) []string {
	if t.BlockedReason != "" {
		return []string{t.BlockedReason}
	}
	return nil
}

// extractNextActions returns next actions from a task.
func extractNextActions(t tasks.Task) []string {
	var actions []string
	if t.PlanFile != "" && t.PlanSection != "" {
		actions = append(actions, fmt.Sprintf("Continue %s in %s", t.PlanSection, t.PlanFile))
	}
	return actions
}
