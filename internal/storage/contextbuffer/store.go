package contextbuffer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
)

// Entry represents a context buffer entry.
type Entry struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	SessionID   string         `json:"session_id"`
	AgentID     string         `json:"agent_id,omitempty"`
	Source      string         `json:"source"`
	Text        string         `json:"text"`
	Priority    int            `json:"priority"` // 1=high, 2=normal, 3=low
	CreatedAt   time.Time      `json:"created_at"`
	ExpiresAt   time.Time      `json:"expires_at"`
	ConsumedAt  *time.Time     `json:"consumed_at,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// EnqueueParams for adding context to the buffer.
type EnqueueParams struct {
	WorkspaceID string         // Required
	SessionID   string         // Required
	AgentID     string         // Optional: for multi-agent scenarios
	Source      string         // Required: identifies the hook/origin (e.g., "smart-grep")
	Text        string         // Required: markdown content
	Priority    int            // 1-3, default 2 (1=high, 3=low)
	TTL         time.Duration  // Default 60s
	Dedupe      bool           // If true, skip if same source+text exists unconsumed
	Metadata    map[string]any // Optional extensibility
}

// DrainParams for retrieving context from the buffer.
type DrainParams struct {
	WorkspaceID  string   // Required
	SessionID    string   // Required
	AgentID      string   // Optional: filter by agent
	Sources      []string // Optional: filter by source
	MinPriority  int      // Optional: only priority <= this (1=high priority only)
	Limit        int      // Max entries to return (default 50)
	MarkConsumed bool     // If true, mark as consumed (default true when draining)
}

// DrainResult from a drain operation.
type DrainResult struct {
	Entries      []Entry `json:"entries"`
	TotalPending int     `json:"total_pending"` // Remaining after drain
	Markdown     string  `json:"markdown"`      // Pre-rendered output
}

// Store defines the persistence interface for context buffer.
type Store interface {
	Close() error

	// Enqueue adds context to the buffer.
	// If Dedupe=true and matching hash exists (unconsumed), updates timestamp only.
	Enqueue(ctx context.Context, params EnqueueParams) (*Entry, error)

	// Drain retrieves and optionally marks entries as consumed.
	// Returns entries ordered by priority ASC, created_at ASC.
	Drain(ctx context.Context, params DrainParams) (*DrainResult, error)

	// Peek is like Drain but never marks consumed.
	Peek(ctx context.Context, params DrainParams) (*DrainResult, error)

	// PruneExpired removes expired + old consumed entries.
	PruneExpired(ctx context.Context, maxConsumedAge time.Duration) (int, error)

	// Count returns pending entries for a session.
	Count(ctx context.Context, workspaceID, sessionID string) (int, error)

	// DB returns the underlying database connection.
	DB() *sql.DB
}

type sqlStore struct {
	db    *sql.DB
	close func() error
}

// Open opens or creates a SQLite-backed context buffer database at root/contextbuffer.db and returns a Store.
// It applies necessary schema migrations using the provided context and returns an error if opening or migrating the database fails.
// The returned Store holds an internal close function; callers must call Close on the Store to release underlying database resources.
func Open(ctx context.Context, root string) (Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "CONTEXTBUFFER", "contextbuffer.db", migrate)
	if err != nil {
		return nil, fmt.Errorf("contextbuffer: open db: %w", err)
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

func (s *sqlStore) Enqueue(ctx context.Context, params EnqueueParams) (*Entry, error) {
	if params.WorkspaceID == "" {
		return nil, errors.New("contextbuffer: workspace_id required")
	}
	if params.SessionID == "" {
		return nil, errors.New("contextbuffer: session_id required")
	}
	if params.Source == "" {
		return nil, errors.New("contextbuffer: source required")
	}
	if params.Text == "" {
		return nil, errors.New("contextbuffer: text required")
	}

	// Defaults
	if params.Priority < 1 || params.Priority > 3 {
		params.Priority = 2
	}
	if params.TTL <= 0 {
		params.TTL = 60 * time.Second
	}

	now := time.Now()
	expiresAt := now.Add(params.TTL)

	// Compute dedupe hash if requested
	var dedupeHash sql.NullString
	if params.Dedupe {
		hash := sha256.Sum256([]byte(params.Source + "\x00" + params.Text))
		dedupeHash = sql.NullString{String: hex.EncodeToString(hash[:]), Valid: true}

		// Check for existing unconsumed entry with same hash
		var existingID string
		err := s.db.QueryRowContext(ctx, `
			SELECT id FROM context_entries
			WHERE workspace_id = ? AND session_id = ? AND dedupe_hash = ?
			  AND consumed_at_ms IS NULL AND expires_at_ms > ?
			LIMIT 1`,
			params.WorkspaceID, params.SessionID, dedupeHash.String, now.UnixMilli()).Scan(&existingID)

		if err == nil {
			// Found existing - update timestamp and return
			_, err = s.db.ExecContext(ctx, `
				UPDATE context_entries
				SET created_at_ms = ?, expires_at_ms = ?
				WHERE id = ?`,
				now.UnixMilli(), expiresAt.UnixMilli(), existingID)
			if err != nil {
				return nil, fmt.Errorf("contextbuffer: update existing: %w", err)
			}

			return &Entry{
				ID:          existingID,
				WorkspaceID: params.WorkspaceID,
				SessionID:   params.SessionID,
				AgentID:     params.AgentID,
				Source:      params.Source,
				Text:        params.Text,
				Priority:    params.Priority,
				CreatedAt:   now,
				ExpiresAt:   expiresAt,
			}, nil
		} else if err != sql.ErrNoRows {
			return nil, fmt.Errorf("contextbuffer: check dedupe: %w", err)
		}
	}

	// Generate new entry
	id := ulid.Make().String()

	var metadataJSON sql.NullString
	if len(params.Metadata) > 0 {
		b, err := json.Marshal(params.Metadata)
		if err != nil {
			return nil, fmt.Errorf("contextbuffer: marshal metadata: %w", err)
		}
		metadataJSON = sql.NullString{String: string(b), Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO context_entries (id, workspace_id, session_id, agent_id, source, text, priority, dedupe_hash, created_at_ms, expires_at_ms, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, params.WorkspaceID, params.SessionID, params.AgentID, params.Source, params.Text, params.Priority,
		dedupeHash, now.UnixMilli(), expiresAt.UnixMilli(), metadataJSON)
	if err != nil {
		return nil, fmt.Errorf("contextbuffer: insert: %w", err)
	}

	return &Entry{
		ID:          id,
		WorkspaceID: params.WorkspaceID,
		SessionID:   params.SessionID,
		AgentID:     params.AgentID,
		Source:      params.Source,
		Text:        params.Text,
		Priority:    params.Priority,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		Metadata:    params.Metadata,
	}, nil
}

func (s *sqlStore) Drain(ctx context.Context, params DrainParams) (*DrainResult, error) {
	params.MarkConsumed = true
	return s.drainInternal(ctx, params)
}

func (s *sqlStore) Peek(ctx context.Context, params DrainParams) (*DrainResult, error) {
	params.MarkConsumed = false
	return s.drainInternal(ctx, params)
}

// drainInternal retrieves entries, optionally marks them consumed, and renders markdown.
//
// Index:
// - Purpose: Drain buffered context with optional consumption and markdown rendering
// - Flow: begin tx → query entries → optionally mark consumed → count pending → commit → render markdown
// - SideEffects: database transaction; optional updates to consumed_at_ms
// - FailureModes: query errors, update errors, commit errors
// - Related: renderMarkdown, scanEntry
// - Keywords: contextbuffer_drain, mark_consumed, priority, total_pending, markdown
func (s *sqlStore) drainInternal(ctx context.Context, params DrainParams) (*DrainResult, error) {
	if params.WorkspaceID == "" {
		return nil, errors.New("contextbuffer: workspace_id required")
	}
	if params.SessionID == "" {
		return nil, errors.New("contextbuffer: session_id required")
	}
	if params.Limit <= 0 {
		params.Limit = 50
	}

	now := time.Now()
	nowMs := now.UnixMilli()

	// Build query with optional filters
	var args []any
	query := `
		SELECT id, workspace_id, session_id, agent_id, source, text, priority, created_at_ms, expires_at_ms, consumed_at_ms, metadata
		FROM context_entries
		WHERE workspace_id = ? AND session_id = ?
		  AND consumed_at_ms IS NULL AND expires_at_ms > ?`
	args = append(args, params.WorkspaceID, params.SessionID, nowMs)

	if params.AgentID != "" {
		query += ` AND agent_id = ?`
		args = append(args, params.AgentID)
	}

	if len(params.Sources) > 0 {
		placeholders := make([]string, len(params.Sources))
		for i, src := range params.Sources {
			placeholders[i] = "?"
			args = append(args, src)
		}
		query += ` AND source IN (` + strings.Join(placeholders, ",") + `)`
	}

	if params.MinPriority > 0 {
		query += ` AND priority <= ?`
		args = append(args, params.MinPriority)
	}

	query += ` ORDER BY priority ASC, created_at_ms ASC LIMIT ?`
	args = append(args, params.Limit)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("contextbuffer: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("contextbuffer: query: %w", err)
	}

	var entries []Entry
	var ids []string
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			errs.Ignore(rows.Close(), "close contextbuffer drain rows")
			return nil, err
		}
		entries = append(entries, entry)
		ids = append(ids, entry.ID)
	}
	errs.Ignore(rows.Close(), "close contextbuffer drain rows")
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("contextbuffer: rows err: %w", err)
	}

	// Mark as consumed if requested
	if params.MarkConsumed && len(ids) > 0 {
		placeholders := make([]string, len(ids))
		updateArgs := []any{nowMs}
		for i, id := range ids {
			placeholders[i] = "?"
			updateArgs = append(updateArgs, id)
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE context_entries SET consumed_at_ms = ? WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
			updateArgs...)
		if err != nil {
			return nil, fmt.Errorf("contextbuffer: mark consumed: %w", err)
		}
	}

	// Count remaining pending
	var totalPending int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM context_entries
		WHERE workspace_id = ? AND session_id = ?
		  AND consumed_at_ms IS NULL AND expires_at_ms > ?`,
		params.WorkspaceID, params.SessionID, nowMs).Scan(&totalPending)
	if err != nil {
		return nil, fmt.Errorf("contextbuffer: count pending: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("contextbuffer: commit: %w", err)
	}

	// Render markdown
	markdown := renderMarkdown(entries)

	return &DrainResult{
		Entries:      entries,
		TotalPending: totalPending,
		Markdown:     markdown,
	}, nil
}

