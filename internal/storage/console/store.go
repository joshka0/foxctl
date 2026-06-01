package console

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

// ConsoleSession represents a persisted console session for interactive actor I/O.
//
// Each console session links a user's interactive terminal to an actor,
// enabling message exchange through the mailbox transport.
type ConsoleSession struct {
	// ConsoleID is the unique identifier (ULID primary key)
	ConsoleID string `json:"console_id"`

	// ActorID is the namespace of the actor this console is attached to
	ActorID string `json:"actor_id"`

	// SessionID is an optional AI tool session ID for context linkage
	SessionID string `json:"session_id,omitempty"`

	// Workspace is the workspace path
	Workspace string `json:"workspace"`

	// CreatedAt is when the console session was created
	CreatedAt time.Time `json:"created_at"`

	// LastAttachedAt is when a user last attached to this console
	LastAttachedAt time.Time `json:"last_attached_at"`

	// Meta holds optional metadata as key-value pairs
	Meta map[string]any `json:"meta,omitempty"`
}

// Store provides CRUD operations for console sessions.
type Store interface {
	// Close closes the store.
	Close() error

	// Create persists a new console session.
	Create(ctx context.Context, session ConsoleSession) error

	// Get retrieves a console session by ID.
	Get(ctx context.Context, consoleID string) (ConsoleSession, error)

	// GetByActor retrieves all console sessions for an actor.
	GetByActor(ctx context.Context, actorID string) ([]ConsoleSession, error)

	// GetBySession retrieves console sessions linked to an AI session.
	GetBySession(ctx context.Context, sessionID string) ([]ConsoleSession, error)

	// List returns console sessions, optionally filtered by workspace.
	List(ctx context.Context, workspace string, limit int) ([]ConsoleSession, error)

	// UpdateAttached updates the last attached timestamp.
	UpdateAttached(ctx context.Context, consoleID string) error

	// LinkSession links a console to an AI session.
	LinkSession(ctx context.Context, consoleID, sessionID string) error

	// Delete removes a console session.
	Delete(ctx context.Context, consoleID string) error
}

// ErrNotFound indicates the console session was not found.
var ErrNotFound = errors.New("console: session not found")

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewStore creates a new SQLite-backed console store.
// The db connection should already be open.
func NewStore(ctx context.Context, db *sql.DB) (*SQLiteStore, error) {
	store := &SQLiteStore{db: db}
	if err := store.ensureSchema(ctx); err != nil {
		return nil, fmt.Errorf("console store: ensure schema: %w", err)
	}
	return store, nil
}

// ensureSchema creates the console_sessions table if it doesn't exist.
func (s *SQLiteStore) ensureSchema(ctx context.Context) error {
	ddl := `
CREATE TABLE IF NOT EXISTS console_sessions (
	console_id       TEXT PRIMARY KEY,
	actor_id         TEXT NOT NULL,
	session_id       TEXT,
	workspace        TEXT NOT NULL,
	created_at       TEXT NOT NULL,
	last_attached_at TEXT NOT NULL,
	meta             TEXT
);

CREATE INDEX IF NOT EXISTS idx_console_actor ON console_sessions(actor_id);
CREATE INDEX IF NOT EXISTS idx_console_workspace ON console_sessions(workspace);
CREATE INDEX IF NOT EXISTS idx_console_session ON console_sessions(session_id);
`
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("create console_sessions: %w", err)
	}
	return nil
}

// Close is a no-op since we don't own the db connection.
func (s *SQLiteStore) Close() error {
	return nil
}

// Create persists a new console session.
func (s *SQLiteStore) Create(ctx context.Context, session ConsoleSession) error {
	now := time.Now().UTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.LastAttachedAt.IsZero() {
		session.LastAttachedAt = now
	}

	var metaJSON sql.NullString
	if len(session.Meta) > 0 {
		b, err := json.Marshal(session.Meta)
		if err != nil {
			return fmt.Errorf("marshal meta: %w", err)
		}
		metaJSON = sql.NullString{String: string(b), Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO console_sessions (console_id, actor_id, session_id, workspace, created_at, last_attached_at, meta)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, session.ConsoleID, session.ActorID, nullString(session.SessionID),
		session.Workspace, sqlutil.FormatTimestamp(session.CreatedAt),
		sqlutil.FormatTimestamp(session.LastAttachedAt), metaJSON)
	if err != nil {
		return fmt.Errorf("create console session: %w", err)
	}
	return nil
}

// Get retrieves a console session by ID.
func (s *SQLiteStore) Get(ctx context.Context, consoleID string) (ConsoleSession, error) {
	var session ConsoleSession
	var sessionID sql.NullString
	var createdAt, lastAttached string
	var metaJSON sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT console_id, actor_id, session_id, workspace, created_at, last_attached_at, meta
		FROM console_sessions
		WHERE console_id = ?
	`, consoleID).Scan(&session.ConsoleID, &session.ActorID, &sessionID,
		&session.Workspace, &createdAt, &lastAttached, &metaJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConsoleSession{}, ErrNotFound
		}
		return ConsoleSession{}, fmt.Errorf("get console session: %w", err)
	}

	session.SessionID = sessionID.String
	if err := parseSessionTimestamps(&session, createdAt, lastAttached); err != nil {
		return ConsoleSession{}, err
	}
	if err := scanSessionMeta(&session, metaJSON); err != nil {
		return ConsoleSession{}, err
	}

	return session, nil
}

// GetByActor retrieves all console sessions for an actor.
func (s *SQLiteStore) GetByActor(ctx context.Context, actorID string) ([]ConsoleSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT console_id, actor_id, session_id, workspace, created_at, last_attached_at, meta
		FROM console_sessions
		WHERE actor_id = ?
		ORDER BY last_attached_at DESC
	`, actorID)
	if err != nil {
		return nil, fmt.Errorf("get by actor: %w", err)
	}
	defer rows.Close()

	return scanSessions(rows)
}

