package mailbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

// Store defines the persistence interface for mailbox messages.
type Store interface {
	Close() error
	Send(ctx context.Context, msg agent.Message) error
	Poll(ctx context.Context, agentNS string, leaseDuration time.Duration, maxMessages int) ([]agent.Message, error)
	PollByTypes(ctx context.Context, agentNS string, leaseDuration time.Duration, maxMessages int, types []agent.MessageType) ([]agent.Message, error)
	Ack(ctx context.Context, messageID string) error
	Nack(ctx context.Context, messageID string, visibilityTimeout time.Duration) error
	List(ctx context.Context, agentNS string, limit int) ([]agent.Message, error)
	Delete(ctx context.Context, messageID string) error
	// ListBySession returns messages for a specific session lineage.
	ListBySession(ctx context.Context, sessionID string, limit int) ([]agent.Message, error)
	// ListByWorkspace returns messages for a specific workspace.
	ListByWorkspace(ctx context.Context, workspace string, limit int) ([]agent.Message, error)
	// DB returns the underlying database connection for advanced operations.
	// This is primarily used by the actor.Watcher to create triggers.
	DB() *sql.DB
}

type sqlStore struct {
	db    *sql.DB
	close func() error
}

// Open opens the mailbox store rooted at the given storage root directory.
// The database driver is selected via the dbdriver env var conventions (e.g., FOXCTL_MAILBOX_DB_DRIVER).
func Open(ctx context.Context, root string) (Store, error) {
	dbPath := filepath.Join(root, "mailbox.db")
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "MAILBOX", filepath.Base(dbPath), migrate)
	if err != nil {
		return nil, fmt.Errorf("mailbox: open db: %w", err)
	}
	return &sqlStore{db: db, close: closeFn}, nil
}

func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func (s *sqlStore) DB() *sql.DB {
	return s.db
}

func (s *sqlStore) Send(ctx context.Context, msg agent.Message) error {
	headersJSON, err := json.Marshal(msg.Headers)
	if err != nil {
		return fmt.Errorf("mailbox: marshal headers: %w", err)
	}
	if err := validatePayloadJSON(msg.Payload); err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO mailbox (id, from_ns, to_ns, type, ttl_ms, headers, payload, visible_at, attempt, ts, session_id, workspace, agent_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		msg.ID, msg.FromNS, msg.ToNS, msg.Type, msg.TTLMS, string(headersJSON), string(msg.Payload), msg.VisibleAt, msg.Attempt, msg.Timestamp,
		msg.SessionID, msg.Workspace, msg.AgentID)
	if err != nil {
		return fmt.Errorf("mailbox: send: %w", err)
	}
	return nil
}

func (s *sqlStore) Poll(ctx context.Context, agentNS string, leaseDuration time.Duration, maxMessages int) ([]agent.Message, error) {
	return s.poll(ctx, agentNS, leaseDuration, maxMessages, nil)
}

func (s *sqlStore) PollByTypes(ctx context.Context, agentNS string, leaseDuration time.Duration, maxMessages int, types []agent.MessageType) ([]agent.Message, error) {
	return s.poll(ctx, agentNS, leaseDuration, maxMessages, types)
}

// poll retrieves visible messages and leases them for delivery in one transaction.
//
// Index:
// - Purpose: Atomically fetch and lease mailbox messages for an agent
// - Flow: begin tx → query visible messages → update visibility → commit → return
// - SideEffects: database transaction; updates visible_at/attempt
// - FailureModes: tx errors, query errors, lease update errors
// - Related: scanMessage
// - Keywords: mailbox_poll, visible_at, lease_duration, attempt, to_ns
func (s *sqlStore) poll(ctx context.Context, agentNS string, leaseDuration time.Duration, maxMessages int, types []agent.MessageType) ([]agent.Message, error) {
	if maxMessages <= 0 {
		maxMessages = 1
	}
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Second
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mailbox: begin poll tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	now := time.Now().Unix()
	query := `
		SELECT id, from_ns, to_ns, type, ttl_ms, headers, payload, visible_at, attempt, ts, session_id, workspace, agent_id
		FROM mailbox
		WHERE to_ns = $1 AND visible_at <= $2`
	args := []any{agentNS, now}
	argIdx := 3
	if len(types) > 0 {
		var ph []string
		for range types {
			ph = append(ph, fmt.Sprintf("$%d", argIdx))
			argIdx++
		}
		query += " AND type IN (" + strings.Join(ph, ",") + ")"
		for _, msgType := range types {
			args = append(args, string(msgType))
		}
	}
	query += fmt.Sprintf(`
		ORDER BY ts ASC
		LIMIT $%d`, argIdx)
	args = append(args, maxMessages)

	rows, err := tx.QueryContext(ctx, query, args...)
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
	if err := rows.Err(); err != nil {
		errs.Ignore(rows.Close(), "close mailbox poll rows")
		return nil, err
	}
	errs.Ignore(rows.Close(), "close mailbox poll rows")

	if len(messages) == 0 {
		// Nothing to claim; commit the empty transaction
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("mailbox: commit empty poll: %w", err)
		}
		return []agent.Message{}, nil
	}

	// Lease claimed messages
	leaseUntil := visibleAtAfter(time.Now(), leaseDuration)
	for i := range messages {
		res, err := tx.ExecContext(ctx, `
			UPDATE mailbox
			SET visible_at = $1, attempt = attempt + 1
			WHERE id = $2 AND visible_at <= $3`,
			leaseUntil, messages[i].ID, now)
		if err != nil {
			return nil, fmt.Errorf("mailbox: update visibility: %w", err)
		}
		rowsAffected, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("mailbox: lease rows affected: %w", err)
		}
		if rowsAffected == 0 {
			// Lost the race to lease this message; skip it
			messages[i].ID = ""
			continue
		}
		messages[i].VisibleAt = leaseUntil
		messages[i].Attempt++
	}

	// Filter out any messages that failed to lease
	filtered := messages[:0]
	for _, msg := range messages {
		if msg.ID != "" {
			filtered = append(filtered, msg)
		}
	}

	if len(filtered) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("mailbox: commit empty lease: %w", err)
		}
		return []agent.Message{}, nil
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("mailbox: commit poll: %w", err)
	}

	return filtered, nil
}

