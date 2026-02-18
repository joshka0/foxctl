package events

import (
	"context"
	"fmt"
	"strings"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	v2events "github.com/jkatigb/agentctl/internal/v2/core/events"
)

// Replay iterates matching events in stable order and applies handler.
func (s *Store) Replay(ctx context.Context, filter v2events.ReplayFilter, handler v2events.ReplayHandler) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 events replay: nil store")
	}
	if handler == nil {
		return fmt.Errorf("v2 events replay: nil handler")
	}

	var (
		where []string
		args  []any
	)
	where = append(where, "1=1")
	if strings.TrimSpace(filter.StreamID) != "" {
		where = append(where, "stream_id = ?")
		args = append(args, filter.StreamID)
	}
	if filter.StreamType != "" {
		where = append(where, "stream_type = ?")
		args = append(args, string(filter.StreamType))
	}
	if filter.FromSequence > 0 {
		where = append(where, "sequence >= ?")
		args = append(args, filter.FromSequence)
	}
	if filter.ToSequence > 0 {
		where = append(where, "sequence <= ?")
		args = append(args, filter.ToSequence)
	}
	if filter.FromVersion > 0 {
		where = append(where, "stream_version >= ?")
		args = append(args, filter.FromVersion)
	}
	if filter.ToVersion > 0 {
		where = append(where, "stream_version <= ?")
		args = append(args, filter.ToVersion)
	}

	limit := filter.Limit
	if limit <= 0 {
		// -1 means "no limit" for SQLite/libSQL, which avoids silent truncation.
		limit = -1
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
		SELECT
			id, stream_id, stream_type, stream_version, sequence, event_type, occurred_at,
			COALESCE(correlation_id, ''), COALESCE(causation_id, ''), COALESCE(actor_id, ''),
			COALESCE(request_id, ''), COALESCE(command, ''), COALESCE(payload, '{}')
		FROM v2_events
		WHERE %s
		ORDER BY stream_id ASC, stream_type ASC, stream_version ASC, sequence ASC
		LIMIT ?
	`, strings.Join(where, " AND "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query replay events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		evt, scanErr := scanEvent(rows)
		if scanErr != nil {
			return scanErr
		}
		if applyErr := handler(ctx, evt); applyErr != nil {
			return &v2errors.V2Error{
				Kind:      v2errors.ErrInternal,
				Message:   "replay handler failed",
				Cause:     applyErr,
				Retryable: true,
				Details: map[string]any{
					"event_id":       evt.ID,
					"event_type":     evt.EventType,
					"stream_id":      evt.StreamID,
					"stream_type":    evt.StreamType,
					"stream_version": evt.StreamVersion,
					"sequence":       evt.Sequence,
				},
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate replay events: %w", err)
	}
	return nil
}
