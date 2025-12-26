// Package mailbox implements SQLite-backed persistence for inter-agent messaging.
package mailbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
)

// Store defines the persistence interface for mailbox messages.
type Store interface {
	Close() error
	Send(ctx context.Context, msg agent.Message) error
	Poll(ctx context.Context, agentNS string, timeout time.Duration, maxMessages int) ([]agent.Message, error)
	Ack(ctx context.Context, messageID string) error
	Nack(ctx context.Context, messageID string, visibilityTimeout time.Duration) error
	List(ctx context.Context, agentNS string, limit int) ([]agent.Message, error)
	Delete(ctx context.Context, messageID string) error
	// ListBySession returns messages for a specific session lineage.
	ListBySession(ctx context.Context, sessionID string, limit int) ([]agent.Message, error)
	// ListByWorkspace returns messages for a specific workspace.
	ListByWorkspace(ctx context.Context, workspace string, limit int) ([]agent.Message, error)
}

type sqlStore struct {
	db *sql.DB
}

// Open initializes the mailbox store rooted at the provided path.
func Open(ctx context.Context, root string) (Store, error) {
	dbPath := filepath.Join(root, "mailbox.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("mailbox: open db: %w", err)
	}
	return &sqlStore{db: db}, nil
}

func (s *sqlStore) Close() error {
	return s.db.Close()
}

func (s *sqlStore) Send(ctx context.Context, msg agent.Message) error {
	headersJSON, err := json.Marshal(msg.Headers)
	if err != nil {
		return fmt.Errorf("mailbox: marshal headers: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO mailbox (id, from_ns, to_ns, type, ttl_ms, headers, payload, visible_at, attempt, ts, session_id, workspace, agent_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.FromNS, msg.ToNS, msg.Type, msg.TTLMS, string(headersJSON), string(msg.Payload), msg.VisibleAt, msg.Attempt, msg.Timestamp,
		msg.SessionID, msg.Workspace, msg.AgentID)
	if err != nil {
		return fmt.Errorf("mailbox: send: %w", err)
	}
	return nil
}

func (s *sqlStore) Poll(ctx context.Context, agentNS string, timeout time.Duration, maxMessages int) ([]agent.Message, error) {
	if maxMessages <= 0 {
		maxMessages = 10
	}

	deadline := time.Now().Add(timeout)
	for {
		// Check context
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Try to fetch messages
		now := time.Now().Unix()
		rows, err := s.db.QueryContext(ctx, `
			SELECT id, from_ns, to_ns, type, ttl_ms, headers, payload, visible_at, attempt, ts, session_id, workspace, agent_id
			FROM mailbox
			WHERE to_ns = ? AND visible_at <= ?
			ORDER BY ts ASC
			LIMIT ?`, agentNS, now, maxMessages)
		if err != nil {
			return nil, fmt.Errorf("mailbox: poll: %w", err)
		}

		var messages []agent.Message
		for rows.Next() {
			msg, err := scanMessage(rows)
			if err != nil {
				errs.Ignore(rows.Close(), "close mailbox poll rows")
				return nil, err
			}
			messages = append(messages, msg)
		}
		errs.Ignore(rows.Close(), "close mailbox poll rows")

		if len(messages) > 0 {
			// Update visibility timeout for leased messages (30 seconds default)
			newVisibleAt := time.Now().Add(30 * time.Second).Unix()
			for i := range messages {
				_, err := s.db.ExecContext(ctx, `
					UPDATE mailbox SET visible_at = ?, attempt = attempt + 1 WHERE id = ?`,
					newVisibleAt, messages[i].ID)
				if err != nil {
					return nil, fmt.Errorf("mailbox: update visibility: %w", err)
				}
				// Update the message struct to reflect new values
				messages[i].VisibleAt = newVisibleAt
				messages[i].Attempt++
			}
			return messages, nil
		}

		// No messages available, check if we should wait
		if time.Now().After(deadline) {
			return []agent.Message{}, nil
		}

		// Sleep briefly before retrying
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// Continue polling
		}
	}
}

