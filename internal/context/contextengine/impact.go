package contextengine

import (
	"fmt"
	"time"
)

// ImpactEdgeKind describes the nature of an impact relationship.
type ImpactEdgeKind string

const (
	ImpactEdgeKindDependsOn     ImpactEdgeKind = "depends_on"
	ImpactEdgeKindCites         ImpactEdgeKind = "cites"
	ImpactEdgeKindGeneratedFrom ImpactEdgeKind = "generated_from"
	ImpactEdgeKindValidates     ImpactEdgeKind = "validates"
	ImpactEdgeKindInvalidates   ImpactEdgeKind = "invalidates"
	ImpactEdgeKindSupersedes    ImpactEdgeKind = "supersedes"
	ImpactEdgeKindRelatesTo     ImpactEdgeKind = "relates_to"
)

// IsValid reports whether k is a known ImpactEdgeKind.
func (k ImpactEdgeKind) IsValid() bool {
	switch k {
	case ImpactEdgeKindDependsOn, ImpactEdgeKindCites, ImpactEdgeKindGeneratedFrom,
		ImpactEdgeKindValidates, ImpactEdgeKindInvalidates, ImpactEdgeKindSupersedes,
		ImpactEdgeKindRelatesTo:
		return true
	default:
		return false
	}
}

// ImpactEdge is a directed relationship between two evidence refs.
type ImpactEdge struct {
	// ID is the unique edge identifier.
	ID string `json:"id"`
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// From is the source ref.
	From EvidenceRef `json:"from"`
	// To is the target ref.
	To EvidenceRef `json:"to"`
	// Kind describes the relationship.
	Kind ImpactEdgeKind `json:"kind"`
	// SourceEventID is the event that created this edge.
	SourceEventID string `json:"source_event_id,omitempty"`
	// CreatedAt is when the edge was created.
	CreatedAt time.Time `json:"created_at"`
}

// Validate checks that the edge has required fields.
func (e ImpactEdge) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("impact edge: missing id")
	}
	if e.WorkspaceID == "" {
		return fmt.Errorf("impact edge: missing workspace_id")
	}
	if err := ValidateEvidenceRef(e.From); err != nil {
		return fmt.Errorf("impact edge: from: %w", err)
	}
	if err := ValidateEvidenceRef(e.To); err != nil {
		return fmt.Errorf("impact edge: to: %w", err)
	}
	if !e.Kind.IsValid() {
		return fmt.Errorf("impact edge: unknown kind %q", e.Kind)
	}
	return nil
}
