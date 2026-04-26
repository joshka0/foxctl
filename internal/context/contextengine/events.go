package contextengine

import (
	"fmt"
	"time"
)

// ContextEventKind classifies the kind of event that occurred.
type ContextEventKind string

const (
	EventKindCodeChangedDirty    ContextEventKind = "code.changed_dirty"
	EventKindCodeIndexedWorktree ContextEventKind = "code.indexed_worktree"
	EventKindCodeValidated      ContextEventKind = "code.validated"
	EventKindCodeCommitted      ContextEventKind = "code.committed"
	EventKindTaskChanged        ContextEventKind = "task.changed"
	EventKindSessionTurnCaptured ContextEventKind = "session.turn_captured"
	EventKindToolEvidenceProduced ContextEventKind = "tool.evidence_produced"
	EventKindRetrievalExecuted  ContextEventKind = "retrieval.executed"
	EventKindRetrievalMissed    ContextEventKind = "retrieval.missed"
	EventKindAnswerCorrected    ContextEventKind = "answer.corrected"
	EventKindMemoryClaimProposed ContextEventKind = "memory.claim_proposed"
	EventKindMemoryClaimPromoted ContextEventKind = "memory.claim_promoted"
	EventKindMemoryClaimInvalidated ContextEventKind = "memory.claim_invalidated"
	EventKindProjectionGenerated ContextEventKind = "projection.generated"
)

// IsValid reports whether k is a known ContextEventKind.
func (k ContextEventKind) IsValid() bool {
	switch k {
	case EventKindCodeChangedDirty, EventKindCodeIndexedWorktree, EventKindCodeValidated,
		EventKindCodeCommitted, EventKindTaskChanged, EventKindSessionTurnCaptured,
		EventKindToolEvidenceProduced, EventKindRetrievalExecuted, EventKindRetrievalMissed,
		EventKindAnswerCorrected, EventKindMemoryClaimProposed, EventKindMemoryClaimPromoted,
		EventKindMemoryClaimInvalidated, EventKindProjectionGenerated:
		return true
	default:
		return false
	}
}

// ContextEvent is an immutable record of something that happened.
type ContextEvent struct {
	// ID is the unique event identifier.
	ID string `json:"id"`
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// Kind categorizes the event.
	Kind ContextEventKind `json:"kind"`
	// Source identifies the actor or system that emitted the event.
	Source string `json:"source"`
	// TaskID is the associated task, if any.
	TaskID string `json:"task_id,omitempty"`
	// SessionID is the associated session, if any.
	SessionID string `json:"session_id,omitempty"`
	// Refs are evidence refs produced or referenced by this event.
	Refs []EvidenceRef `json:"refs,omitempty"`
	// Data holds unstructured event-specific data.
	Data map[string]any `json:"data,omitempty"`
	// CreatedAt is when the event occurred.
	CreatedAt time.Time `json:"created_at"`
}

// Validate checks that the event has required fields.
func (e ContextEvent) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("context event: missing id")
	}
	if e.WorkspaceID == "" {
		return fmt.Errorf("context event: missing workspace_id")
	}
	if !e.Kind.IsValid() {
		return fmt.Errorf("context event: unknown kind %q", e.Kind)
	}
	if e.Source == "" {
		return fmt.Errorf("context event: missing source")
	}
	return nil
}
