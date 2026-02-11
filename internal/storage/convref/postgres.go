package convref

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PostgresStore implements Store on PostgreSQL.
type PostgresStore struct {
	db      *sql.DB
	nowFunc func() time.Time
}

// NewPostgresStore creates a new PostgresStore.
// The db connection should already be open. If nowFunc is nil it defaults to
// time.Now, which allows callers to inject a deterministic clock for testing.
func NewPostgresStore(db *sql.DB, nowFunc func() time.Time) *PostgresStore {
	if nowFunc == nil {
		nowFunc = time.Now
	}
	return &PostgresStore{db: db, nowFunc: nowFunc}
}

// Close is a no-op; the caller owns the *sql.DB lifetime.
func (s *PostgresStore) Close() error { return nil }

// Migrate creates the conversation_refs table and indexes if they do not exist.
func (s *PostgresStore) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("convref: nil db")
	}
	return migrate(ctx, s.db)
}

// CreateTable creates the conversation_refs table and indexes if they do not exist.
// Deprecated: use Migrate instead.
func (s *PostgresStore) CreateTable(ctx context.Context) error {
	return s.Migrate(ctx)
}

// Upsert inserts or updates a conversation reference keyed by ConversationKey.
// If CreatedAtMS or UpdatedAtMS are unset, they are filled with the current time.
func (s *PostgresStore) Upsert(ctx context.Context, ref Ref) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("convref: nil db")
	}

	nowMS := s.nowFunc().UTC().UnixMilli()
	ref, err := normalizeRefForUpsert(ref, nowMS)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO conversation_refs (
	conversation_key, platform, tenant_id, raw_conversation_id,
	service_url, last_activity_id, bot_id, created_at_ms, updated_at_ms
) VALUES (
	$1, $2, $3, $4,
	$5, $6, $7, $8, $9
)
ON CONFLICT (conversation_key) DO UPDATE SET
	platform = EXCLUDED.platform,
	tenant_id = EXCLUDED.tenant_id,
	raw_conversation_id = EXCLUDED.raw_conversation_id,
	service_url = EXCLUDED.service_url,
	last_activity_id = EXCLUDED.last_activity_id,
	bot_id = EXCLUDED.bot_id,
	updated_at_ms = EXCLUDED.updated_at_ms
`, ref.ConversationKey, ref.Platform, ref.TenantID, ref.RawConversationID,
		ref.ServiceURL, ref.LastActivityID, ref.BotID, ref.CreatedAtMS, ref.UpdatedAtMS)
	if err != nil {
		return fmt.Errorf("convref: upsert: %w", err)
	}
	return nil
}

// Get looks up a conversation reference by its conversation key.
// It returns (nil, nil) if the row does not exist.
func (s *PostgresStore) Get(ctx context.Context, conversationKey string) (*Ref, error) {
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
WHERE conversation_key = $1
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
func (s *PostgresStore) Delete(ctx context.Context, conversationKey string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("convref: nil db")
	}
	conversationKey = normalizeKey(conversationKey)
	if conversationKey == "" {
		return fmt.Errorf("convref: conversation_key required")
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM conversation_refs WHERE conversation_key = $1`, conversationKey); err != nil {
		return fmt.Errorf("convref: delete: %w", err)
	}
	return nil
}

// DeleteStale deletes refs whose UpdatedAtMS is older than olderThanMS
// (milliseconds since Unix epoch), returning the number of rows removed.
func (s *PostgresStore) DeleteStale(ctx context.Context, olderThanMS int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("convref: nil db")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM conversation_refs WHERE updated_at_ms < $1`, olderThanMS)
	if err != nil {
		return 0, fmt.Errorf("convref: delete stale: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("convref: delete stale rows affected: %w", err)
	}
	return n, nil
}
