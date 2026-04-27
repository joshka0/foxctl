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
}

// MemoryQueryFunc queries memory store and returns claims for the memory lane.
// The query argument lets implementations filter claims by content (e.g.
// substring match on summary/subject); an empty query returns all claims.
type MemoryQueryFunc func(ctx context.Context, workspaceID, query string) ([]MemoryClaim, error)

// ContextQueryFunc retrieves the current TopOfMind/ContextPacket for the context lane.
type ContextQueryFunc func(ctx context.Context, workspaceID string) (*ContextPacket, error)

// TaskQueryFunc retrieves task contexts for the task lane.
type TaskQueryFunc func(ctx context.Context, workspaceID, taskID string) (*TaskContext, error)

// TaskListFunc lists task IDs for a workspace.
type TaskListFunc func(ctx context.Context, workspaceID string) ([]string, error)

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
func recordEpisode(ctx context.Context, cfg LaneConfig, query string, lane EvidenceLane, packID string, durationMs int64, hitCount int, subEpisodeIDs []string) error {
	episode := RetrievalEpisode{
		ID:            cfg.IDGen(),
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
	return err
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
