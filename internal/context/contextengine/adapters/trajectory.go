package adapters

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

// ConvertTrajectoryEvent converts a trajectory.Event to a contextengine.ContextEvent.
func ConvertTrajectoryEvent(workspaceID string, src trajectory.Event) contextengine.ContextEvent {
	kind := mapTrajectoryEventKind(src.Kind)
	data := make(map[string]any)
	if src.DataInline != nil {
		data = src.DataInline
	}
	if src.DataArtifact != "" {
		data["data_artifact"] = src.DataArtifact
	}
	if src.Meta != nil {
		data["trace_id"] = src.Meta.TraceID
		data["job_id"] = src.Meta.JobID
		data["task_id"] = src.Meta.TaskID
		data["epic_id"] = src.Meta.EpicID
		data["review_id"] = src.Meta.ReviewID
		data["actor_id"] = src.Meta.ActorID
		data["task_run_id"] = src.Meta.TaskRunID
		data["trace_parent"] = src.Meta.TraceParent
		data["job_attempt"] = src.Meta.JobAttempt
		data["created_by"] = src.Meta.CreatedBy
		data["cas_digest"] = src.Meta.CASDigest
	}

	return contextengine.ContextEvent{
		ID:          src.ID,
		WorkspaceID: workspaceID,
		Kind:        kind,
		Source:      src.Actor,
		TaskID:      safeMetaField(src.Meta, func(m *trajectory.EventMeta) string { return m.TaskID }),
		SessionID:   src.TrajectoryID,
		Refs:        nil,
		Data:        data,
		CreatedAt:   src.TS,
	}
}

// mapTrajectoryEventKind maps trajectory EventKind to ContextEventKind.
// Uses explicit switch, not keyword heuristics.
func mapTrajectoryEventKind(kind trajectory.EventKind) contextengine.ContextEventKind {
	switch kind {
	case trajectory.EventKindToolCall:
		return contextengine.EventKindToolEvidenceProduced
	case trajectory.EventKindToolResult:
		return contextengine.EventKindToolEvidenceProduced
	case trajectory.EventKindUserRequest:
		return contextengine.EventKindSessionTurnCaptured
	case trajectory.EventKindAgentThought:
		return contextengine.EventKindSessionTurnCaptured
	case trajectory.EventKindReviewRequest:
		return contextengine.EventKindSessionTurnCaptured
	case trajectory.EventKindReviewResult:
		return contextengine.EventKindCodeValidated
	case trajectory.EventKindTaskTransition:
		return contextengine.EventKindTaskChanged
	case trajectory.EventKindGraphSearch:
		return contextengine.EventKindRetrievalExecuted
	case trajectory.EventKindSWEGrep:
		return contextengine.EventKindRetrievalExecuted
	case trajectory.EventKindHookCall:
		return contextengine.EventKindToolEvidenceProduced
	case trajectory.EventKindHookResult:
		return contextengine.EventKindToolEvidenceProduced
	default:
		return contextengine.EventKindSessionTurnCaptured
	}
}

// safeMetaField extracts a field from EventMeta if present.
func safeMetaField(meta *trajectory.EventMeta, fn func(*trajectory.EventMeta) string) string {
	if meta == nil {
		return ""
	}
	return fn(meta)
}

// ConvertTrajectory converts a trajectory.Trajectory to a contextengine.ContextPacket.
func ConvertTrajectory(src trajectory.Trajectory) contextengine.ContextPacket {
	taskRefs := make([]contextengine.EvidenceRef, 0, len(src.TaskIDs))
	for _, tid := range src.TaskIDs {
		taskRefs = append(taskRefs, contextengine.EvidenceRef{
			Type: contextengine.RefTypeTask,
			Ref:  tid,
		})
	}
	if src.EpicID != "" {
		taskRefs = append(taskRefs, contextengine.EvidenceRef{
			Type: contextengine.RefTypeTask,
			Ref:  src.EpicID,
		})
	}

	return contextengine.ContextPacket{
		WorkspaceID:  src.WorkspaceID,
		TaskID:       safeFirst(src.TaskIDs),
		Objective:    src.Summary,
		Phase:        string(src.Status),
		RelevantRefs: taskRefs,
		Metadata: map[string]any{
			"id":               src.ID,
			"root_request_id":  src.RootRequestID,
			"epic_id":          src.EpicID,
			"agent_role":       src.AgentRole,
			"job_id":           src.JobID,
			"trace_id":         src.TraceID,
			"status":           string(src.Status),
			"session_id":       src.SessionID,
			"artifact_digest":  src.ArtifactDigest,
			"created_at":       src.CreatedAt,
			"updated_at":       src.UpdatedAt,
		},
	}
}

// safeFirst returns the first element or empty string.
func safeFirst(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}

// ConvertTrajectoryToContextEvent converts a full Trajectory into a ContextEvent
// summarizing the trajectory outcome.
func ConvertTrajectoryToContextEvent(src trajectory.Trajectory) contextengine.ContextEvent {
	data := map[string]any{
		"trajectory_id": src.ID,
		"agent_role":    src.AgentRole,
		"job_id":        src.JobID,
		"trace_id":      src.TraceID,
		"artifact_digest": src.ArtifactDigest,
	}
	if src.Outcome != nil {
		data["success"] = src.Outcome.Success
		data["tasks_completed"] = src.Outcome.TasksCompleted
		data["tool_call_count"] = src.Outcome.ToolCallCount
		data["error_count"] = src.Outcome.ErrorCount
		data["duration_ms"] = src.Outcome.DurationMS
	}

	return contextengine.ContextEvent{
		ID:          fmt.Sprintf("trajectory_%s", src.ID),
		WorkspaceID: src.WorkspaceID,
		Kind:        contextengine.EventKindSessionTurnCaptured,
		Source:      fmt.Sprintf("trajectory:%s", src.AgentRole),
		TaskID:      safeFirst(src.TaskIDs),
		SessionID:   src.SessionID,
		Data:        data,
		CreatedAt:   src.CreatedAt,
	}
}