func (s *sqlStore) Ack(ctx context.Context, messageID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mailbox WHERE id = $1`, messageID)
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
	newVisibleAt := visibleAtAfter(time.Now(), visibilityTimeout)
	res, err := s.db.ExecContext(ctx, `UPDATE mailbox SET visible_at = $1 WHERE id = $2`, newVisibleAt, messageID)
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

func visibleAtAfter(now time.Time, delay time.Duration) int64 {
	visibleAt := now.Add(delay).Unix()
	if delay > 0 && visibleAt <= now.Unix() {
		return now.Unix() + 1
	}
	return visibleAt
}

func (s *sqlStore) List(ctx context.Context, agentNS string, limit int) ([]agent.Message, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, from_ns, to_ns, type, ttl_ms, headers, payload, visible_at, attempt, ts, session_id, workspace, agent_id
		FROM mailbox
		WHERE to_ns = $1
		ORDER BY ts DESC
		LIMIT $2`, agentNS, limit)
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
	res, err := s.db.ExecContext(ctx, `DELETE FROM mailbox WHERE id = $1`, messageID)
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
		WHERE session_id = $1
		ORDER BY ts DESC
		LIMIT $2`, sessionID, limit)
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
		WHERE workspace = $1
		ORDER BY ts DESC
		LIMIT $2`, workspace, limit)
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
	sessionColumns := []struct {
		name       string
		columnType string
		defaultVal string
	}{
		{name: "session_id", columnType: "TEXT NOT NULL", defaultVal: "''"},
		{name: "workspace", columnType: "TEXT NOT NULL", defaultVal: "''"},
		{name: "agent_id", columnType: "TEXT NOT NULL", defaultVal: "''"},
	}
	for _, column := range sessionColumns {
		if err := dbutil.AddColumnIfNotExists(ctx, db, "mailbox", column.name, column.columnType, column.defaultVal); err != nil {
			return fmt.Errorf("mailbox: migrate add %s column: %w", column.name, err)
		}
	}

	// Create indexes for session columns (may already exist)
	sessionIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_mailbox_session ON mailbox(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_mailbox_workspace ON mailbox(workspace)`,
	}
	for _, stmt := range sessionIndexes {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("mailbox: migrate session index %q: %w", stmt, err)
		}
	}

	return nil
}

func scanMessage(rows *sql.Rows) (agent.Message, error) {
	var msg agent.Message
	var headersJSON string
	var payloadJSON string
	var sessionID, workspace, agentID sql.NullString
	if err := rows.Scan(&msg.ID, &msg.FromNS, &msg.ToNS, &msg.Type, &msg.TTLMS, &headersJSON, &payloadJSON, &msg.VisibleAt, &msg.Attempt, &msg.Timestamp, &sessionID, &workspace, &agentID); err != nil {
		return agent.Message{}, fmt.Errorf("mailbox: scan: %w", err)
	}

	msg.SessionID = sessionID.String
	msg.Workspace = workspace.String
	msg.AgentID = agentID.String

	// Parse headers
	if headersJSON != "" {
		if err := json.Unmarshal([]byte(headersJSON), &msg.Headers); err != nil {
			return agent.Message{}, fmt.Errorf("mailbox: unmarshal headers: %w", err)
		}
	}

	// Set payload as RawMessage
	payload := json.RawMessage(payloadJSON)
	if err := validatePayloadJSON(payload); err != nil {
		return agent.Message{}, err
	}
	msg.Payload = payload

	return msg, nil
}

func validatePayloadJSON(payload json.RawMessage) error {
	if !json.Valid(payload) {
		return fmt.Errorf("mailbox: invalid payload JSON")
	}
	return nil
}

// ErrNotFound indicates the message was not found.
var ErrNotFound = errors.New("mailbox: not found")
