package convref

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SQLiteStore implements Store on SQLite.
type SQLiteStore struct {
	db      *sql.DB
	nowFunc func() time.Time
}

// NewSQLiteStore creates a new SQLiteStore.
// The db connection should already be open. If nowFunc is nil it defaults to
// time.Now, which allows callers to inject a deterministic clock for testing.
func NewSQLiteStore(db *sql.DB, nowFunc func() time.Time) *SQLiteStore {
	if nowFunc == nil {
		nowFunc = time.Now
	}
	return &SQLiteStore{db: db, nowFunc: nowFunc}
}

// Close is a no-op; the caller owns the *sql.DB lifetime.
func (s *SQLiteStore) Close() error { return nil }

// Migrate creates the conversation_refs table and indexes if they do not exist.
func (s *SQLiteStore) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("convref: nil db")
	}
	return migrate(ctx, s.db)
}

// CreateTable creates the conversation_refs table and indexes if they do not exist.
// Deprecated: use Migrate instead.
func (s *SQLiteStore) CreateTable(ctx context.Context) error {
	return s.Migrate(ctx)
}

// Upsert inserts or updates a conversation reference keyed by ConversationKey.
// If CreatedAtMS or UpdatedAtMS are unset, they are filled with the current time.
func (s *SQLiteStore) Upsert(ctx context.Context, ref Ref) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("convref: nil db")
	}

	nowMS := s.nowFunc().UTC().UnixMilli()
	ref, err := normalizeRefForUpsert(ref, nowMS)
	if err != nil {
		return err
	}

	// SQLite supports ON CONFLICT DO UPDATE with the modernc driver.
	_, err = s.db.ExecContext(ctx, `
INSERT INTO conversation_refs (
	conversation_key, platform, tenant_id, raw_conversation_id,
	service_url, last_activity_id, bot_id, created_at_ms, updated_at_ms
) VALUES (
	?, ?, ?, ?,
	?, ?, ?, ?, ?
)
ON CONFLICT(conversation_key) DO UPDATE SET
	platform = excluded.platform,
	tenant_id = excluded.tenant_id,
	raw_conversation_id = excluded.raw_conversation_id,
	service_url = excluded.service_url,
	last_activity_id = excluded.last_activity_id,
	bot_id = excluded.bot_id,
	updated_at_ms = excluded.updated_at_ms
`, ref.ConversationKey, ref.Platform, ref.TenantID, ref.RawConversationID,
		ref.ServiceURL, ref.LastActivityID, ref.BotID, ref.CreatedAtMS, ref.UpdatedAtMS)
	if err != nil {
		return fmt.Errorf("convref: upsert: %w", err)
	}
	return nil
}

// Get looks up a conversation reference by its conversation key.
// It returns (nil, nil) if the row does not exist.
func (s *SQLiteStore) Get(ctx context.Context, conversationKey string) (*Ref, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("convref: nil db")
	}

	conversationKey = normalizeKey(conversationKey)
	if conversationKey == "" {
		return nil, fmt.Errorf("convref: conversation_key required")
	}

	var ref Ref
	err := s.db.QueryRowContext(ctx, `
SELECT conversation_key, platform, tenant_id, raw_conversation_id, service_url, last_activity_id, bot_id, created_at_ms, updated_at_ms
FROM conversation_refs
WHERE conversation_key = ?
`, conversationKey).Scan(
		&ref.ConversationKey, &ref.Platform, &ref.TenantID, &ref.RawConversationID,
		&ref.ServiceURL, &ref.LastActivityID, &ref.BotID, &ref.CreatedAtMS, &ref.UpdatedAtMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("convref: get: %w", err)
	}

	return &ref, nil
}

// Delete removes a conversation reference by its conversation key.
func (s *SQLiteStore) Delete(ctx context.Context, conversationKey string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("convref: nil db")
	}
	conversationKey = normalizeKey(conversationKey)
	if conversationKey == "" {
		return fmt.Errorf("convref: conversation_key required")
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM conversation_refs WHERE conversation_key = ?`, conversationKey); err != nil {
		return fmt.Errorf("convref: delete: %w", err)
	}
	return nil
}

// DeleteStale deletes refs whose UpdatedAtMS is older than olderThanMS
// (milliseconds since Unix epoch), returning the number of rows removed.
func (s *SQLiteStore) DeleteStale(ctx context.Context, olderThanMS int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("convref: nil db")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM conversation_refs WHERE updated_at_ms < ?`, olderThanMS)
	if err != nil {
		return 0, fmt.Errorf("convref: delete stale: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("convref: delete stale rows affected: %w", err)
	}
	return n, nil
}
