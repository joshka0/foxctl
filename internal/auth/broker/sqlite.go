package authbroker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SQLiteStore implements Store on SQLite.
type SQLiteStore struct {
	db    *sql.DB
	clock Clock
}

func NewSQLiteStore(db *sql.DB, clock Clock) *SQLiteStore {
	if clock == nil {
		clock = realClock{}
	}
	return &SQLiteStore{db: db, clock: clock}
}

// Close is a no-op; caller owns *sql.DB lifecycle.
func (s *SQLiteStore) Close() error { return nil }

func (s *SQLiteStore) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("authbroker: nil db")
	}
	return migrate(ctx, s.db)
}

func (s *SQLiteStore) UpsertToken(ctx context.Context, row TokenRow) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("authbroker: nil db")
	}
	nowMS := s.clock.Now().UTC().UnixMilli()
	if row.CreatedAtMS == 0 {
		row.CreatedAtMS = nowMS
	}
	if row.UpdatedAtMS == 0 {
		row.UpdatedAtMS = nowMS
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO oauth_tokens (
	id, tenant_id, subject, provider, scopes_hash, token_json_enc, created_at_ms, updated_at_ms, expires_at_ms
) VALUES (
	?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT(tenant_id, subject, provider, scopes_hash) DO UPDATE SET
	id = excluded.id,
	token_json_enc = excluded.token_json_enc,
	updated_at_ms = excluded.updated_at_ms,
	expires_at_ms = excluded.expires_at_ms
`, row.ID, row.TenantID, row.Subject, string(row.Provider), row.ScopesHash, row.TokenJSONEnc, row.CreatedAtMS, row.UpdatedAtMS, row.ExpiresAtMS)
	if err != nil {
		return fmt.Errorf("authbroker: upsert token: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetToken(ctx context.Context, tenantID, subject string, provider Provider, scopesHash string) (*TokenRow, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("authbroker: nil db")
	}

	var row TokenRow
	err := s.db.QueryRowContext(ctx, `
SELECT id, tenant_id, subject, provider, scopes_hash, token_json_enc, created_at_ms, updated_at_ms, expires_at_ms
FROM oauth_tokens
WHERE tenant_id = ? AND subject = ? AND provider = ? AND scopes_hash = ?
`, tenantID, subject, string(provider), scopesHash).Scan(
		&row.ID, &row.TenantID, &row.Subject, &row.Provider, &row.ScopesHash, &row.TokenJSONEnc, &row.CreatedAtMS, &row.UpdatedAtMS, &row.ExpiresAtMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("authbroker: get token: %w", err)
	}
	return &row, nil
}

func (s *SQLiteStore) DeleteToken(ctx context.Context, tenantID, subject string, provider Provider) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("authbroker: nil db")
	}
	if _, err := s.db.ExecContext(ctx, `
DELETE FROM oauth_tokens
WHERE tenant_id = ? AND subject = ? AND provider = ?
`, tenantID, subject, string(provider)); err != nil {
		return fmt.Errorf("authbroker: delete token: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteExpiredTokens(ctx context.Context, beforeMS int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("authbroker: nil db")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE expires_at_ms <= ?`, beforeMS)
	if err != nil {
		return 0, fmt.Errorf("authbroker: delete expired tokens: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("authbroker: delete expired tokens rows affected: %w", err)
	}
	return n, nil
}

func (s *SQLiteStore) CreateAuthRequest(ctx context.Context, row AuthRequestRow) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("authbroker: nil db")
	}
	if row.CreatedAtMS == 0 {
		row.CreatedAtMS = s.clock.Now().UTC().UnixMilli()
	}
	if err := validateAuthRequestForCreate(row); err != nil {
		return fmt.Errorf("authbroker: create auth request: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO oauth_auth_requests (
	id, tenant_id, subject, provider, scopes, state_nonce, conversation_id, reply_context, created_at_ms, expires_at_ms, completed_at_ms
) VALUES (
	?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
`, row.ID, row.TenantID, row.Subject, string(row.Provider), row.Scopes, row.StateNonce, row.ConversationID, nullableReplyContext(row.ReplyContext), row.CreatedAtMS, row.ExpiresAtMS, row.CompletedAtMS)
	if err != nil {
		return fmt.Errorf("authbroker: create auth request: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetAuthRequest(ctx context.Context, id string) (*AuthRequestRow, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("authbroker: nil db")
	}

	var row AuthRequestRow
	var replyContext sql.NullString
	var completedAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT id, tenant_id, subject, provider, scopes, state_nonce, conversation_id, reply_context, created_at_ms, expires_at_ms, completed_at_ms
FROM oauth_auth_requests
WHERE id = ?
`, id).Scan(
		&row.ID, &row.TenantID, &row.Subject, &row.Provider, &row.Scopes, &row.StateNonce, &row.ConversationID, &replyContext, &row.CreatedAtMS, &row.ExpiresAtMS, &completedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("authbroker: get auth request: %w", err)
	}
	if replyContext.Valid {
		row.ReplyContext = replyContext.String
	}
	if completedAt.Valid {
		row.CompletedAtMS = &completedAt.Int64
	}
	return &row, nil
}