func (s *sqlStore) PruneExpired(ctx context.Context, maxConsumedAge time.Duration) (int, error) {
	if maxConsumedAge <= 0 {
		maxConsumedAge = 24 * time.Hour
	}

	now := time.Now()
	nowMs := now.UnixMilli()
	consumedCutoff := now.Add(-maxConsumedAge).UnixMilli()

	// Delete: expired unconsumed OR old consumed
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM context_entries
		WHERE (expires_at_ms <= ? AND consumed_at_ms IS NULL)
		   OR (consumed_at_ms IS NOT NULL AND consumed_at_ms < ?)`,
		nowMs, consumedCutoff)
	if err != nil {
		return 0, fmt.Errorf("contextbuffer: prune: %w", err)
	}

	count, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("contextbuffer: prune rows affected: %w", err)
	}

	return int(count), nil
}

func (s *sqlStore) Count(ctx context.Context, workspaceID, sessionID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM context_entries
		WHERE workspace_id = ? AND session_id = ?
		  AND consumed_at_ms IS NULL AND expires_at_ms > ?`,
		workspaceID, sessionID, time.Now().UnixMilli()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("contextbuffer: count: %w", err)
	}
	return count, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS context_entries (
	id              TEXT PRIMARY KEY,
	workspace_id    TEXT NOT NULL,
	session_id      TEXT NOT NULL,
	agent_id        TEXT NOT NULL DEFAULT '',

	-- Content
	source          TEXT NOT NULL,
	text            TEXT NOT NULL,
	priority        INTEGER NOT NULL DEFAULT 2,

	-- Deduplication
	dedupe_hash     TEXT,

	-- Lifecycle
	created_at_ms   INTEGER NOT NULL,
	expires_at_ms   INTEGER NOT NULL,
	consumed_at_ms  INTEGER,

	-- Metadata
	metadata        TEXT
);

