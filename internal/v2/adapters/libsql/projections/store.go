package projections

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
)

// ErrNotFound indicates projection rows are absent.
var ErrNotFound = errors.New("v2 projections: not found")

type runStatus string

const (
	runStatusRunning   runStatus = "running"
	runStatusCompleted runStatus = "completed"
	runStatusFailed    runStatus = "failed"
)

type agentStatus string

const (
	agentStatusActive agentStatus = "active"
	agentStatusIdle   agentStatus = "idle"
	agentStatusError  agentStatus = "error"
)

// RunState is the materialized projection for a run stream.
type RunState struct {
	RunID             string
	Status            string
	LastEventID       string
	LastStreamVersion int64
	Command           string
	RequestID         string
	ActorID           string
	UpdatedAt         time.Time
}

// AgentState is the materialized projection for an agent.
type AgentState struct {
	AgentID           string
	State             string
	LastEventID       string
	LastStreamVersion int64
	UpdatedAt         time.Time
}

// LegacyResolver resolves v1 IDs to v2 IDs.
type LegacyResolver interface {
	ResolveV2ID(ctx context.Context, entityType, legacyID string) (string, error)
}

// Store applies events into projection tables.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB) *Store {
	return &Store{
		db: db,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// Apply materializes one event into projection tables.
func (s *Store) Apply(ctx context.Context, evt v2events.Event) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 projections apply: nil store")
	}

	runState := mapRunStatus(evt)
	if runState != "" {
		if err := s.upsertRunState(ctx, evt, string(runState)); err != nil {
			return err
		}
	}

	agentID := extractAgentID(evt)
	if agentID != "" {
		agentState := mapAgentStatus(evt)
		if agentState != "" {
			if err := s.upsertAgentState(ctx, agentID, evt, string(agentState)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Store) upsertRunState(ctx context.Context, evt v2events.Event, status string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO v2_run_state (
			run_id, status, last_event_id, last_stream_version, command, request_id, actor_id, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(run_id) DO UPDATE SET
			status = excluded.status,
			last_event_id = excluded.last_event_id,
			last_stream_version = excluded.last_stream_version,
			command = excluded.command,
			request_id = excluded.request_id,
			actor_id = excluded.actor_id,
			updated_at = excluded.updated_at
	`,
		evt.StreamID,
		status,
		evt.ID,
		evt.StreamVersion,
		evt.Command,
		evt.RequestID,
		evt.ActorID,
		sqlutil.FormatTimestamp(s.now()),
	)
	if err != nil {
		return fmt.Errorf("upsert run_state: %w", err)
	}
	return nil
}

func (s *Store) upsertAgentState(ctx context.Context, agentID string, evt v2events.Event, state string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO v2_agent_state (
			agent_id, state, last_event_id, last_stream_version, updated_at
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(agent_id) DO UPDATE SET
			state = excluded.state,
			last_event_id = excluded.last_event_id,
			last_stream_version = excluded.last_stream_version,
			updated_at = excluded.updated_at
	`,
		agentID,
		state,
		evt.ID,
		evt.StreamVersion,
		sqlutil.FormatTimestamp(s.now()),
	)
	if err != nil {
		return fmt.Errorf("upsert agent_state: %w", err)
	}
	return nil
}

func mapRunStatus(evt v2events.Event) runStatus {
	if evt.StreamType != v2events.StreamTypeRun {
		return ""
	}
	switch evt.EventType {
	case v2events.EventRunStarted:
		return runStatusRunning
	case v2events.EventRunCompleted:
		return runStatusCompleted
	case v2events.EventRunFailed:
		return runStatusFailed
	default:
		return ""
	}
}

func mapAgentStatus(evt v2events.Event) agentStatus {
	switch evt.EventType {
	case v2events.EventRunStarted:
		return agentStatusActive
	case v2events.EventRunCompleted:
		return agentStatusIdle
	case v2events.EventRunFailed:
		return agentStatusError
	default:
		return ""
	}
}

func extractAgentID(evt v2events.Event) string {
	if evt.StreamType == v2events.StreamTypeAgent && strings.TrimSpace(evt.StreamID) != "" {
		return evt.StreamID
	}
	var payload map[string]any
	if len(evt.Payload) == 0 {
		return ""
	}
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return ""
	}
	raw, ok := payload["agent_id"]
	if !ok {
		return ""
	}
	id, _ := raw.(string)
	return strings.TrimSpace(id)
}

// GetRunState fetches a run projection by run id.
func (s *Store) GetRunState(ctx context.Context, runID string) (RunState, error) {
	if s == nil || s.db == nil {
		return RunState{}, fmt.Errorf("v2 projections get run: nil store")
	}
	var (
		out       RunState
		updatedAt string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT run_id, status, last_event_id, last_stream_version, COALESCE(command, ''),
		       COALESCE(request_id, ''), COALESCE(actor_id, ''), updated_at
		FROM v2_run_state
		WHERE run_id = $1
	`, runID).Scan(
		&out.RunID,
		&out.Status,
		&out.LastEventID,
		&out.LastStreamVersion,
		&out.Command,
		&out.RequestID,
		&out.ActorID,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RunState{}, ErrNotFound
	}
	if err != nil {
		return RunState{}, fmt.Errorf("query run_state: %w", err)
	}
	parsed, parseErr := sqlutil.ScanTimestamp(updatedAt)
	if parseErr != nil {
		return RunState{}, fmt.Errorf("parse run_state.updated_at: %w", parseErr)
	}
	out.UpdatedAt = parsed
	return out, nil
}

// GetRunStateByRef resolves legacy IDs via resolver and fetches run state.
func (s *Store) GetRunStateByRef(ctx context.Context, ref string, resolver LegacyResolver) (RunState, error) {
	runID := strings.TrimSpace(ref)
	if runID == "" {
		return RunState{}, ErrNotFound
	}
	if state, err := s.GetRunState(ctx, runID); err == nil {
		return state, nil
	} else if !errors.Is(err, ErrNotFound) {
		return RunState{}, err
	}
	if resolver == nil {
		return RunState{}, ErrNotFound
	}
	resolved, err := resolver.ResolveV2ID(ctx, "run", runID)
	if err != nil {
		return RunState{}, err
	}
	return s.GetRunState(ctx, resolved)
}
