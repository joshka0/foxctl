package authbroker

import (
	"context"
	"database/sql"
	"fmt"
)

func migrate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("authbroker: nil db")
	}

	// Execute each DDL statement individually: pgx extended protocol rejects
	// multi-statement SQL in a single Exec call.
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS oauth_tokens (
			id              TEXT PRIMARY KEY,
			tenant_id       TEXT NOT NULL,
			subject         TEXT NOT NULL,
			provider        TEXT NOT NULL,
			scopes_hash     TEXT NOT NULL,
			token_json_enc  BLOB NOT NULL,
			created_at_ms   BIGINT NOT NULL,
			updated_at_ms   BIGINT NOT NULL,
			expires_at_ms   BIGINT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_oauth_tokens_key ON oauth_tokens(tenant_id, subject, provider, scopes_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_oauth_tokens_expiry ON oauth_tokens(expires_at_ms)`,
		`CREATE TABLE IF NOT EXISTS oauth_auth_requests (
			id               TEXT PRIMARY KEY,
			tenant_id        TEXT NOT NULL,
			subject          TEXT NOT NULL,
			provider         TEXT NOT NULL,
			scopes           TEXT NOT NULL,
			state_nonce      TEXT NOT NULL,
			conversation_id  TEXT NOT NULL,
			reply_context    TEXT,
			created_at_ms    BIGINT NOT NULL,
			expires_at_ms    BIGINT NOT NULL,
			completed_at_ms  BIGINT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_oauth_auth_requests_nonce ON oauth_auth_requests(state_nonce)`,
		`CREATE INDEX IF NOT EXISTS idx_oauth_auth_requests_expiry ON oauth_auth_requests(expires_at_ms)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("authbroker: migrate: %w", err)
		}
	}
	return nil
}
