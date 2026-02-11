package convref

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// normalizeKey trims surrounding whitespace from storage keys and IDs.
func normalizeKey(s string) string {
	return strings.TrimSpace(s)
}

// normalizeRefForUpsert canonicalizes a Ref for storage and fills missing
// timestamps using nowMS (milliseconds since Unix epoch).
func normalizeRefForUpsert(ref Ref, nowMS int64) (Ref, error) {
	ref.ConversationKey = normalizeKey(ref.ConversationKey)
	ref.Platform = normalizeKey(ref.Platform)
	ref.TenantID = normalizeKey(ref.TenantID)
	ref.RawConversationID = normalizeKey(ref.RawConversationID)
	ref.ServiceURL = normalizeKey(ref.ServiceURL)
	ref.LastActivityID = normalizeKey(ref.LastActivityID)
	ref.BotID = normalizeKey(ref.BotID)

	if ref.ConversationKey == "" {
		return Ref{}, fmt.Errorf("convref: conversation_key required")
	}
	if ref.Platform == "" {
		return Ref{}, fmt.Errorf("convref: platform required")
	}
	if ref.RawConversationID == "" {
		return Ref{}, fmt.Errorf("convref: raw_conversation_id required")
	}

	if ref.CreatedAtMS == 0 {
		ref.CreatedAtMS = nowMS
	}
	// Always update the UpdatedAtMS on upsert unless explicitly provided.
	if ref.UpdatedAtMS == 0 {
		ref.UpdatedAtMS = nowMS
	}
	return ref, nil
}

// migrate creates the conversation_refs table and indexes if they do not exist.
// It is shared by both PostgresStore and SQLiteStore; the DDL is compatible
// with both engines (INTEGER vs BIGINT are aliases in SQLite).
//
// Each statement is executed individually because PostgreSQL drivers using the
// extended protocol (e.g. pgx) reject multiple statements in a single call.
func migrate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("convref: nil db")
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS conversation_refs (
			conversation_key    TEXT PRIMARY KEY,
			platform            TEXT NOT NULL,
			tenant_id           TEXT NOT NULL DEFAULT '',
			raw_conversation_id TEXT NOT NULL,
			service_url         TEXT NOT NULL DEFAULT '',
			last_activity_id    TEXT NOT NULL DEFAULT '',
			bot_id              TEXT NOT NULL DEFAULT '',
			created_at_ms       BIGINT NOT NULL,
			updated_at_ms       BIGINT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_refs_updated ON conversation_refs(updated_at_ms)`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_refs_tenant ON conversation_refs(tenant_id, platform)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("convref: migrate: %w", err)
		}
	}
	return nil
}