func (s *sqlStore) Ack(ctx context.Context, messageID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mailbox WHERE id = ?`, messageID)
	if err != nil {
		return fmt.Errorf("mailbox: ack: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mailbox: ack rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) Nack(ctx context.Context, messageID string, visibilityTimeout time.Duration) error {
	newVisibleAt := time.Now().Add(visibilityTimeout).Unix()
	res, err := s.db.ExecContext(ctx, `UPDATE mailbox SET visible_at = ? WHERE id = ?`, newVisibleAt, messageID)
	if err != nil {
		return fmt.Errorf("mailbox: nack: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mailbox: nack rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) List(ctx context.Context, agentNS string, limit int) ([]agent.Message, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, from_ns, to_ns, type, ttl_ms, headers, payload, visible_at, attempt, ts, session_id, workspace, agent_id
		FROM mailbox
		WHERE to_ns = ?
		ORDER BY ts DESC
		LIMIT ?`, agentNS, limit)
	if err != nil {
		return nil, fmt.Errorf("mailbox: list: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close mailbox list rows")
	}()

	var messages []agent.Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func (s *sqlStore) Delete(ctx context.Context, messageID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mailbox WHERE id = ?`, messageID)
	if err != nil {
		return fmt.Errorf("mailbox: delete: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mailbox: delete rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) ListBySession(ctx context.Context, sessionID string, limit int) ([]agent.Message, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, from_ns, to_ns, type, ttl_ms, headers, payload, visible_at, attempt, ts, session_id, workspace, agent_id
		FROM mailbox
		WHERE session_id = ?
		ORDER BY ts DESC
		LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("mailbox: list by session: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close mailbox list by session rows")
	}()

	var messages []agent.Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func (s *sqlStore) ListByWorkspace(ctx context.Context, workspace string, limit int) ([]agent.Message, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, from_ns, to_ns, type, ttl_ms, headers, payload, visible_at, attempt, ts, session_id, workspace, agent_id
		FROM mailbox
		WHERE workspace = ?
		ORDER BY ts DESC
		LIMIT ?`, workspace, limit)
	if err != nil {
		return nil, fmt.Errorf("mailbox: list by workspace: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close mailbox list by workspace rows")
	}()

	var messages []agent.Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS mailbox (
	id           TEXT PRIMARY KEY,
	from_ns      TEXT NOT NULL,
	to_ns        TEXT NOT NULL,
	type         TEXT NOT NULL,
	ttl_ms       INTEGER NOT NULL,
	headers      TEXT,
	payload      TEXT NOT NULL,
	visible_at   INTEGER NOT NULL,
	attempt      INTEGER NOT NULL DEFAULT 0,
	ts           INTEGER NOT NULL,
	session_id   TEXT NOT NULL DEFAULT '',
	workspace    TEXT NOT NULL DEFAULT '',
	agent_id     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_mailbox_to_visible ON mailbox(to_ns, visible_at);
CREATE INDEX IF NOT EXISTS idx_mailbox_ts ON mailbox(ts);
CREATE INDEX IF NOT EXISTS idx_mailbox_session ON mailbox(session_id);
CREATE INDEX IF NOT EXISTS idx_mailbox_workspace ON mailbox(workspace);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("mailbox: migrate: %w", err)
	}

	// Add session columns if they don't exist (migration for existing databases)
	sessionColumns := []string{
		`ALTER TABLE mailbox ADD COLUMN session_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE mailbox ADD COLUMN workspace TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE mailbox ADD COLUMN agent_id TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range sessionColumns {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			// Ignore "duplicate column" errors
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("mailbox: migrate session columns: %w", err)
			}
		}
	}

	// Create indexes for session columns (may already exist)
	sessionIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_mailbox_session ON mailbox(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_mailbox_workspace ON mailbox(workspace)`,
	}
	for _, stmt := range sessionIndexes {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			// Ignore errors, index might already exist
			_ = err
		}
	}

	return nil
}

// isDuplicateColumnError checks if an error is a "duplicate column" error.
// SQLite error format: "SQL logic error: duplicate column name: xxx (1)"
func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Simple substring search for "duplicate column name"
	const needle = "duplicate column name"
	if len(errStr) < len(needle) {
		return false
	}
	for i := 0; i <= len(errStr)-len(needle); i++ {
		if errStr[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func scanMessage(rows *sql.Rows) (agent.Message, error) {
	var msg agent.Message
	var headersJSON string
	var payloadJSON string
	if err := rows.Scan(&msg.ID, &msg.FromNS, &msg.ToNS, &msg.Type, &msg.TTLMS, &headersJSON, &payloadJSON, &msg.VisibleAt, &msg.Attempt, &msg.Timestamp, &msg.SessionID, &msg.Workspace, &msg.AgentID); err != nil {
		return agent.Message{}, fmt.Errorf("mailbox: scan: %w", err)
	}

	// Parse headers
	if headersJSON != "" {
		if err := json.Unmarshal([]byte(headersJSON), &msg.Headers); err != nil {
			return agent.Message{}, fmt.Errorf("mailbox: unmarshal headers: %w", err)
		}
	}

	// Set payload as RawMessage
	msg.Payload = json.RawMessage(payloadJSON)

	return msg, nil
}

// ErrNotFound indicates the message was not found.
var ErrNotFound = errors.New("mailbox: not found")
