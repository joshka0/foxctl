package contextengine

import (
	"context"
	"errors"
	"time"
)

// ErrEmptyQuery is returned when a retrieval lane receives an empty query.
var ErrEmptyQuery = errors.New("contextengine: empty query")

// EmptyQueryError is a typed error for empty query rejection.
type EmptyQueryError struct {
	Lane EvidenceLane
}

func (e EmptyQueryError) Error() string {
	return "contextengine: empty query for lane " + string(e.Lane)
}

func (e EmptyQueryError) Unwrap() error {
	return ErrEmptyQuery
}

// LaneError records a partial failure from a single lane in a mixed retrieval.
type LaneError struct {
	Lane EvidenceLane
	Err  error
}

func (e LaneError) Error() string {
	return "contextengine: lane " + string(e.Lane) + ": " + e.Err.Error()
}

func (e LaneError) Unwrap() error {
	return e.Err
}

// CodeSearchFunc searches code and returns raw results for the code lane to wrap.
// The returned slice contains maps with at minimum "path" and "snippet" keys.
type CodeSearchFunc func(ctx context.Context, query string) ([]CodeSearchHit, error)

// CodeSearchHit represents a single code search result.
type CodeSearchHit struct {
	Path     string
	Snippet  string
	Line     int
	Symbol   string
	Score    float64
	Language string
	Metadata map[string]any
	Sources  []string
}

// MemoryQueryFunc queries memory store and returns claims for the memory lane.
// The query argument lets implementations filter claims by content (e.g.
// substring match on summary/subject); an empty query returns all claims.
type MemoryQueryFunc func(ctx context.Context, workspaceID, query string) ([]MemoryClaim, error)

// ContextQueryFunc retrieves the current TopOfMind/ContextPacket for the context lane.
type ContextQueryFunc func(ctx context.Context, workspaceID string) (*ContextPacket, error)

// ContextPackFunc retrieves additional context-lane EvidencePacks for a query.
// Implementations can adapt ContextWiki retrieval, vault-backed notes, or other
// workspace context sources without changing the basic ContextPacket contract.
type ContextPackFunc func(ctx context.Context, workspaceID, query string, limit int) ([]EvidencePack, error)

// TaskQueryFunc retrieves task contexts for the task lane.
type TaskQueryFunc func(ctx context.Context, workspaceID, taskID string) (*TaskContext, error)

// TaskListFunc lists task IDs for a workspace.
type TaskListFunc func(ctx context.Context, workspaceID string) ([]string, error)

// SessionRecallFunc retrieves compact prior-session evidence for a query.
type SessionRecallFunc func(ctx context.Context, workspaceID, query string, limit int) ([]SessionRecallHit, error)

// SessionRecallHit is a source-agnostic session recall item. Adapters may
// derive it from semantic session recall, chat archives, or transcript stores.
type SessionRecallHit struct {
	SessionID   string
	Summary     string
	Score       float64
	Decisions   []string
	Gotchas     []string
	KeyFiles    []string
	StartedAt   time.Time
	Source      string
	CanVerify   bool
	SpanLocator string
	Metadata    map[string]any
}

// IDGen generates unique identifiers.
type IDGen func() string

// ClockFunc returns the current time.
type ClockFunc func() time.Time

// LaneConfig holds dependencies shared across all lane services.
type LaneConfig struct {
	Store       RetrievalStore
	IDGen       IDGen
	Clock       ClockFunc
	WorkspaceID string
}

// RetrievalStore is the subset of the store interface needed for recording
// retrieval episodes and persisting the EvidencePacks they produce.
type RetrievalStore interface {
	RecordRetrievalEpisode(ctx context.Context, episode RetrievalEpisode) (RetrievalEpisode, error)
	PutEvidencePack(ctx context.Context, pack EvidencePack) (EvidencePack, error)
}

// validateQuery rejects empty queries with a typed error.
func validateQuery(query string, lane EvidenceLane) error {
	if query == "" {
		return EmptyQueryError{Lane: lane}
	}
	return nil
}

// recordEpisode creates and records a RetrievalEpisode.
// Returns the generated episode ID.
func recordEpisode(ctx context.Context, cfg LaneConfig, query string, lane EvidenceLane, packID string, durationMs int64, hitCount int, subEpisodeIDs []string) (string, error) {
	episodeID := cfg.IDGen()
	if cfg.Store == nil {
		return episodeID, nil
	}
	episode := RetrievalEpisode{
		ID:            episodeID,
		WorkspaceID:   cfg.WorkspaceID,
		Query:         query,
		Lane:          lane,
		PackID:        packID,
		DurationMs:    durationMs,
		HitCount:      hitCount,
		SubEpisodeIDs: subEpisodeIDs,
		CreatedAt:     cfg.Clock(),
	}
	_, err := cfg.Store.RecordRetrievalEpisode(ctx, episode)
	return episodeID, err
}

// recordPack persists an EvidencePack via the lane store. Best-effort: errors
// are returned for the caller to swallow, mirroring recordEpisode. Lanes call
// this unconditionally on the success path before returning a pack.
func recordPack(ctx context.Context, cfg LaneConfig, pack EvidencePack) error {
	if cfg.Store == nil {
		return nil
	}
	_, err := cfg.Store.PutEvidencePack(ctx, pack)
	return err
}