// GetBySession retrieves console sessions linked to an AI session.
func (s *SQLiteStore) GetBySession(ctx context.Context, sessionID string) ([]ConsoleSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT console_id, actor_id, session_id, workspace, created_at, last_attached_at, meta
		FROM console_sessions
		WHERE session_id = ?
		ORDER BY last_attached_at DESC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get by session: %w", err)
	}
	defer rows.Close()

	return scanSessions(rows)
}

// List returns console sessions, optionally filtered by workspace.
func (s *SQLiteStore) List(ctx context.Context, workspace string, limit int) ([]ConsoleSession, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows *sql.Rows
	var err error

	if workspace != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT console_id, actor_id, session_id, workspace, created_at, last_attached_at, meta
			FROM console_sessions
			WHERE workspace = ?
			ORDER BY last_attached_at DESC
			LIMIT ?
		`, workspace, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT console_id, actor_id, session_id, workspace, created_at, last_attached_at, meta
			FROM console_sessions
			ORDER BY last_attached_at DESC
			LIMIT ?
		`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list console sessions: %w", err)
	}
	defer rows.Close()

	return scanSessions(rows)
}

// UpdateAttached updates the last attached timestamp.
func (s *SQLiteStore) UpdateAttached(ctx context.Context, consoleID string) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		UPDATE console_sessions
		SET last_attached_at = ?
		WHERE console_id = ?
	`, sqlutil.FormatTimestamp(now), consoleID)
	if err != nil {
		return fmt.Errorf("update attached: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update attached rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// LinkSession links a console to an AI session.
func (s *SQLiteStore) LinkSession(ctx context.Context, consoleID, sessionID string) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		UPDATE console_sessions
		SET session_id = ?, last_attached_at = ?
		WHERE console_id = ?
	`, sessionID, sqlutil.FormatTimestamp(now), consoleID)
	if err != nil {
		return fmt.Errorf("link session: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("link session rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a console session.
func (s *SQLiteStore) Delete(ctx context.Context, consoleID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM console_sessions WHERE console_id = ?`, consoleID)
	if err != nil {
		return fmt.Errorf("delete console session: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete console session rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// scanSessions scans rows into a slice of ConsoleSession.
func scanSessions(rows *sql.Rows) ([]ConsoleSession, error) {
	sessions := []ConsoleSession{}
	for rows.Next() {
		var session ConsoleSession
		var sessionID sql.NullString
		var createdAt, lastAttached string
		var metaJSON sql.NullString

		if err := rows.Scan(&session.ConsoleID, &session.ActorID, &sessionID,
			&session.Workspace, &createdAt, &lastAttached, &metaJSON); err != nil {
			return nil, fmt.Errorf("scan console session: %w", err)
		}

		session.SessionID = sessionID.String
		if err := parseSessionTimestamps(&session, createdAt, lastAttached); err != nil {
			return nil, err
		}

		if err := scanSessionMeta(&session, metaJSON); err != nil {
			return nil, err
		}

		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan console sessions iteration: %w", err)
	}

	return sessions, nil
}

func parseSessionTimestamps(session *ConsoleSession, createdAt, lastAttachedAt string) error {
	var err error
	session.CreatedAt, err = sqlutil.ScanTimestamp(createdAt)
	if err != nil {
		return fmt.Errorf("console: scan session created_at: %w", err)
	}
	session.LastAttachedAt, err = sqlutil.ScanTimestamp(lastAttachedAt)
	if err != nil {
		return fmt.Errorf("console: scan session last_attached_at: %w", err)
	}
	return nil
}

func scanSessionMeta(session *ConsoleSession, metaJSON sql.NullString) error {
	if !metaJSON.Valid || metaJSON.String == "" {
		return nil
	}
	meta, err := decodeSessionMetaJSON(metaJSON.String)
	if err != nil {
		return fmt.Errorf("console: scan session meta: %w", err)
	}
	session.Meta = meta
	return nil
}

func decodeSessionMetaJSON(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, fmt.Errorf("session meta must be a JSON object")
	}
	return meta, nil
}

// nullString converts a string to sql.NullString.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// Ensure SQLiteStore implements Store.
var _ Store = (*SQLiteStore)(nil)