CREATE INDEX IF NOT EXISTS idx_context_pending ON context_entries(
	workspace_id, session_id, consumed_at_ms, expires_at_ms, priority, created_at_ms
);
CREATE INDEX IF NOT EXISTS idx_context_dedupe ON context_entries(workspace_id, session_id, dedupe_hash)
	WHERE consumed_at_ms IS NULL;
CREATE INDEX IF NOT EXISTS idx_context_cleanup ON context_entries(expires_at_ms, consumed_at_ms);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("contextbuffer: migrate: %w", err)
	}
	return nil
}

func scanEntry(rows *sql.Rows) (Entry, error) {
	var e Entry
	var createdAtMs, expiresAtMs int64
	var consumedAtMs sql.NullInt64
	var metadataJSON sql.NullString

	if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.SessionID, &e.AgentID, &e.Source, &e.Text, &e.Priority,
		&createdAtMs, &expiresAtMs, &consumedAtMs, &metadataJSON); err != nil {
		return Entry{}, fmt.Errorf("contextbuffer: scan: %w", err)
	}

	e.CreatedAt = time.UnixMilli(createdAtMs)
	e.ExpiresAt = time.UnixMilli(expiresAtMs)
	if consumedAtMs.Valid {
		t := time.UnixMilli(consumedAtMs.Int64)
		e.ConsumedAt = &t
	}

	if metadataJSON.Valid && metadataJSON.String != "" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &e.Metadata); err != nil {
			return Entry{}, fmt.Errorf("contextbuffer: unmarshal metadata: %w", err)
		}
	}

	return e, nil
}

// renderMarkdown formats entries as markdown for injection.
func renderMarkdown(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString("**")
		sb.WriteString(e.Source)
		sb.WriteString("**:\n")
		sb.WriteString(e.Text)
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String())
}

// ErrNotFound indicates the entry was not found.
var ErrNotFound = errors.New("contextbuffer: not found")
