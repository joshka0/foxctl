package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
	v2events "github.com/jkatigb/agentctl/internal/v2/core/events"
)

// Store persists append-only runtime events to a libsql-backed database.
type Store struct {
	db      *sql.DB
	closeFn func() error
	now     func() time.Time
}

// NewStore constructs an event store over an existing sql.DB.
func NewStore(db *sql.DB, closeFn func() error) *Store {
	return &Store{
		db:      db,
		closeFn: closeFn,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// SetNowForTest overrides time source for deterministic tests.
func (s *Store) SetNowForTest(now func() time.Time) {
	if s == nil || now == nil {
		return
	}
	s.now = now
}

// Open opens a libsql-first v2 event store.
//
// Behavior:
// 1) default to local libsql file (`v2_events.libsql`)
// 2) respect explicit env driver overrides
// 3) fallback to sqlite in environments without cgo/libsql support
func Open(ctx context.Context, storageRoot string) (*Store, error) {
	if strings.TrimSpace(storageRoot) == "" {
		return nil, fmt.Errorf("v2 events open: storageRoot is required")
	}

	defaultCfg := dbdriver.DefaultLibSQLConfig(filepath.Join(storageRoot, "v2_events.libsql"), true)
	cfg := defaultCfg
	if hasDriverOverride() {
		cfg = dbdriver.NewConfigLoader(storageRoot).LoadConfig("V2_EVENTS", "v2_events.db")
	}

	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, cfg, MigrateSchema)
	if err != nil && cfg.Driver == dbdriver.DriverLibSQL {
		fallback := dbdriver.DefaultSQLiteConfig(filepath.Join(storageRoot, "v2_events.db"))
		db, closeFn, err = dbdriver.OpenDBCompatWithCloser(ctx, fallback, MigrateSchema)
	}
	if err != nil {
		return nil, fmt.Errorf("v2 events open: %w", err)
	}
	return NewStore(db, closeFn), nil
}

func hasDriverOverride() bool {
	return os.Getenv("AGENTCTL_V2_EVENTS_DB_DRIVER") != "" || os.Getenv("AGENTCTL_DB_DRIVER") != ""
}

// Close releases database resources.
func (s *Store) Close() error {
	if s == nil || s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}

// Append writes one event with monotonic stream_version and sequence enforcement.
func (s *Store) Append(ctx context.Context, event v2events.Event) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 events append: nil store")
	}
	event = normalizeEvent(event, s.now())

	return sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		var maxVersion int64
		if err := tx.QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(stream_version), 0) FROM v2_events WHERE stream_id = $1 AND stream_type = $2`,
			event.StreamID, string(event.StreamType),
		).Scan(&maxVersion); err != nil {
			return fmt.Errorf("query max version: %w", err)
		}

		expectedVersion := maxVersion + 1
		if event.StreamVersion == 0 {
			event.StreamVersion = expectedVersion
		}
		if event.StreamVersion != expectedVersion {
			return fmt.Errorf(
				"%w: stream=%s/%s expected=%d got=%d",
				v2events.ErrVersionConflict, event.StreamID, event.StreamType, expectedVersion, event.StreamVersion,
			)
		}

		var maxSequence int64
		if err := tx.QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(sequence), 0) FROM v2_events WHERE stream_id = $1 AND stream_type = $2`,
			event.StreamID, string(event.StreamType),
		).Scan(&maxSequence); err != nil {
			return fmt.Errorf("query max sequence: %w", err)
		}
		expectedSequence := maxSequence + 1
		if event.Sequence == 0 {
			event.Sequence = expectedSequence
		}
		if event.Sequence != expectedSequence {
			return fmt.Errorf(
				"%w: sequence stream=%s/%s expected=%d got=%d",
				v2events.ErrVersionConflict, event.StreamID, event.StreamType, expectedSequence, event.Sequence,
			)
		}

		_, err := tx.ExecContext(ctx, `
			INSERT INTO v2_events (
				id, stream_id, stream_type, stream_version, sequence, event_type, occurred_at,
				correlation_id, causation_id, actor_id, request_id, command, payload, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		`,
			event.ID,
			event.StreamID,
			string(event.StreamType),
			event.StreamVersion,
			event.Sequence,
			string(event.EventType),
			sqlutil.FormatTimestamp(event.OccurredAt),
			event.CorrelationID,
			event.CausationID,
			event.ActorID,
			event.RequestID,
			event.Command,
			string(event.Payload),
			sqlutil.FormatTimestamp(s.now()),
		)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return fmt.Errorf("%w: %v", v2events.ErrVersionConflict, err)
			}
			return fmt.Errorf("insert event: %w", err)
		}

		return nil
	})
}

