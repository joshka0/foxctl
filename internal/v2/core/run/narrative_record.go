package run

import (
	"context"
	"errors"
	"time"
)

// ErrNarrativeNotFound indicates a session-scoped narrative artifact is missing.
var ErrNarrativeNotFound = errors.New("v2 run: narrative not found")

// NarrativeClaim is one evidence-cited narrative statement.
type NarrativeClaim struct {
	Text       string   `json:"text"`
	AnchorRefs []string `json:"anchor_refs,omitempty"`
}

// Clone returns a deep copy safe for cross-goroutine reads.
func (c NarrativeClaim) Clone() NarrativeClaim {
	out := c
	if len(c.AnchorRefs) > 0 {
		out.AnchorRefs = append([]string(nil), c.AnchorRefs...)
	}
	return out
}

// NarrativeRecord is a session-scoped, derived coherence artifact.
type NarrativeRecord struct {
	SessionID       string           `json:"session_id"`
	TurnID          string           `json:"turn_id,omitempty"`
	SourceTurnID    string           `json:"source_turn_id,omitempty"`
	SourceTurnIndex int              `json:"source_turn_index,omitempty"`
	SourceTurnCount int              `json:"source_turn_count,omitempty"`
	Ref             string           `json:"ref,omitempty"`
	ArtifactVersion string           `json:"artifact_version,omitempty"`
	Summary         string           `json:"summary,omitempty"`
	Claims          []NarrativeClaim `json:"claims,omitempty"`
	AnchorRefs      []string         `json:"anchor_refs,omitempty"`
	UpdatedAt       time.Time        `json:"updated_at,omitempty"`
}

// Clone returns a deep copy safe for cross-goroutine reads.
func (r NarrativeRecord) Clone() NarrativeRecord {
	out := r
	if len(r.Claims) > 0 {
		out.Claims = make([]NarrativeClaim, len(r.Claims))
		for i := range r.Claims {
			out.Claims[i] = r.Claims[i].Clone()
		}
	}
	if len(r.AnchorRefs) > 0 {
		out.AnchorRefs = append([]string(nil), r.AnchorRefs...)
	}
	return out
}

// NarrativeWriter persists session-scoped narrative records.
type NarrativeWriter interface {
	SaveNarrative(ctx context.Context, narrative NarrativeRecord) error
}

// NarrativeReader loads session-scoped narrative records.
type NarrativeReader interface {
	GetNarrative(ctx context.Context, sessionID, artifactVersion string) (NarrativeRecord, error)
}
