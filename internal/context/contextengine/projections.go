package contextengine

import (
	"fmt"
	"time"
)

// ProjectionMeta holds versioning information for a projection.
type ProjectionMeta struct {
	// ProjectionID is the unique projection identifier.
	ProjectionID string `json:"projection_id"`
	// ProjectionType is the kind of projection (top_of_mind, task_context, etc.).
	ProjectionType string `json:"projection_type"`
	// ProjectionVersion is the monotonic version number.
	ProjectionVersion int `json:"projection_version"`
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// GeneratedFromEvents are the event IDs that contributed to this projection.
	GeneratedFromEvents []string `json:"generated_from_events,omitempty"`
	// GeneratedAt is when the projection was created.
	GeneratedAt time.Time `json:"generated_at"`
	// ExpiresAt is when the projection expires (optional).
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// Validate checks that the projection meta has required fields.
func (m ProjectionMeta) Validate() error {
	if m.ProjectionID == "" {
		return fmt.Errorf("projection meta: missing projection_id")
	}
	if m.ProjectionType == "" {
		return fmt.Errorf("projection meta: missing projection_type")
	}
	if m.ProjectionVersion < 1 {
		return fmt.Errorf("projection meta: projection_version must be >= 1")
	}
	if m.WorkspaceID == "" {
		return fmt.Errorf("projection meta: missing workspace_id")
	}
	return nil
}

// WorkingSet tracks the current working state for a workspace.
type WorkingSet struct {
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// DirtyRefs are evidence refs modified since the last commit.
	DirtyRefs []EvidenceRef `json:"dirty_refs,omitempty"`
	// RecentCommands are the last N commands executed.
	RecentCommands []string `json:"recent_commands,omitempty"`
	// RecentFailures are recent validation failures.
	RecentFailures []string `json:"recent_failures,omitempty"`
	// RecentSuccesses are recent validation successes.
	RecentSuccesses []string `json:"recent_successes,omitempty"`
	// UpdatedAt is when the working set was last changed.
	UpdatedAt time.Time `json:"updated_at"`
}

// AddDirtyRef adds a dirty ref, deduplicating by ref value.
func (ws *WorkingSet) AddDirtyRef(ref EvidenceRef) {
	for _, existing := range ws.DirtyRefs {
		if existing.Equal(ref) {
			return
		}
	}
	ws.DirtyRefs = append(ws.DirtyRefs, ref)
}

// AddRecentCommand adds a command, bounded to maxCommands.
func (ws *WorkingSet) AddRecentCommand(cmd string, maxCommands int) {
	for _, existing := range ws.RecentCommands {
		if existing == cmd {
			return
		}
	}
	ws.RecentCommands = append(ws.RecentCommands, cmd)
	if len(ws.RecentCommands) > maxCommands {
		ws.RecentCommands = ws.RecentCommands[len(ws.RecentCommands)-maxCommands:]
	}
}

// Validate checks that the working set has required fields.
func (ws WorkingSet) Validate() error {
	if ws.WorkspaceID == "" {
		return fmt.Errorf("working set: missing workspace_id")
	}
	for i, ref := range ws.DirtyRefs {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("working set: dirty_ref[%d]: %w", i, err)
		}
	}
	return nil
}

// TaskContext is a task-scoped projection of context.
type TaskContext struct {
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// TaskID is the associated task.
	TaskID string `json:"task_id"`
	// Objective is the task objective.
	Objective string `json:"objective,omitempty"`
	// Status is the current task status.
	Status string `json:"status,omitempty"`
	// Scope defines the task scope.
	Scope ClaimScope `json:"scope,omitempty"`
	// OpenGaps are unresolved issues.
	OpenGaps []string `json:"open_gaps,omitempty"`
	// StaleWarnings are staleness warnings for this task.
	StaleWarnings []string `json:"stale_warnings,omitempty"`
	// NextActions are suggested next actions.
	NextActions []string `json:"next_actions,omitempty"`
	// RelatedCodeRefs are code evidence refs related to this task.
	RelatedCodeRefs []EvidenceRef `json:"related_code_refs,omitempty"`
	// RelatedClaims are claim evidence refs related to this task.
	RelatedClaims []EvidenceRef `json:"related_claims,omitempty"`
	// RelatedSessions are session evidence refs related to this task.
	RelatedSessions []EvidenceRef `json:"related_sessions,omitempty"`
	// RelatedArtifacts are artifact evidence refs related to this task.
	RelatedArtifacts []EvidenceRef `json:"related_artifacts,omitempty"`
	// ValidationEvidence are refs used to validate this task.
	ValidationEvidence []EvidenceRef `json:"validation_evidence,omitempty"`
	// ProjectionMeta holds projection versioning information.
	ProjectionMeta ProjectionMeta `json:"projection_meta"`
	// UpdatedAt is when the task context was last changed.
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks that the task context has required fields.
func (tc TaskContext) Validate() error {
	if tc.WorkspaceID == "" {
		return fmt.Errorf("task context: missing workspace_id")
	}
	if tc.TaskID == "" {
		return fmt.Errorf("task context: missing task_id")
	}
	if err := tc.ProjectionMeta.Validate(); err != nil {
		return fmt.Errorf("task context: %w", err)
	}
	for i, ref := range tc.RelatedCodeRefs {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("task context: related_code_ref[%d]: %w", i, err)
		}
	}
	for i, ref := range tc.RelatedClaims {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("task context: related_claim[%d]: %w", i, err)
		}
	}
	for i, ref := range tc.RelatedSessions {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("task context: related_session[%d]: %w", i, err)
		}
	}
	for i, ref := range tc.RelatedArtifacts {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("task context: related_artifact[%d]: %w", i, err)
		}
	}
	for i, ref := range tc.ValidationEvidence {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("task context: validation_evidence[%d]: %w", i, err)
		}
	}
	return nil
}

// ContextPacket is a generic container for context passed between components.
type ContextPacket struct {
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// TaskID is the associated task, if any.
	TaskID string `json:"task_id,omitempty"`
	// SessionID is the associated session, if any.
	SessionID string `json:"session_id,omitempty"`
	// Objective is the active objective.
	Objective string `json:"objective,omitempty"`
	// Phase is the current phase.
	Phase string `json:"phase,omitempty"`
	// HardConstraints are must-have constraints.
	HardConstraints []string `json:"hard_constraints,omitempty"`
	// Blockers are active blockers.
	Blockers []string `json:"blockers,omitempty"`
	// RecentDecisions are recent decisions made.
	RecentDecisions []RecentDecision `json:"recent_decisions,omitempty"`
	// OpenLoops are open items to address.
	OpenLoops []string `json:"open_loops,omitempty"`
	// NextActions are suggested next actions.
	NextActions []string `json:"next_actions,omitempty"`
	// RelevantRefs are evidence refs relevant to this context.
	RelevantRefs []EvidenceRef `json:"relevant_refs,omitempty"`
	// Metadata holds unstructured context metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// RecentDecision captures a bounded decision item.
type RecentDecision struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Ref  string `json:"ref,omitempty"`
}

// Validate checks that the context packet has required fields.
func (cp ContextPacket) Validate() error {
	if cp.WorkspaceID == "" {
		return fmt.Errorf("context packet: missing workspace_id")
	}
	for i, ref := range cp.RelevantRefs {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("context packet: relevant_ref[%d]: %w", i, err)
		}
	}
	return nil
}