// ListStream returns ordered stream events.
func (s *Store) ListStream(ctx context.Context, filter v2events.StreamFilter) ([]v2events.Event, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("v2 events list: nil store")
	}
	if strings.TrimSpace(filter.StreamID) == "" {
		return nil, fmt.Errorf("v2 events list: stream_id is required")
	}
	if strings.TrimSpace(string(filter.StreamType)) == "" {
		return nil, fmt.Errorf("v2 events list: stream_type is required")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 1000
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, stream_id, stream_type, stream_version, sequence, event_type, occurred_at,
			COALESCE(correlation_id, ''), COALESCE(causation_id, ''), COALESCE(actor_id, ''),
			COALESCE(request_id, ''), COALESCE(command, ''), COALESCE(payload, '{}')
		FROM v2_events
		WHERE stream_id = $1
			AND stream_type = $2
			AND stream_version > $3
		ORDER BY stream_version ASC
		LIMIT $4
	`, filter.StreamID, string(filter.StreamType), filter.AfterVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("query stream events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]v2events.Event, 0, limit)
	for rows.Next() {
		evt, scanErr := scanEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, evt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stream events: %w", err)
	}
	return out, nil
}

// DeleteOrchestrationIssueHistory removes orchestration-related events for the
// provided issue ids within a workspace. When issueIDs is empty, all matching
// orchestration issue events for the workspace are removed.
func (s *Store) DeleteOrchestrationIssueHistory(ctx context.Context, workspaceID string, issueIDs []string) (deleted int, eventIDs []string, err error) {
	if s == nil || s.db == nil {
		return 0, nil, fmt.Errorf("v2 events delete orchestration history: nil store")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return 0, nil, fmt.Errorf("v2 events delete orchestration history: workspace_id is required")
	}
	issueSet := make(map[string]struct{}, len(issueIDs))
	for _, issueID := range issueIDs {
		if trimmed := strings.TrimSpace(issueID); trimmed != "" {
			issueSet[trimmed] = struct{}{}
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, payload
		FROM v2_events
		WHERE command IN ($1, $2)
	`,
		"orchestration/dispatch-issue",
		"orchestration/card-action",
	)
	if err != nil {
		return 0, nil, fmt.Errorf("query orchestration history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	toDelete := make([]string, 0)
	for rows.Next() {
		var (
			eventID string
			payload string
		)
		if scanErr := rows.Scan(&eventID, &payload); scanErr != nil {
			return 0, nil, fmt.Errorf("scan orchestration history row: %w", scanErr)
		}
		var data map[string]any
		if unmarshalErr := json.Unmarshal([]byte(payload), &data); unmarshalErr != nil {
			continue
		}
		if strings.TrimSpace(stringFromAnyMap(data, "workspace_id")) != workspaceID {
			continue
		}
		issueID := strings.TrimSpace(stringFromAnyMap(data, "issue_id"))
		if issueID == "" {
			continue
		}
		if len(issueSet) > 0 {
			if _, ok := issueSet[issueID]; !ok {
				continue
			}
		}
		toDelete = append(toDelete, eventID)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("iterate orchestration history rows: %w", err)
	}
	if len(toDelete) == 0 {
		return 0, nil, nil
	}

	if err := sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		for _, eventID := range toDelete {
			if _, execErr := tx.ExecContext(ctx, `DELETE FROM v2_events WHERE id = $1`, eventID); execErr != nil {
				return fmt.Errorf("delete orchestration event %s: %w", eventID, execErr)
			}
		}
		return nil
	}); err != nil {
		return 0, nil, err
	}
	return len(toDelete), toDelete, nil
}

func normalizeEvent(event v2events.Event, now time.Time) v2events.Event {
	if strings.TrimSpace(event.ID) == "" {
		event.ID = fmt.Sprintf("%s:%s:%d", event.StreamID, event.EventType, now.UnixNano())
	}
	if event.StreamType == "" {
		event.StreamType = v2events.StreamTypeRun
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	} else if !json.Valid(event.Payload) {
		event.Payload = json.RawMessage(`{"decode_error":"invalid_payload"}`)
	}
	return event
}

func stringFromAnyMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	value := m[key]
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func scanEvent(row interface {
	Scan(dest ...any) error
},
) (v2events.Event, error) {
	var (
		evt        v2events.Event
		streamType string
		eventType  string
		occurredAt string
		payloadRaw string
	)
	err := row.Scan(
		&evt.ID,
		&evt.StreamID,
		&streamType,
		&evt.StreamVersion,
		&evt.Sequence,
		&eventType,
		&occurredAt,
		&evt.CorrelationID,
		&evt.CausationID,
		&evt.ActorID,
		&evt.RequestID,
		&evt.Command,
		&payloadRaw,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v2events.Event{}, v2events.ErrNotFound
		}
		return v2events.Event{}, fmt.Errorf("scan event: %w", err)
	}

	evt.StreamType = v2events.StreamType(streamType)
	evt.EventType = v2events.EventType(eventType)
	parsedTime, parseErr := sqlutil.ScanTimestamp(occurredAt)
	if parseErr != nil {
		return v2events.Event{}, fmt.Errorf("parse occurred_at: %w", parseErr)
	}
	evt.OccurredAt = parsedTime
	if payloadRaw == "" {
		payloadRaw = "{}"
	}
	evt.Payload = json.RawMessage(payloadRaw)
	return evt, nil
}

var _ v2events.Repository = (*Store)(nil)
