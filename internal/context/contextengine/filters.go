package contextengine

import (
	"encoding/json"
	"time"
)

// Filter types for querying entities. These are defined in the domain package
// so that the impact engine can accept and return typed filters without
// importing the storage package.

// EventFilter constrains a ListEvents query.
type EventFilter struct {
	ID          string
	WorkspaceID string
	Kind        ContextEventKind
	TaskID      string
	SessionID   string
	Limit       int
	Offset      int
}

// ClaimFilter constrains a ListClaims query.
type ClaimFilter struct {
	WorkspaceID string
	Status      ClaimStatus
	TaskID      string
	SessionID   string
	Limit       int
	Offset      int
}

// StalenessFilter constrains a ListStaleness query.
type StalenessFilter struct {
	WorkspaceID string
	TargetRef   *EvidenceRef
	Status      StalenessStatus
	Limit       int
	Offset      int
}

// ImpactFilter constrains a ListImpactEdges query.
type ImpactFilter struct {
	WorkspaceID string
	FromRef     *EvidenceRef
	ToRef       *EvidenceRef
	Kind        ImpactEdgeKind
	Limit       int
	Offset      int
}

// ProjectionFilter constrains a ListProjections query.
type ProjectionFilter struct {
	WorkspaceID    string
	ProjectionType string
	TaskID         string
	Limit          int
	Offset         int
}

// ProjectionRow is a stored projection record.
type ProjectionRow struct {
	ID                  string
	WorkspaceID         string
	ProjectionType      string
	ProjectionVersion   int
	TaskID              string
	GeneratedFromEvents []string
	Payload             json.RawMessage
	GeneratedAt         time.Time
	ExpiresAt           time.Time
	CreatedAt           time.Time
}
