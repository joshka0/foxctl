package run

import (
	"context"
	"errors"
	"time"
)

// ErrEpisodeNotFound indicates a referenced episode record does not exist.
var ErrEpisodeNotFound = errors.New("v2 run: episode not found")

// EpisodeRecord is one derived semantic chapter spanning a turn range.
type EpisodeRecord struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"session_id,omitempty"`
	EpisodeVersion string    `json:"episode_version,omitempty"`
	BoundaryKey    string    `json:"boundary_key,omitempty"`
	StartTurnID    string    `json:"start_turn_id,omitempty"`
	EndTurnID      string    `json:"end_turn_id,omitempty"`
	StartTurnIndex int       `json:"start_turn_index,omitempty"`
	EndTurnIndex   int       `json:"end_turn_index,omitempty"`
	Topic          string    `json:"topic,omitempty"`
	Summary        string    `json:"summary,omitempty"`
	SalienceScore  float64   `json:"salience_score,omitempty"`
	IsLandmark     bool      `json:"is_landmark,omitempty"`
	AnchorRefs     []string  `json:"anchor_refs,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

// Clone returns a deep copy safe for cross-goroutine reads.
func (e EpisodeRecord) Clone() EpisodeRecord {
	out := e
	if len(e.AnchorRefs) > 0 {
		out.AnchorRefs = append([]string(nil), e.AnchorRefs...)
	}
	return out
}

// EpisodeWriter persists derived episode records.
type EpisodeWriter interface {
	SaveEpisode(ctx context.Context, episode EpisodeRecord) error
}

// EpisodeReader loads persisted episode records.
type EpisodeReader interface {
	GetEpisode(ctx context.Context, episodeID string) (EpisodeRecord, error)
	ListEpisodes(ctx context.Context, sessionID string, opts EpisodeListOptions) ([]EpisodeRecord, error)
}

// EpisodeListOptions controls episode timeline retrieval for one session.
type EpisodeListOptions struct {
	Limit        int
	Since        time.Time
	Until        time.Time
	Asc          bool
	LandmarkOnly bool
}
