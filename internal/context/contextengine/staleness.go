package contextengine

import (
	"fmt"
	"time"
)

// StalenessStatus is the lifecycle status of a staleness marker.
type StalenessStatus string

const (
	StalenessStatusFresh            StalenessStatus = "fresh"
	StalenessStatusDirty            StalenessStatus = "dirty"
	StalenessStatusNeedsRevalidation StalenessStatus = "needs_revalidation"
	StalenessStatusStale            StalenessStatus = "stale"
	StalenessStatusContradicted      StalenessStatus = "contradicted"
	StalenessStatusSuperseded       StalenessStatus = "superseded"
	StalenessStatusUnknown          StalenessStatus = "unknown"
)

// IsValid reports whether s is a known StalenessStatus.
func (s StalenessStatus) IsValid() bool {
	switch s {
	case StalenessStatusFresh, StalenessStatusDirty, StalenessStatusNeedsRevalidation,
		StalenessStatusStale, StalenessStatusContradicted, StalenessStatusSuperseded,
		StalenessStatusUnknown:
		return true
	default:
		return false
	}
}

// StalenessMarker tracks the invalidation state of evidence.
type StalenessMarker struct {
	// ID is the unique marker identifier.
	ID string `json:"id"`
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// TargetRef is the evidence ref this marker tracks.
	TargetRef EvidenceRef `json:"target_ref"`
	// Status is the current staleness status.
	Status StalenessStatus `json:"status"`
	// CausedByEvents are the events that created this marker.
	CausedByEvents []string `json:"caused_by_events,omitempty"`
	// ResolvedByEvent is the event that resolved this marker.
	ResolvedByEvent string `json:"resolved_by_event,omitempty"`
	// CreatedAt is when the marker was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the marker was last changed.
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks that the marker has required fields.
func (m StalenessMarker) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("staleness marker: missing id")
	}
	if m.WorkspaceID == "" {
		return fmt.Errorf("staleness marker: missing workspace_id")
	}
	if err := ValidateEvidenceRef(m.TargetRef); err != nil {
		return fmt.Errorf("staleness marker: %w", err)
	}
	if !m.Status.IsValid() {
		return fmt.Errorf("staleness marker: unknown status %q", m.Status)
	}
	// Dirty markers must have caused_by_events
	if m.Status == StalenessStatusDirty && len(m.CausedByEvents) == 0 {
		return fmt.Errorf("staleness marker: dirty status requires at least one caused_by_event")
	}
	// Resolution requires resolved_by_event
	if (m.Status == StalenessStatusFresh || m.Status == StalenessStatusSuperseded) && m.ResolvedByEvent == "" {
		return fmt.Errorf("staleness marker: resolution requires resolved_by_event")
	}
	return nil
}

// stalenessTransitionValid records which staleness transitions are allowed.
var stalenessTransitionValid = map[StalenessStatus]map[StalenessStatus]bool{
	StalenessStatusFresh: {
		StalenessStatusDirty:             true,
		StalenessStatusNeedsRevalidation: true,
		StalenessStatusUnknown:           true,
	},
	StalenessStatusDirty: {
		StalenessStatusNeedsRevalidation: true,
	},
	StalenessStatusNeedsRevalidation: {
		StalenessStatusFresh:    true,
		StalenessStatusStale:    true,
		StalenessStatusSuperseded: true,
	},
	StalenessStatusStale: {
		StalenessStatusSuperseded: true,
	},
	StalenessStatusContradicted: {},
	StalenessStatusSuperseded:   {},
	StalenessStatusUnknown: {
		StalenessStatusNeedsRevalidation: true,
	},
}

// StalenessTransitionError describes an invalid staleness transition.
type StalenessTransitionError struct {
	From StalenessStatus `json:"from"`
	To   StalenessStatus `json:"to"`
}

func (e StalenessTransitionError) Error() string {
	return fmt.Sprintf("staleness: invalid transition %s -> %s", e.From, e.To)
}

// CanTransitionStalenessStatus reports whether a staleness marker can move from one status to another.
// Valid transitions:
//   - fresh → dirty, needs_revalidation, unknown
//   - dirty → needs_revalidation
//   - needs_revalidation → fresh, stale, superseded
//   - stale → superseded
//   - contradicted → (none)
//   - superseded → (none)
//   - unknown → needs_revalidation
func CanTransitionStalenessStatus(from, to StalenessStatus) bool {
	if from == to {
		return true
	}
	if !from.IsValid() || !to.IsValid() {
		return false
	}
	allowed, ok := stalenessTransitionValid[from]
	if !ok {
		return false
	}
	return allowed[to]
}

// ValidateStalenessTransition checks that a transition is valid.
func ValidateStalenessTransition(from, to StalenessStatus) error {
	if !from.IsValid() {
		return fmt.Errorf("%w: from=%q", ErrInvalidStalenessStatus, from)
	}
	if !to.IsValid() {
		return fmt.Errorf("%w: to=%q", ErrInvalidStalenessStatus, to)
	}
	if !CanTransitionStalenessStatus(from, to) {
		return StalenessTransitionError{From: from, To: to}
	}
	return nil
}

// ApplyStalenessTransition applies a validated transition and returns the updated marker.
func ApplyStalenessTransition(marker StalenessMarker, to StalenessStatus, resolvedByEvent string, at time.Time) (StalenessMarker, error) {
	if err := ValidateStalenessTransition(marker.Status, to); err != nil {
		return StalenessMarker{}, err
	}
	if marker.Status == to {
		return marker, nil
	}
	marker.Status = to
	marker.ResolvedByEvent = resolvedByEvent
	marker.UpdatedAt = at
	return marker, nil
}