func (s *SQLiteStore) GetAuthRequestByNonce(ctx context.Context, stateNonce string) (*AuthRequestRow, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("authbroker: nil db")
	}

	var row AuthRequestRow
	var replyContext sql.NullString
	var completedAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT id, tenant_id, subject, provider, scopes, state_nonce, conversation_id, reply_context, created_at_ms, expires_at_ms, completed_at_ms
FROM oauth_auth_requests
WHERE state_nonce = ?
ORDER BY created_at_ms DESC
LIMIT 1
`, stateNonce).Scan(
		&row.ID, &row.TenantID, &row.Subject, &row.Provider, &row.Scopes, &row.StateNonce, &row.ConversationID, &replyContext, &row.CreatedAtMS, &row.ExpiresAtMS, &completedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("authbroker: get auth request by nonce: %w", err)
	}
	if replyContext.Valid {
		row.ReplyContext = replyContext.String
	}
	if completedAt.Valid {
		row.CompletedAtMS = &completedAt.Int64
	}
	return &row, nil
}

func (s *SQLiteStore) CompleteAuthRequest(ctx context.Context, id string, completedAtMS int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("authbroker: nil db")
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE oauth_auth_requests
SET completed_at_ms = ?
WHERE id = ? AND completed_at_ms IS NULL AND expires_at_ms > ?
`, completedAtMS, id, completedAtMS)
	if err != nil {
		return fmt.Errorf("authbroker: complete auth request: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("authbroker: complete auth request rows affected: %w", err)
	}
	if n == 0 {
		var existingCompleted sql.NullInt64
		var expiresAtMS int64
		if err := s.db.QueryRowContext(ctx, `
SELECT completed_at_ms, expires_at_ms
FROM oauth_auth_requests
WHERE id = ?
`, id).Scan(&existingCompleted, &expiresAtMS); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("authbroker: auth request %q not found", id)
			}
			return fmt.Errorf("authbroker: inspect auth request completion: %w", err)
		}
		if existingCompleted.Valid {
			return fmt.Errorf("authbroker: auth request %q already completed", id)
		}
		if completedAtMS >= expiresAtMS {
			return fmt.Errorf("authbroker: auth request %q expired", id)
		}
		return fmt.Errorf("authbroker: auth request %q is not pending", id)
	}
	return nil
}

func (s *SQLiteStore) DeleteExpiredAuthRequests(ctx context.Context, beforeMS int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("authbroker: nil db")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM oauth_auth_requests WHERE expires_at_ms <= ?`, beforeMS)
	if err != nil {
		return 0, fmt.Errorf("authbroker: delete expired auth requests: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("authbroker: delete expired auth requests rows affected: %w", err)
	}
	return n, nil
}

func nullableReplyContext(replyContext string) any {
	if replyContext == "" {
		return nil
	}
	return replyContext
}
