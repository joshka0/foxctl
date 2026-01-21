// Package sessions implements storage for captured Claude Code conversation sessions.
package sessions

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
	"github.com/jkatigb/agentctl/internal/storage/vector"
)

// Ensure Store implements storage.SessionStore.
var _ storage.SessionStore = (*Store)(nil)

// Session aliases the shared storage type.
type Session = storage.Session

// SessionTurn aliases the shared turn type.
type SessionTurn = storage.SessionTurn

// ToolCall aliases the shared tool call type.
type ToolCall = storage.ToolCall

// SessionEdge aliases the shared edge type.
type SessionEdge = storage.SessionEdge

// Stats aliases the shared stats type.
type Stats = storage.SessionStats

// ListOptions aliases the shared list options type.
type ListOptions = storage.SessionListOptions

// TurnListOptions aliases the shared turn list options type.
type TurnListOptions = storage.SessionTurnListOptions

// SessionChunk aliases the shared chunk type.
type SessionChunk = storage.SessionChunk

// SessionChunkSummary aliases the shared chunk summary type.
type SessionChunkSummary = storage.SessionChunkSummary

// ScoredChunk aliases the shared scored chunk type.
type ScoredChunk = storage.ScoredChunk

// ChunkListOptions aliases the shared chunk list options type.
type ChunkListOptions = storage.ChunkListOptions

// ContextWindow aliases the shared context window type.
type ContextWindow = storage.ContextWindow

// ScoredContextWindow aliases the shared scored context window type.
type ScoredContextWindow = storage.ScoredContextWindow

// Session status constants (re-exported for convenience)
const (
	StatusRunning  = storage.SessionStatusRunning
	StatusOK       = storage.SessionStatusOK
	StatusError    = storage.SessionStatusError
	StatusCanceled = storage.SessionStatusCanceled
)

// Session edge type constants (re-exported for convenience)
const (
	EdgeContinues  = storage.SessionEdgeContinues
	EdgeForkedFrom = storage.SessionEdgeForkedFrom
	EdgeRelatesTo  = storage.SessionEdgeRelatesTo
)

// Store handles session persistence.
type Store struct {
	db   *sql.DB
	path string
}

// Connection pool defaults
const (
	defaultMaxOpenConns    = 10
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 10 * time.Minute
	defaultConnMaxIdleTime = 15 * time.Minute
)

// Open initializes the session store.
func Open(ctx context.Context, root string) (store *Store, err error) {
	dbPath := filepath.Join(root, "sessions.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("sessions: open db: %w", err)
	}
	defer errs.CloseOnErr(db, &err)

	db.SetMaxOpenConns(defaultMaxOpenConns)
	db.SetMaxIdleConns(defaultMaxIdleConns)
	db.SetConnMaxLifetime(defaultConnMaxLifetime)
	db.SetConnMaxIdleTime(defaultConnMaxIdleTime)

	store = &Store{db: db, path: dbPath}

	// Validate embedding dimensions against config (non-blocking warning)
	store.validateDimensionsOnOpen(ctx)

	return store, nil
}

// OpenFromConfig opens the sessions store using paths from config.
// This is the preferred way to open the store as it ensures correct path handling.
func OpenFromConfig(ctx context.Context, cfg config.Config) (*Store, error) {
	return Open(ctx, cfg.Storage.Root)
}

// Close releases resources.
func (s *Store) Close() error {
	return s.db.Close()
}

// Stats returns session count.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
		return Stats{}, fmt.Errorf("sessions: stats: %w", err)
	}
	return Stats{Count: count, Path: s.path}, nil
}

// Save inserts or updates a session.
func (s *Store) Save(ctx context.Context, session Session) (Session, error) {
	now := timeutil.NowUTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.UpdatedAt = now

	// Set defaults for lineage fields
	if session.AgentID == "" {
		session.AgentID = "agentctl"
	}
	if session.AgentType == "" {
		session.AgentType = "claude"
	}
	if session.Status == "" {
		session.Status = StatusOK
	}

	// Format JSON arrays
	accomplishedJSON, err := sqlutil.FormatJSON(session.Accomplished)
	if err != nil {
		return Session{}, fmt.Errorf("sessions: format accomplished: %w", err)
	}
	decisionsJSON, err := sqlutil.FormatJSON(session.Decisions)
	if err != nil {
		return Session{}, fmt.Errorf("sessions: format decisions: %w", err)
	}
	gotchasJSON, err := sqlutil.FormatJSON(session.Gotchas)
	if err != nil {
		return Session{}, fmt.Errorf("sessions: format gotchas: %w", err)
	}
	tagsJSON, err := sqlutil.FormatJSON(session.Tags)
	if err != nil {
		return Session{}, fmt.Errorf("sessions: format tags: %w", err)
	}
	keyFilesJSON, err := sqlutil.FormatJSON(session.KeyFiles)
	if err != nil {
		return Session{}, fmt.Errorf("sessions: format key_files: %w", err)
	}

	// Handle nullable parent_session_id
	var parentSessionID any
	if session.ParentSessionID != "" {
		parentSessionID = session.ParentSessionID
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO sessions (
	id, workspace_path, project_name, git_branch, claude_version,
	started_at, ended_at, summary, accomplished, decisions, gotchas,
	tags, key_files, tools_pattern, message_count, user_turns,
	tool_invocations, total_tokens, raw_jsonl_path, content_hash, embedding, embedding_model,
	created_at, updated_at, parent_session_id, agent_id, agent_type, status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	workspace_path = excluded.workspace_path,
	project_name = excluded.project_name,
	git_branch = excluded.git_branch,
	claude_version = excluded.claude_version,
	started_at = excluded.started_at,
	ended_at = excluded.ended_at,
	summary = excluded.summary,
	accomplished = excluded.accomplished,
	decisions = excluded.decisions,
	gotchas = excluded.gotchas,
	tags = excluded.tags,
	key_files = excluded.key_files,
	tools_pattern = excluded.tools_pattern,
	message_count = excluded.message_count,
	user_turns = excluded.user_turns,
	tool_invocations = excluded.tool_invocations,
	total_tokens = excluded.total_tokens,
	raw_jsonl_path = excluded.raw_jsonl_path,
	content_hash = COALESCE(excluded.content_hash, sessions.content_hash),
	embedding = COALESCE(excluded.embedding, sessions.embedding),
	embedding_model = COALESCE(excluded.embedding_model, sessions.embedding_model),
	updated_at = excluded.updated_at,
	parent_session_id = excluded.parent_session_id,
	agent_id = excluded.agent_id,
	agent_type = excluded.agent_type,
	status = excluded.status
`,
		session.ID, session.WorkspacePath, session.ProjectName, session.GitBranch, session.ClaudeVersion,
		sqlutil.FormatTimestamp(session.StartedAt), sqlutil.FormatTimestamp(session.EndedAt),
		session.Summary, accomplishedJSON, decisionsJSON, gotchasJSON,
		tagsJSON, keyFilesJSON, session.ToolsPattern, session.MessageCount, session.UserTurns,
		session.ToolInvocations, session.TotalTokens, session.RawJSONLPath, session.ContentHash, session.Embedding, session.EmbeddingModel,
		sqlutil.FormatTimestamp(session.CreatedAt), sqlutil.FormatTimestamp(session.UpdatedAt),
		parentSessionID, session.AgentID, session.AgentType, session.Status,
	)
	if err != nil {
		return Session{}, fmt.Errorf("sessions: save: %w", err)
	}
	return session, nil
}

// Get retrieves a session by ID.
func (s *Store) Get(ctx context.Context, id string) (Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, content_hash, embedding, embedding_model,
			created_at, updated_at, parent_session_id, agent_id, agent_type, status, key_questions
		FROM sessions
		WHERE id = ?`, id)
	return scanSession(row)
}

// List returns sessions matching the options.
func (s *Store) List(ctx context.Context, opts ListOptions) ([]Session, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}

	var conditions []string
	var args []any

	if opts.WorkspacePath != "" {
		conditions = append(conditions, "workspace_path = ?")
		args = append(args, opts.WorkspacePath)
	}
	if opts.ProjectName != "" {
		conditions = append(conditions, "project_name = ?")
		args = append(args, opts.ProjectName)
	}
	if len(opts.Tags) > 0 {
		// Match any of the specified tags
		// Use a replacer to escape LIKE special characters
		likeEscaper := strings.NewReplacer(
			`%`, `\%`,
			`_`, `\_`,
			`\`, `\\`,
		)
		tagConditions := make([]string, len(opts.Tags))
		for i, tag := range opts.Tags {
			tagConditions[i] = `tags LIKE ? ESCAPE '\'`
			escapedTag := likeEscaper.Replace(tag)
			args = append(args, `%"`+escapedTag+`"%`)
		}
		conditions = append(conditions, "("+strings.Join(tagConditions, " OR ")+")")
	}
	if len(opts.Statuses) > 0 {
		// Match any of the specified statuses
		placeholders := make([]string, len(opts.Statuses))
		for i, status := range opts.Statuses {
			placeholders[i] = "?"
			args = append(args, status)
		}
		conditions = append(conditions, "status IN ("+strings.Join(placeholders, ", ")+")")
	}

	query := `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, content_hash, embedding, embedding_model,
			created_at, updated_at, parent_session_id, agent_id, agent_type, status, key_questions
		FROM sessions`

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY started_at DESC LIMIT ? OFFSET ?"
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sessions: list: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close sessions list rows")
	}()

	return scanSessions(rows)
}

// Delete removes a session.
func (s *Store) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sessions: delete: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sessions: rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Search finds sessions matching the query in summary or tags.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 20
	}
	like := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, content_hash, embedding, embedding_model,
			created_at, updated_at, parent_session_id, agent_id, agent_type, status, key_questions
		FROM sessions
		WHERE LOWER(summary) LIKE ?
			OR LOWER(tags) LIKE ?
			OR LOWER(accomplished) LIKE ?
			OR LOWER(decisions) LIKE ?
			OR LOWER(gotchas) LIKE ?
		ORDER BY started_at DESC
		LIMIT ?`, like, like, like, like, like, limit)
	if err != nil {
		return nil, fmt.Errorf("sessions: search: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close sessions search rows")
	}()

	return scanSessions(rows)
}

// UpdateSummary updates the summary fields for a session.
func (s *Store) UpdateSummary(ctx context.Context, id string, summary string, accomplished, decisions, gotchas, userInsights, tags, keyFiles []string, toolsPattern string) error {
	return s.UpdateSummaryWithQuestions(ctx, id, summary, accomplished, decisions, gotchas, userInsights, tags, keyFiles, toolsPattern, nil)
}

// UpdateSummaryWithQuestions updates the summary fields for a session including key_questions.
func (s *Store) UpdateSummaryWithQuestions(ctx context.Context, id string, summary string, accomplished, decisions, gotchas, userInsights, tags, keyFiles []string, toolsPattern string, keyQuestions []string) error {
	accomplishedJSON, err := sqlutil.FormatJSON(accomplished)
	if err != nil {
		return fmt.Errorf("sessions: format accomplished: %w", err)
	}
	decisionsJSON, err := sqlutil.FormatJSON(decisions)
	if err != nil {
		return fmt.Errorf("sessions: format decisions: %w", err)
	}
	gotchasJSON, err := sqlutil.FormatJSON(gotchas)
	if err != nil {
		return fmt.Errorf("sessions: format gotchas: %w", err)
	}
	userInsightsJSON, err := sqlutil.FormatJSON(userInsights)
	if err != nil {
		return fmt.Errorf("sessions: format user_insights: %w", err)
	}
	tagsJSON, err := sqlutil.FormatJSON(tags)
	if err != nil {
		return fmt.Errorf("sessions: format tags: %w", err)
	}
	keyFilesJSON, err := sqlutil.FormatJSON(keyFiles)
	if err != nil {
		return fmt.Errorf("sessions: format key_files: %w", err)
	}
	keyQuestionsJSON, err := sqlutil.FormatJSON(keyQuestions)
	if err != nil {
		return fmt.Errorf("sessions: format key_questions: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET
			summary = ?,
			accomplished = ?,
			decisions = ?,
			gotchas = ?,
			user_insights = ?,
			tags = ?,
			key_files = ?,
			tools_pattern = ?,
			key_questions = ?,
			updated_at = ?
		WHERE id = ?`,
		summary, accomplishedJSON, decisionsJSON, gotchasJSON, userInsightsJSON, tagsJSON, keyFilesJSON, toolsPattern, keyQuestionsJSON,
		sqlutil.FormatTimestamp(timeutil.NowUTC()), id)
	if err != nil {
		return fmt.Errorf("sessions: update summary: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sessions: rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetEmbedding updates the embedding for a session.
func (s *Store) SetEmbedding(ctx context.Context, id string, embedding []byte, model string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET
			embedding = ?,
			embedding_model = ?,
			updated_at = ?
		WHERE id = ?`,
		embedding, model, sqlutil.FormatTimestamp(timeutil.NowUTC()), id)
	if err != nil {
		return fmt.Errorf("sessions: set embedding: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sessions: rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// SearchSimilar finds sessions similar to the given embedding using cosine similarity.
func (s *Store) SearchSimilar(ctx context.Context, workspace string, queryEmbedding []float32, limit int) ([]storage.SimilarSession, error) {
	if limit <= 0 {
		limit = 10
	}

	// Load sessions with embeddings, filtered by workspace if provided
	var rows *sql.Rows
	var err error

	if workspace != "" {
		// Filter by workspace for scoped search
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, workspace_path, project_name, git_branch, claude_version,
				started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
				tags, key_files, tools_pattern, message_count, user_turns,
				tool_invocations, total_tokens, raw_jsonl_path, content_hash, embedding, embedding_model,
				created_at, updated_at, parent_session_id, agent_id, agent_type, status, key_questions
			FROM sessions
			WHERE workspace_path = ? AND embedding IS NOT NULL AND LENGTH(embedding) > 0
			ORDER BY started_at DESC`, workspace)
	} else {
		// Global search (no workspace filter)
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, workspace_path, project_name, git_branch, claude_version,
				started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
				tags, key_files, tools_pattern, message_count, user_turns,
				tool_invocations, total_tokens, raw_jsonl_path, content_hash, embedding, embedding_model,
				created_at, updated_at, parent_session_id, agent_id, agent_type, status, key_questions
			FROM sessions
			WHERE embedding IS NOT NULL AND LENGTH(embedding) > 0
			ORDER BY started_at DESC`)
	}
	if err != nil {
		return nil, fmt.Errorf("sessions: search similar: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close sessions search similar rows")
	}()

	var results []storage.SimilarSession

	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			continue // Skip malformed rows
		}

		// Deserialize embedding
		sessionEmb := vector.DeserializeF32(session.Embedding)
		if len(sessionEmb) == 0 {
			continue
		}

		// Compute cosine similarity
		similarity := vector.Cosine(queryEmbedding, sessionEmb)

		results = append(results, storage.SimilarSession{
			Session:    session,
			Similarity: similarity,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sessions: rows error: %w", err)
	}

	// Sort by similarity descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	// Limit results
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// --- Content Hash Deduplication ---

// FindByContentHash finds a summarized session with the given content hash.
// Returns nil if no matching session is found.
// This is used to detect forked sessions with identical content for deduplication.
func (s *Store) FindByContentHash(ctx context.Context, contentHash string) (*Session, error) {
	if contentHash == "" {
		return nil, nil
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, content_hash, embedding, embedding_model,
			created_at, updated_at, parent_session_id, agent_id, agent_type, status, key_questions
		FROM sessions
		WHERE content_hash = ? AND summary != '' AND summary IS NOT NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, contentHash)

	session, err := scanSession(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sessions: find by content hash: %w", err)
	}
	return &session, nil
}

// SetContentHash sets the content hash for a session.
func (s *Store) SetContentHash(ctx context.Context, id, contentHash string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET content_hash = ?, updated_at = ? WHERE id = ?
	`, contentHash, timeutil.NowUTC(), id)
	if err != nil {
		return fmt.Errorf("sessions: set content hash: %w", err)
	}
	return nil
}

// --- Lineage Operations ---

// GetActive returns the most recently started session for a workspace/agent that hasn't ended.
// Returns nil if no active session exists.
// Uses status-based detection: only sessions with status = 'running' are considered active.
func (s *Store) GetActive(ctx context.Context, workspace, agentID string) (*Session, error) {
	if agentID == "" {
		agentID = "agentctl"
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, content_hash, embedding, embedding_model,
			created_at, updated_at, parent_session_id, agent_id, agent_type, status, key_questions
		FROM sessions
		WHERE workspace_path = ? AND agent_id = ? AND status = ?
		ORDER BY started_at DESC
		LIMIT 1`, workspace, agentID, StatusRunning)

	session, err := scanSession(row)
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("sessions: get active: %w", err)
	}
	return &session, nil
}

// SetStatus updates a session's status.
// If the status is terminal (ok, error, canceled), also sets ended_at.
func (s *Store) SetStatus(ctx context.Context, id, status string) error {
	now := sqlutil.FormatTimestamp(timeutil.NowUTC())

	var query string
	var args []any

	if storage.IsTerminalStatus(status) {
		// Terminal status: also set ended_at
		query = `UPDATE sessions SET status = ?, ended_at = ?, updated_at = ? WHERE id = ?`
		args = []any{status, now, now, id}
	} else {
		// Non-terminal status: clear ended_at (session is active)
		query = `UPDATE sessions SET status = ?, ended_at = NULL, updated_at = ? WHERE id = ?`
		args = []any{status, now, id}
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("sessions: set status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sessions: rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPendingRestore marks a session as needing restore after compaction.
// This is used by the PreCompact hook to signal the UserPromptSubmit hook.
func (s *Store) SetPendingRestore(ctx context.Context, id string) error {
	now := sqlutil.FormatTimestamp(timeutil.NowUTC())
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET pending_restore_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	if err != nil {
		return fmt.Errorf("sessions: set pending restore: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sessions: rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearPendingRestore removes the pending restore flag from a session.
// Called after restore has been successfully performed.
func (s *Store) ClearPendingRestore(ctx context.Context, id string) error {
	now := sqlutil.FormatTimestamp(timeutil.NowUTC())
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET pending_restore_at = NULL, updated_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return fmt.Errorf("sessions: clear pending restore: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sessions: rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// GetPendingRestore finds a session with pending restore for a workspace.
// Returns nil if no session needs restore. Only returns sessions where
// pending_restore_at is within the last 10 minutes (to avoid stale markers).
func (s *Store) GetPendingRestore(ctx context.Context, workspace string) (*Session, error) {
	// Calculate 10 minute cutoff
	cutoff := sqlutil.FormatTimestamp(timeutil.NowUTC().Add(-10 * time.Minute))

	query := `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, content_hash, embedding, embedding_model,
			created_at, updated_at, parent_session_id, agent_id, agent_type, status, key_questions
		FROM sessions
		WHERE workspace_path = ? AND pending_restore_at IS NOT NULL AND pending_restore_at > ?
		ORDER BY pending_restore_at DESC LIMIT 1`

	row := s.db.QueryRowContext(ctx, query, workspace, cutoff)
	session, err := scanSession(row)
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("sessions: get pending restore: %w", err)
	}
	return &session, nil
}

// FindLastSession finds the most recent session matching criteria.
// If statuses is empty, matches any status.
func (s *Store) FindLastSession(ctx context.Context, workspace, agentID string, statuses []string) (*Session, error) {
	if agentID == "" {
		agentID = "agentctl"
	}

	var args []any
	args = append(args, workspace, agentID)

	query := `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, content_hash, embedding, embedding_model,
			created_at, updated_at, parent_session_id, agent_id, agent_type, status, key_questions
		FROM sessions
		WHERE workspace_path = ? AND agent_id = ?`

	if len(statuses) > 0 {
		placeholders := make([]string, len(statuses))
		for i, s := range statuses {
			placeholders[i] = "?"
			args = append(args, s)
		}
		query += " AND status IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY started_at DESC LIMIT 1"

	row := s.db.QueryRowContext(ctx, query, args...)
	session, err := scanSession(row)
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("sessions: find last session: %w", err)
	}
	return &session, nil
}

// SaveEdge saves a session edge (relationship between sessions).
func (s *Store) SaveEdge(ctx context.Context, edge SessionEdge) error {
	now := timeutil.NowUTC()
	if edge.CreatedAt.IsZero() {
		edge.CreatedAt = now
	}

	var metadataJSON any
	if len(edge.Metadata) > 0 {
		data, err := sqlutil.FormatJSON(edge.Metadata)
		if err != nil {
			return fmt.Errorf("sessions: format edge metadata: %w", err)
		}
		metadataJSON = data
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session_edges (id, workspace, from_session, to_session, edge_type, created_at, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(from_session, to_session, edge_type) DO UPDATE SET
			metadata = excluded.metadata`,
		edge.ID, edge.Workspace, edge.FromSession, edge.ToSession, edge.EdgeType,
		sqlutil.FormatTimestamp(edge.CreatedAt), metadataJSON)
	if err != nil {
		return fmt.Errorf("sessions: save edge: %w", err)
	}
	return nil
}

// GetAncestorChain retrieves the ancestor chain of a session using CTE.
// Returns sessions in order from immediate parent to oldest ancestor.
// maxDepth limits how far back to traverse (0 = no limit).
func (s *Store) GetAncestorChain(ctx context.Context, sessionID string, maxDepth int) ([]Session, error) {
	if maxDepth <= 0 {
		maxDepth = 100 // reasonable default
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH RECURSIVE ancestors(id, depth) AS (
			SELECT parent_session_id, 1 FROM sessions WHERE id = ? AND parent_session_id IS NOT NULL
			UNION ALL
			SELECT s.parent_session_id, a.depth + 1
			FROM sessions s
			JOIN ancestors a ON s.id = a.id
			WHERE s.parent_session_id IS NOT NULL AND a.depth < ?
		)
		SELECT s.id, s.workspace_path, s.project_name, s.git_branch, s.claude_version,
			s.started_at, s.ended_at, s.summary, s.accomplished, s.decisions, s.gotchas, s.user_insights,
			s.tags, s.key_files, s.tools_pattern, s.message_count, s.user_turns,
			s.tool_invocations, s.total_tokens, s.raw_jsonl_path, s.embedding, s.embedding_model,
			s.created_at, s.updated_at, s.parent_session_id, s.agent_id, s.status, s.key_questions
		FROM sessions s
		JOIN ancestors a ON s.id = a.id
		ORDER BY a.depth ASC`, sessionID, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("sessions: get ancestor chain: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close ancestor chain rows")
	}()

	return scanSessions(rows)
}

// GetEdges retrieves all edges for a session (both incoming and outgoing).
func (s *Store) GetEdges(ctx context.Context, sessionID string) ([]SessionEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace, from_session, to_session, edge_type, created_at, metadata
		FROM session_edges
		WHERE from_session = ? OR to_session = ?
		ORDER BY created_at DESC`, sessionID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("sessions: get edges: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close session edges rows")
	}()

	var edges []SessionEdge
	for rows.Next() {
		var edge SessionEdge
		var createdAt sql.NullString
		var metadata sql.NullString

		if err := rows.Scan(&edge.ID, &edge.Workspace, &edge.FromSession, &edge.ToSession,
			&edge.EdgeType, &createdAt, &metadata); err != nil {
			return nil, fmt.Errorf("sessions: scan edge: %w", err)
		}

		if createdAt.Valid {
			ts, _ := sqlutil.ScanTimestamp(createdAt.String)
			edge.CreatedAt = ts
		}
		if metadata.Valid {
			var m map[string]any
			if err := sqlutil.ScanJSON(metadata.String, &m); err == nil {
				edge.Metadata = m
			}
		}

		edges = append(edges, edge)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sessions: edges rows error: %w", err)
	}
	return edges, nil
}

// --- Turn Operations ---

// SaveTurn inserts or updates a session turn.
func (s *Store) SaveTurn(ctx context.Context, turn SessionTurn) (SessionTurn, error) {
	now := timeutil.NowUTC()
	if turn.ID == "" {
		turn.ID = fmt.Sprintf("%s-%d", turn.SessionID, turn.TurnIndex)
	}
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = now
	}

	toolCallsJSON, err := sqlutil.FormatJSON(turn.ToolCalls)
	if err != nil {
		return SessionTurn{}, fmt.Errorf("sessions: format tool_calls: %w", err)
	}
	filesTouchedJSON, err := sqlutil.FormatJSON(turn.FilesTouched)
	if err != nil {
		return SessionTurn{}, fmt.Errorf("sessions: format files_touched: %w", err)
	}

	hasError := 0
	if turn.HasError {
		hasError = 1
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO session_turns (
	id, session_id, turn_index, role, content_preview, tool_calls, files_touched,
	has_error, error_type, error_message, resolution, tokens_used, timestamp, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	content_preview = excluded.content_preview,
	tool_calls = excluded.tool_calls,
	files_touched = excluded.files_touched,
	has_error = excluded.has_error,
	error_type = excluded.error_type,
	error_message = excluded.error_message,
	resolution = excluded.resolution,
	tokens_used = excluded.tokens_used
`,
		turn.ID, turn.SessionID, turn.TurnIndex, turn.Role, turn.ContentPreview,
		toolCallsJSON, filesTouchedJSON, hasError, turn.ErrorType, turn.ErrorMessage,
		turn.Resolution, turn.TokensUsed, sqlutil.FormatTimestamp(turn.Timestamp),
		sqlutil.FormatTimestamp(turn.CreatedAt),
	)
	if err != nil {
		return SessionTurn{}, fmt.Errorf("sessions: save turn: %w", err)
	}
	return turn, nil
}

// SaveTurns bulk inserts session turns.
func (s *Store) SaveTurns(ctx context.Context, turns []SessionTurn) error {
	if len(turns) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sessions: begin tx: %w", err)
	}
	defer func() {
		errs.Ignore(tx.Rollback(), "rollback save turns tx")
	}()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO session_turns (
	id, session_id, turn_index, role, content_preview, tool_calls, files_touched,
	has_error, error_type, error_message, resolution, tokens_used, timestamp, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	content_preview = excluded.content_preview,
	tool_calls = excluded.tool_calls,
	files_touched = excluded.files_touched,
	has_error = excluded.has_error,
	error_type = excluded.error_type,
	error_message = excluded.error_message,
	resolution = excluded.resolution,
	tokens_used = excluded.tokens_used
`)
	if err != nil {
		return fmt.Errorf("sessions: prepare turn insert: %w", err)
	}
	defer func() {
		errs.Ignore(stmt.Close(), "close turn insert stmt")
	}()

	now := timeutil.NowUTC()
	for _, turn := range turns {
		if turn.ID == "" {
			turn.ID = fmt.Sprintf("%s-%d", turn.SessionID, turn.TurnIndex)
		}
		if turn.CreatedAt.IsZero() {
			turn.CreatedAt = now
		}

		toolCallsJSON, err := sqlutil.FormatJSON(turn.ToolCalls)
		if err != nil {
			return fmt.Errorf("sessions: format tool_calls: %w", err)
		}
		filesTouchedJSON, err := sqlutil.FormatJSON(turn.FilesTouched)
		if err != nil {
			return fmt.Errorf("sessions: format files_touched: %w", err)
		}

		hasError := 0
		if turn.HasError {
			hasError = 1
		}

		_, err = stmt.ExecContext(ctx,
			turn.ID, turn.SessionID, turn.TurnIndex, turn.Role, turn.ContentPreview,
			toolCallsJSON, filesTouchedJSON, hasError, turn.ErrorType, turn.ErrorMessage,
			turn.Resolution, turn.TokensUsed, sqlutil.FormatTimestamp(turn.Timestamp),
			sqlutil.FormatTimestamp(turn.CreatedAt),
		)
		if err != nil {
			return fmt.Errorf("sessions: insert turn %s: %w", turn.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sessions: commit turns: %w", err)
	}
	return nil
}

// GetTurns retrieves turns for a session with optional filtering.
func (s *Store) GetTurns(ctx context.Context, sessionID string, opts TurnListOptions) ([]SessionTurn, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}

	var conditions []string
	var args []any

	conditions = append(conditions, "session_id = ?")
	args = append(args, sessionID)

	if opts.ErrorsOnly {
		conditions = append(conditions, "has_error = 1")
	}
	if opts.Role != "" {
		conditions = append(conditions, "role = ?")
		args = append(args, opts.Role)
	}

	query := `
		SELECT id, session_id, turn_index, role, content_preview, tool_calls, files_touched,
			has_error, error_type, error_message, resolution, tokens_used, timestamp, created_at
		FROM session_turns
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY turn_index ASC
		LIMIT ? OFFSET ?`
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sessions: get turns: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close get turns rows")
	}()

	return scanTurns(rows)
}

// GetTurnsWithErrors retrieves turns with errors for a session.
func (s *Store) GetTurnsWithErrors(ctx context.Context, sessionID string) ([]SessionTurn, error) {
	return s.GetTurns(ctx, sessionID, TurnListOptions{
		SessionID:  sessionID,
		ErrorsOnly: true,
		Limit:      100,
	})
}

// SearchTurns finds turns matching the query across all sessions.
func (s *Store) SearchTurns(ctx context.Context, query string, limit int) ([]SessionTurn, error) {
	if limit <= 0 {
		limit = 50
	}
	like := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, turn_index, role, content_preview, tool_calls, files_touched,
			has_error, error_type, error_message, resolution, tokens_used, timestamp, created_at
		FROM session_turns
		WHERE LOWER(content_preview) LIKE ?
			OR LOWER(error_message) LIKE ?
			OR LOWER(resolution) LIKE ?
		ORDER BY timestamp DESC
		LIMIT ?`, like, like, like, limit)
	if err != nil {
		return nil, fmt.Errorf("sessions: search turns: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close search turns rows")
	}()

	return scanTurns(rows)
}

// DeleteTurns removes all turns for a session.
func (s *Store) DeleteTurns(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_turns WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("sessions: delete turns: %w", err)
	}
	return nil
}

// SaveChunk inserts or updates a session chunk.
func (s *Store) SaveChunk(ctx context.Context, chunk SessionChunk) (SessionChunk, error) {
	now := timeutil.NowUTC()
	if chunk.CreatedAt.IsZero() {
		chunk.CreatedAt = now
	}

	toolsUsedJSON, err := sqlutil.FormatJSON(chunk.ToolsUsed)
	if err != nil {
		return SessionChunk{}, fmt.Errorf("sessions: format tools_used: %w", err)
	}
	filesTouchedJSON, err := sqlutil.FormatJSON(chunk.FilesTouched)
	if err != nil {
		return SessionChunk{}, fmt.Errorf("sessions: format files_touched: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO session_chunks (
	id, session_id, chunk_index, chunk_type, content_hash, content_preview,
	byte_offset, byte_length, tools_used, files_touched, has_error, error_type,
	context_window_index, embedding, embedding_model, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	chunk_type = excluded.chunk_type,
	content_hash = excluded.content_hash,
	content_preview = excluded.content_preview,
	byte_offset = excluded.byte_offset,
	byte_length = excluded.byte_length,
	tools_used = excluded.tools_used,
	files_touched = excluded.files_touched,
	has_error = excluded.has_error,
	error_type = excluded.error_type,
	context_window_index = excluded.context_window_index,
	embedding = excluded.embedding,
	embedding_model = excluded.embedding_model`,
		chunk.ID, chunk.SessionID, chunk.ChunkIndex, chunk.ChunkType, chunk.ContentHash,
		chunk.ContentPreview, chunk.ByteOffset, chunk.ByteLength, toolsUsedJSON, filesTouchedJSON,
		boolToInt(chunk.HasError), chunk.ErrorType, chunk.ContextWindowIndex, chunk.Embedding, chunk.EmbeddingModel,
		sqlutil.FormatTimestamp(chunk.CreatedAt),
	)
	if err != nil {
		return SessionChunk{}, fmt.Errorf("sessions: save chunk: %w", err)
	}
	return chunk, nil
}

// SaveChunks inserts multiple chunks in a batch.
func (s *Store) SaveChunks(ctx context.Context, chunks []SessionChunk) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sessions: begin chunk tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO session_chunks (
	id, session_id, chunk_index, chunk_type, content_hash, content_preview,
	byte_offset, byte_length, tools_used, files_touched, has_error, error_type,
	context_window_index, embedding, embedding_model, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	chunk_type = excluded.chunk_type,
	content_hash = excluded.content_hash,
	content_preview = excluded.content_preview,
	byte_offset = excluded.byte_offset,
	byte_length = excluded.byte_length,
	tools_used = excluded.tools_used,
	files_touched = excluded.files_touched,
	has_error = excluded.has_error,
	error_type = excluded.error_type,
	context_window_index = excluded.context_window_index,
	embedding = excluded.embedding,
	embedding_model = excluded.embedding_model`)
	if err != nil {
		return fmt.Errorf("sessions: prepare chunk stmt: %w", err)
	}
	defer stmt.Close()

	now := timeutil.NowUTC()
	for _, chunk := range chunks {
		if chunk.CreatedAt.IsZero() {
			chunk.CreatedAt = now
		}
		toolsUsedJSON, _ := sqlutil.FormatJSON(chunk.ToolsUsed)
		filesTouchedJSON, _ := sqlutil.FormatJSON(chunk.FilesTouched)

		_, err := stmt.ExecContext(ctx,
			chunk.ID, chunk.SessionID, chunk.ChunkIndex, chunk.ChunkType, chunk.ContentHash,
			chunk.ContentPreview, chunk.ByteOffset, chunk.ByteLength, toolsUsedJSON, filesTouchedJSON,
			boolToInt(chunk.HasError), chunk.ErrorType, chunk.ContextWindowIndex, chunk.Embedding, chunk.EmbeddingModel,
			sqlutil.FormatTimestamp(chunk.CreatedAt),
		)
		if err != nil {
			return fmt.Errorf("sessions: save chunk batch: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sessions: commit chunk tx: %w", err)
	}
	return nil
}

// GetChunks retrieves chunks for a session.
func (s *Store) GetChunks(ctx context.Context, sessionID string, limit int) ([]SessionChunk, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, chunk_index, chunk_type, content_hash, content_preview,
       byte_offset, byte_length, tools_used, files_touched, has_error, error_type,
       embedding, embedding_model, context_window_index, created_at
FROM session_chunks
WHERE session_id = ?
ORDER BY chunk_index ASC
LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("sessions: get chunks: %w", err)
	}
	defer rows.Close()

	return scanChunks(rows)
}

// GetChunk retrieves a specific chunk by index.
func (s *Store) GetChunk(ctx context.Context, sessionID string, chunkIndex int) (SessionChunk, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, session_id, chunk_index, chunk_type, content_hash, content_preview,
       byte_offset, byte_length, tools_used, files_touched, has_error, error_type,
       embedding, embedding_model, context_window_index, created_at
FROM session_chunks
WHERE session_id = ? AND chunk_index = ?`, sessionID, chunkIndex)

	return scanChunk(row)
}

// SearchChunks searches chunks by embedding similarity.
func (s *Store) SearchChunks(ctx context.Context, embedding []float32, limit int) ([]ScoredChunk, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, chunk_index, chunk_type, content_hash, content_preview,
       byte_offset, byte_length, tools_used, files_touched, has_error, error_type,
       embedding, embedding_model, context_window_index, created_at
FROM session_chunks
WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("sessions: search chunks: %w", err)
	}
	defer rows.Close()

	chunks, err := scanChunks(rows)
	if err != nil {
		return nil, err
	}

	// Calculate similarities and sort
	var scored []ScoredChunk
	for _, chunk := range chunks {
		if len(chunk.Embedding) == 0 {
			continue
		}
		chunkEmb := vector.DeserializeF32(chunk.Embedding)
		if len(chunkEmb) == 0 {
			continue
		}
		sim := vector.Cosine(embedding, chunkEmb)
		scored = append(scored, ScoredChunk{Chunk: chunk, Similarity: sim})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Similarity > scored[j].Similarity
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	return scored, nil
}

// DeleteChunks removes all chunks for a session.
func (s *Store) DeleteChunks(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_chunks WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("sessions: delete chunks: %w", err)
	}
	return nil
}

// SaveChunkSummary upserts a chunk-level summary record.
func (s *Store) SaveChunkSummary(ctx context.Context, summary SessionChunkSummary) (SessionChunkSummary, error) {
	if summary.ID == "" {
		return SessionChunkSummary{}, fmt.Errorf("sessions: save chunk summary: missing id")
	}
	if summary.SessionID == "" {
		return SessionChunkSummary{}, fmt.Errorf("sessions: save chunk summary: missing session_id")
	}
	if summary.Summary == "" {
		return SessionChunkSummary{}, fmt.Errorf("sessions: save chunk summary: missing summary")
	}

	now := timeutil.NowUTC()
	if summary.CreatedAt.IsZero() {
		summary.CreatedAt = now
	}
	summary.UpdatedAt = now
	if len(summary.ChunkIndices) > 0 {
		if min, max, ok := chunkIndexBounds(summary.ChunkIndices); ok {
			summary.ChunkIndexMin = min
			summary.ChunkIndexMax = max
		}
	}

	chunkIndicesJSON, err := sqlutil.FormatJSON(summary.ChunkIndices)
	if err != nil {
		return SessionChunkSummary{}, fmt.Errorf("sessions: format chunk_indices: %w", err)
	}
	toolsJSON, err := sqlutil.FormatJSON(summary.Tools)
	if err != nil {
		return SessionChunkSummary{}, fmt.Errorf("sessions: format tools: %w", err)
	}
	filesJSON, err := sqlutil.FormatJSON(summary.Files)
	if err != nil {
		return SessionChunkSummary{}, fmt.Errorf("sessions: format files: %w", err)
	}
	errorsJSON, err := sqlutil.FormatJSON(summary.Errors)
	if err != nil {
		return SessionChunkSummary{}, fmt.Errorf("sessions: format errors: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO session_chunk_summaries (
	id, session_id, window_index, trigger, chunk_indices, chunk_index_min, chunk_index_max, tools, files, errors,
	summary, summary_model, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	trigger = excluded.trigger,
	chunk_indices = excluded.chunk_indices,
	chunk_index_min = excluded.chunk_index_min,
	chunk_index_max = excluded.chunk_index_max,
	tools = excluded.tools,
	files = excluded.files,
	errors = excluded.errors,
	summary = excluded.summary,
	summary_model = excluded.summary_model,
	updated_at = excluded.updated_at`,
		summary.ID, summary.SessionID, summary.WindowIndex, summary.Trigger, chunkIndicesJSON,
		summary.ChunkIndexMin, summary.ChunkIndexMax, toolsJSON, filesJSON, errorsJSON,
		summary.Summary, summary.SummaryModel,
		sqlutil.FormatTimestamp(summary.CreatedAt), sqlutil.FormatTimestamp(summary.UpdatedAt),
	)
	if err != nil {
		return SessionChunkSummary{}, fmt.Errorf("sessions: save chunk summary: %w", err)
	}
	return summary, nil
}

// SaveChunkSummaries upserts multiple chunk-level summaries in a transaction.
func (s *Store) SaveChunkSummaries(ctx context.Context, summaries []SessionChunkSummary) error {
	if len(summaries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sessions: begin chunk summary tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO session_chunk_summaries (
	id, session_id, window_index, trigger, chunk_indices, chunk_index_min, chunk_index_max, tools, files, errors,
	summary, summary_model, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	trigger = excluded.trigger,
	chunk_indices = excluded.chunk_indices,
	chunk_index_min = excluded.chunk_index_min,
	chunk_index_max = excluded.chunk_index_max,
	tools = excluded.tools,
	files = excluded.files,
	errors = excluded.errors,
	summary = excluded.summary,
	summary_model = excluded.summary_model,
	updated_at = excluded.updated_at`)
	if err != nil {
		return fmt.Errorf("sessions: prepare chunk summary stmt: %w", err)
	}
	defer stmt.Close()

	now := timeutil.NowUTC()
	for _, summary := range summaries {
		if summary.ID == "" || summary.SessionID == "" || summary.Summary == "" {
			return fmt.Errorf("sessions: save chunk summaries: missing required fields")
		}
		if summary.CreatedAt.IsZero() {
			summary.CreatedAt = now
		}
		summary.UpdatedAt = now
		if len(summary.ChunkIndices) > 0 {
			if min, max, ok := chunkIndexBounds(summary.ChunkIndices); ok {
				summary.ChunkIndexMin = min
				summary.ChunkIndexMax = max
			}
		}

		chunkIndicesJSON, _ := sqlutil.FormatJSON(summary.ChunkIndices)
		toolsJSON, _ := sqlutil.FormatJSON(summary.Tools)
		filesJSON, _ := sqlutil.FormatJSON(summary.Files)
		errorsJSON, _ := sqlutil.FormatJSON(summary.Errors)

		_, err := stmt.ExecContext(ctx,
			summary.ID, summary.SessionID, summary.WindowIndex, summary.Trigger, chunkIndicesJSON,
			summary.ChunkIndexMin, summary.ChunkIndexMax, toolsJSON, filesJSON, errorsJSON,
			summary.Summary, summary.SummaryModel,
			sqlutil.FormatTimestamp(summary.CreatedAt), sqlutil.FormatTimestamp(summary.UpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("sessions: save chunk summary batch: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sessions: commit chunk summary tx: %w", err)
	}
	return nil
}

// GetChunkSummaries loads chunk summaries for a window.
func (s *Store) GetChunkSummaries(ctx context.Context, sessionID string, windowIndex int) ([]SessionChunkSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, window_index, trigger, chunk_indices, tools, files, errors,
       chunk_index_min, chunk_index_max, summary, summary_model, created_at, updated_at
FROM session_chunk_summaries
WHERE session_id = ? AND window_index = ?
ORDER BY created_at ASC`, sessionID, windowIndex)
	if err != nil {
		return nil, fmt.Errorf("sessions: get chunk summaries: %w", err)
	}
	defer rows.Close()

	return scanChunkSummaries(rows)
}

// GetChunkSummary loads a chunk summary by ID.
func (s *Store) GetChunkSummary(ctx context.Context, summaryID string) (SessionChunkSummary, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, session_id, window_index, trigger, chunk_indices, tools, files, errors,
       chunk_index_min, chunk_index_max, summary, summary_model, created_at, updated_at
FROM session_chunk_summaries
WHERE id = ?`, summaryID)

	return scanChunkSummary(row)
}

// DeleteChunkSummaries removes all chunk summaries for a session.
func (s *Store) DeleteChunkSummaries(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_chunk_summaries WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("sessions: delete chunk summaries: %w", err)
	}
	return nil
}

// SetArchivePath sets the archive path for a session.
func (s *Store) SetArchivePath(ctx context.Context, sessionID, archivePath string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET raw_jsonl_path = ? WHERE id = ?`, archivePath, sessionID)
	if err != nil {
		return fmt.Errorf("sessions: set archive path: %w", err)
	}
	return nil
}

// GetArchivePath retrieves the archive path for a session.
func (s *Store) GetArchivePath(ctx context.Context, sessionID string) (string, error) {
	var path sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT raw_jsonl_path FROM sessions WHERE id = ?`, sessionID).Scan(&path)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("sessions: get archive path: %w", err)
	}
	if !path.Valid {
		return "", nil
	}
	return path.String, nil
}

// --- Context Window Operations ---

// SaveContextWindow inserts or updates a context window.
func (s *Store) SaveContextWindow(ctx context.Context, window ContextWindow) (ContextWindow, error) {
	now := timeutil.NowUTC()
	if window.CreatedAt.IsZero() {
		window.CreatedAt = now
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO session_context_windows (
	id, session_id, window_index, started_at, ended_at, pre_compact_tokens,
	trigger, chunk_start, chunk_end, message_count, summary, embedding, embedding_model, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id, window_index) DO UPDATE SET
	started_at = excluded.started_at,
	ended_at = excluded.ended_at,
	pre_compact_tokens = excluded.pre_compact_tokens,
	trigger = excluded.trigger,
	chunk_start = excluded.chunk_start,
	chunk_end = excluded.chunk_end,
	message_count = excluded.message_count,
	summary = COALESCE(NULLIF(excluded.summary, ''), session_context_windows.summary),
	embedding = COALESCE(excluded.embedding, session_context_windows.embedding),
	embedding_model = COALESCE(NULLIF(excluded.embedding_model, ''), session_context_windows.embedding_model)`,
		window.ID, window.SessionID, window.WindowIndex,
		sqlutil.FormatTimestamp(window.StartedAt), sqlutil.FormatTimestamp(window.EndedAt),
		window.PreCompactTokens, window.Trigger, window.ChunkStart, window.ChunkEnd,
		window.MessageCount, window.Summary, window.Embedding, window.EmbeddingModel,
		sqlutil.FormatTimestamp(window.CreatedAt),
	)
	if err != nil {
		return ContextWindow{}, fmt.Errorf("sessions: save context window: %w", err)
	}
	return window, nil
}

// SaveContextWindows inserts multiple context windows in a batch.
func (s *Store) SaveContextWindows(ctx context.Context, windows []ContextWindow) error {
	if len(windows) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sessions: begin window tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO session_context_windows (
	id, session_id, window_index, started_at, ended_at, pre_compact_tokens,
	trigger, chunk_start, chunk_end, message_count, summary, embedding, embedding_model, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id, window_index) DO UPDATE SET
	started_at = excluded.started_at,
	ended_at = excluded.ended_at,
	pre_compact_tokens = excluded.pre_compact_tokens,
	trigger = excluded.trigger,
	chunk_start = excluded.chunk_start,
	chunk_end = excluded.chunk_end,
	message_count = excluded.message_count,
	summary = COALESCE(NULLIF(excluded.summary, ''), session_context_windows.summary),
	embedding = COALESCE(excluded.embedding, session_context_windows.embedding),
	embedding_model = COALESCE(NULLIF(excluded.embedding_model, ''), session_context_windows.embedding_model)`)
	if err != nil {
		return fmt.Errorf("sessions: prepare window stmt: %w", err)
	}
	defer stmt.Close()

	now := timeutil.NowUTC()
	for _, window := range windows {
		if window.CreatedAt.IsZero() {
			window.CreatedAt = now
		}

		_, err := stmt.ExecContext(ctx,
			window.ID, window.SessionID, window.WindowIndex,
			sqlutil.FormatTimestamp(window.StartedAt), sqlutil.FormatTimestamp(window.EndedAt),
			window.PreCompactTokens, window.Trigger, window.ChunkStart, window.ChunkEnd,
			window.MessageCount, window.Summary, window.Embedding, window.EmbeddingModel,
			sqlutil.FormatTimestamp(window.CreatedAt),
		)
		if err != nil {
			return fmt.Errorf("sessions: save window batch: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sessions: commit window tx: %w", err)
	}
	return nil
}

// GetContextWindows retrieves all context windows for a session.
func (s *Store) GetContextWindows(ctx context.Context, sessionID string) ([]ContextWindow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, window_index, started_at, ended_at, pre_compact_tokens,
       trigger, chunk_start, chunk_end, message_count, summary, embedding, embedding_model, created_at
FROM session_context_windows
WHERE session_id = ?
ORDER BY window_index ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("sessions: get context windows: %w", err)
	}
	defer rows.Close()

	return scanContextWindows(rows)
}

// GetContextWindow retrieves a specific context window by session and index.
func (s *Store) GetContextWindow(ctx context.Context, sessionID string, windowIndex int) (ContextWindow, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, session_id, window_index, started_at, ended_at, pre_compact_tokens,
       trigger, chunk_start, chunk_end, message_count, summary, embedding, embedding_model, created_at
FROM session_context_windows
WHERE session_id = ? AND window_index = ?`, sessionID, windowIndex)

	return scanContextWindow(row)
}

// UpdateWindowSummary updates the summary and embedding for a context window.
// Nil/empty values are treated as "no change" to preserve existing data.
//
// Deprecated: Use UpdateContextWindowSummary or SetContextWindowEmbedding instead
// for clearer intent and to prevent accidental overwrites.
func (s *Store) UpdateWindowSummary(ctx context.Context, windowID string, summary string, embedding []byte, model string) error {
	// Normalize: treat empty slice as nil so COALESCE works correctly
	if len(embedding) == 0 {
		embedding = nil
	}
	summary = strings.TrimSpace(summary)
	model = strings.TrimSpace(model)

	result, err := s.db.ExecContext(ctx, `
UPDATE session_context_windows SET
	summary = COALESCE(NULLIF(?, ''), summary),
	embedding = COALESCE(?, embedding),
	embedding_model = COALESCE(NULLIF(?, ''), embedding_model)
WHERE id = ?`, summary, embedding, model, windowID)
	if err != nil {
		return fmt.Errorf("sessions: update window summary: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sessions: rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateContextWindowSummary updates only the summary text for a context window.
// Use this when generating summaries without affecting embeddings.
func (s *Store) UpdateContextWindowSummary(ctx context.Context, windowID string, summary string) error {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil // No-op for empty summary
	}

	result, err := s.db.ExecContext(ctx, `
UPDATE session_context_windows SET summary = ?
WHERE id = ?`, summary, windowID)
	if err != nil {
		return fmt.Errorf("sessions: update context window summary: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sessions: rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetContextWindowEmbedding sets the embedding and model for a context window.
// Use this when generating embeddings without affecting the summary.
func (s *Store) SetContextWindowEmbedding(ctx context.Context, windowID string, embedding []byte, model string) error {
	if len(embedding) == 0 {
		return nil // No-op for empty embedding
	}
	model = strings.TrimSpace(model)

	result, err := s.db.ExecContext(ctx, `
UPDATE session_context_windows SET embedding = ?, embedding_model = ?
WHERE id = ?`, embedding, model, windowID)
	if err != nil {
		return fmt.Errorf("sessions: set context window embedding: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sessions: rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// SearchContextWindows searches context windows by embedding similarity.
func (s *Store) SearchContextWindows(ctx context.Context, queryEmbedding []float32, limit int) ([]ScoredContextWindow, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, window_index, started_at, ended_at, pre_compact_tokens,
       trigger, chunk_start, chunk_end, message_count, summary, embedding, embedding_model, created_at
FROM session_context_windows
WHERE embedding IS NOT NULL AND LENGTH(embedding) > 0`)
	if err != nil {
		return nil, fmt.Errorf("sessions: search context windows: %w", err)
	}
	defer rows.Close()

	windows, err := scanContextWindows(rows)
	if err != nil {
		return nil, err
	}

	// Calculate similarities and sort
	var scored []ScoredContextWindow
	for _, window := range windows {
		if len(window.Embedding) == 0 {
			continue
		}
		windowEmb := vector.DeserializeF32(window.Embedding)
		if len(windowEmb) == 0 {
			continue
		}
		sim := vector.Cosine(queryEmbedding, windowEmb)
		scored = append(scored, ScoredContextWindow{Window: window, Similarity: sim})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Similarity > scored[j].Similarity
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	return scored, nil
}

// DeleteContextWindows removes all context windows for a session.
func (s *Store) DeleteContextWindows(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_context_windows WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("sessions: delete context windows: %w", err)
	}
	return nil
}

// scanContextWindow scans a single context window from a row.
func scanContextWindow(row scannable) (ContextWindow, error) {
	var window ContextWindow
	var startedAt, endedAt, createdAt sql.NullString
	var trigger, summary, embeddingModel sql.NullString
	var chunkStart, chunkEnd, messageCount sql.NullInt64

	err := row.Scan(
		&window.ID, &window.SessionID, &window.WindowIndex,
		&startedAt, &endedAt, &window.PreCompactTokens,
		&trigger, &chunkStart, &chunkEnd, &messageCount,
		&summary, &window.Embedding, &embeddingModel, &createdAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return ContextWindow{}, ErrNotFound
		}
		return ContextWindow{}, fmt.Errorf("sessions: scan context window: %w", err)
	}

	if startedAt.Valid {
		ts, _ := sqlutil.ScanTimestamp(startedAt.String)
		window.StartedAt = ts
	}
	if endedAt.Valid {
		ts, _ := sqlutil.ScanTimestamp(endedAt.String)
		window.EndedAt = ts
	}
	if createdAt.Valid {
		ts, _ := sqlutil.ScanTimestamp(createdAt.String)
		window.CreatedAt = ts
	}
	if trigger.Valid {
		window.Trigger = trigger.String
	}
	if summary.Valid {
		window.Summary = summary.String
	}
	if embeddingModel.Valid {
		window.EmbeddingModel = embeddingModel.String
	}
	if chunkStart.Valid {
		window.ChunkStart = int(chunkStart.Int64)
	}
	if chunkEnd.Valid {
		window.ChunkEnd = int(chunkEnd.Int64)
	}
	if messageCount.Valid {
		window.MessageCount = int(messageCount.Int64)
	}

	return window, nil
}

// scanContextWindows scans multiple context windows from rows.
func scanContextWindows(rows *sql.Rows) ([]ContextWindow, error) {
	var windows []ContextWindow
	for rows.Next() {
		window, err := scanContextWindow(rows)
		if err != nil {
			return nil, err
		}
		windows = append(windows, window)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sessions: context windows rows error: %w", err)
	}
	return windows, nil
}

// EmbeddingMetadata tracks embedding configuration for sessions.
type EmbeddingMetadata struct {
	TableName  string    `json:"table_name"`
	ColumnName string    `json:"column_name"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	Dimensions int       `json:"dimensions"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// GetEmbeddingMetadata retrieves embedding metadata for sessions.
// Returns nil if no metadata exists (embeddings never stored).
func (s *Store) GetEmbeddingMetadata(ctx context.Context) (*EmbeddingMetadata, error) {
	var meta EmbeddingMetadata
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT table_name, column_name, provider, model, dimensions, created_at, updated_at
		FROM embedding_metadata WHERE table_name = 'sessions'
	`).Scan(&meta.TableName, &meta.ColumnName, &meta.Provider, &meta.Model, &meta.Dimensions, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No metadata yet
		}
		return nil, fmt.Errorf("sessions: get embedding metadata: %w", err)
	}

	meta.CreatedAt, _ = sqlutil.ScanTimestamp(createdAt)
	meta.UpdatedAt, _ = sqlutil.ScanTimestamp(updatedAt)

	return &meta, nil
}

// SetEmbeddingMetadata stores or updates embedding metadata for sessions.
func (s *Store) SetEmbeddingMetadata(ctx context.Context, meta EmbeddingMetadata) error {
	now := timeutil.NowUTC()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO embedding_metadata (table_name, column_name, provider, model, dimensions, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(table_name) DO UPDATE SET
			column_name = excluded.column_name,
			provider = excluded.provider,
			model = excluded.model,
			dimensions = excluded.dimensions,
			updated_at = excluded.updated_at
	`, meta.TableName, meta.ColumnName, meta.Provider, meta.Model, meta.Dimensions,
		sqlutil.FormatTimestamp(meta.CreatedAt), sqlutil.FormatTimestamp(meta.UpdatedAt))
	if err != nil {
		return fmt.Errorf("sessions: set embedding metadata: %w", err)
	}
	return nil
}

// ValidateDimensions checks if stored embedding dimensions match the expected value.
// Returns an error with reindex guidance if there's a mismatch.
// Returns nil if no metadata exists (no embeddings stored yet) or if dimensions match.
func (s *Store) ValidateDimensions(ctx context.Context, expectedDims int) error {
	meta, err := s.GetEmbeddingMetadata(ctx)
	if err != nil {
		return err
	}
	if meta == nil {
		// No embeddings stored yet, nothing to validate
		return nil
	}

	if meta.Dimensions != expectedDims {
		return fmt.Errorf("sessions: embedding dimension mismatch: stored %d, config expects %d; "+
			"delete sessions.db to rebuild with new dimensions or update embedding.dimensions in config.yaml",
			meta.Dimensions, expectedDims)
	}
	return nil
}

// validateDimensionsOnOpen loads config and validates stored embedding dimensions.
// Logs a warning but does not fail if validation encounters issues (non-blocking).
func (s *Store) validateDimensionsOnOpen(ctx context.Context) {
	cfg, err := config.Load(ctx)
	if err != nil {
		log.Printf("[WARN] sessions: failed to load config for dimension validation: %v", err)
		return
	}

	expectedDims := cfg.Embedding.Dimensions
	if expectedDims == 0 {
		expectedDims = dbdriver.GetDefaultVectorDimensions()
	}

	if err := s.ValidateDimensions(ctx, expectedDims); err != nil {
		log.Printf("[WARN] %v", err)
	}
}

func scanChunk(row scannable) (SessionChunk, error) {
	var chunk SessionChunk
	var createdAt sql.NullString
	var contentPreview, toolsUsed, filesTouched sql.NullString
	var errorType sql.NullString
	var hasError int
	var embeddingModel sql.NullString
	var contextWindowIndex sql.NullInt64

	err := row.Scan(
		&chunk.ID, &chunk.SessionID, &chunk.ChunkIndex, &chunk.ChunkType, &chunk.ContentHash,
		&contentPreview, &chunk.ByteOffset, &chunk.ByteLength, &toolsUsed, &filesTouched,
		&hasError, &errorType, &chunk.Embedding, &embeddingModel, &contextWindowIndex, &createdAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return SessionChunk{}, ErrNotFound
		}
		return SessionChunk{}, fmt.Errorf("sessions: scan chunk: %w", err)
	}

	chunk.HasError = hasError == 1

	if contentPreview.Valid {
		chunk.ContentPreview = contentPreview.String
	}
	if errorType.Valid {
		chunk.ErrorType = errorType.String
	}
	if embeddingModel.Valid {
		chunk.EmbeddingModel = embeddingModel.String
	}
	if contextWindowIndex.Valid {
		chunk.ContextWindowIndex = int(contextWindowIndex.Int64)
	}

	if createdAt.Valid {
		ts, err := sqlutil.ScanTimestamp(createdAt.String)
		errs.Ignore(err, "parse chunk created_at")
		chunk.CreatedAt = ts
	}

	if toolsUsed.Valid {
		errs.Ignore(sqlutil.ScanJSON(toolsUsed.String, &chunk.ToolsUsed), "parse tools_used JSON")
	}
	if filesTouched.Valid {
		errs.Ignore(sqlutil.ScanJSON(filesTouched.String, &chunk.FilesTouched), "parse files_touched JSON")
	}

	return chunk, nil
}

func scanChunkSummary(row scannable) (SessionChunkSummary, error) {
	var summary SessionChunkSummary
	var trigger, chunkIndices, tools, files, errors sql.NullString
	var chunkIndexMin, chunkIndexMax sql.NullInt64
	var summaryModel sql.NullString
	var createdAt, updatedAt sql.NullString

	err := row.Scan(
		&summary.ID, &summary.SessionID, &summary.WindowIndex, &trigger, &chunkIndices,
		&tools, &files, &errors, &chunkIndexMin, &chunkIndexMax, &summary.Summary,
		&summaryModel, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return SessionChunkSummary{}, ErrNotFound
		}
		return SessionChunkSummary{}, fmt.Errorf("sessions: scan chunk summary: %w", err)
	}

	if trigger.Valid {
		summary.Trigger = trigger.String
	}
	if summaryModel.Valid {
		summary.SummaryModel = summaryModel.String
	}
	if createdAt.Valid {
		ts, err := sqlutil.ScanTimestamp(createdAt.String)
		errs.Ignore(err, "parse chunk summary created_at")
		summary.CreatedAt = ts
	}
	if updatedAt.Valid {
		ts, err := sqlutil.ScanTimestamp(updatedAt.String)
		errs.Ignore(err, "parse chunk summary updated_at")
		summary.UpdatedAt = ts
	}

	if chunkIndices.Valid {
		errs.Ignore(sqlutil.ScanJSON(chunkIndices.String, &summary.ChunkIndices), "parse chunk_indices JSON")
	}
	if chunkIndexMin.Valid {
		summary.ChunkIndexMin = int(chunkIndexMin.Int64)
	}
	if chunkIndexMax.Valid {
		summary.ChunkIndexMax = int(chunkIndexMax.Int64)
	}
	if (!chunkIndexMin.Valid || !chunkIndexMax.Valid) && len(summary.ChunkIndices) > 0 {
		if min, max, ok := chunkIndexBounds(summary.ChunkIndices); ok {
			if !chunkIndexMin.Valid {
				summary.ChunkIndexMin = min
			}
			if !chunkIndexMax.Valid {
				summary.ChunkIndexMax = max
			}
		}
	}
	if tools.Valid {
		errs.Ignore(sqlutil.ScanJSON(tools.String, &summary.Tools), "parse tools JSON")
	}
	if files.Valid {
		errs.Ignore(sqlutil.ScanJSON(files.String, &summary.Files), "parse files JSON")
	}
	if errors.Valid {
		errs.Ignore(sqlutil.ScanJSON(errors.String, &summary.Errors), "parse errors JSON")
	}

	return summary, nil
}

func chunkIndexBounds(indices []int) (min int, max int, ok bool) {
	if len(indices) == 0 {
		return 0, 0, false
	}
	min = indices[0]
	max = indices[0]
	for _, idx := range indices[1:] {
		if idx < min {
			min = idx
		}
		if idx > max {
			max = idx
		}
	}
	return min, max, true
}

func scanChunks(rows *sql.Rows) ([]SessionChunk, error) {
	var chunks []SessionChunk
	for rows.Next() {
		chunk, err := scanChunk(rows)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sessions: chunks rows error: %w", err)
	}
	return chunks, nil
}

func scanChunkSummaries(rows *sql.Rows) ([]SessionChunkSummary, error) {
	var summaries []SessionChunkSummary
	for rows.Next() {
		summary, err := scanChunkSummary(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sessions: chunk summaries rows error: %w", err)
	}
	return summaries, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func scanTurn(row scannable) (SessionTurn, error) {
	var turn SessionTurn
	var timestamp, createdAt sql.NullString
	var contentPreview, toolCalls, filesTouched sql.NullString
	var errorType, errorMessage, resolution sql.NullString
	var hasError int

	err := row.Scan(
		&turn.ID, &turn.SessionID, &turn.TurnIndex, &turn.Role, &contentPreview,
		&toolCalls, &filesTouched, &hasError, &errorType, &errorMessage,
		&resolution, &turn.TokensUsed, &timestamp, &createdAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return SessionTurn{}, ErrNotFound
		}
		return SessionTurn{}, fmt.Errorf("sessions: scan turn: %w", err)
	}

	turn.HasError = hasError == 1

	if contentPreview.Valid {
		turn.ContentPreview = contentPreview.String
	}
	if errorType.Valid {
		turn.ErrorType = errorType.String
	}
	if errorMessage.Valid {
		turn.ErrorMessage = errorMessage.String
	}
	if resolution.Valid {
		turn.Resolution = resolution.String
	}

	if timestamp.Valid {
		ts, err := sqlutil.ScanTimestamp(timestamp.String)
		errs.Ignore(err, "parse turn timestamp")
		turn.Timestamp = ts
	}
	if createdAt.Valid {
		ts, err := sqlutil.ScanTimestamp(createdAt.String)
		errs.Ignore(err, "parse turn created_at")
		turn.CreatedAt = ts
	}

	if toolCalls.Valid {
		errs.Ignore(sqlutil.ScanJSON(toolCalls.String, &turn.ToolCalls), "parse tool_calls JSON")
	}
	if filesTouched.Valid {
		errs.Ignore(sqlutil.ScanJSON(filesTouched.String, &turn.FilesTouched), "parse files_touched JSON")
	}

	return turn, nil
}

func scanTurns(rows *sql.Rows) ([]SessionTurn, error) {
	var turns []SessionTurn
	for rows.Next() {
		turn, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sessions: turns rows error: %w", err)
	}
	return turns, nil
}

// ErrNotFound indicates a session was not found.
var ErrNotFound = fmt.Errorf("sessions: not found")

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	workspace_path TEXT NOT NULL,
	project_name TEXT,
	git_branch TEXT,
	claude_version TEXT,
	started_at TEXT,
	ended_at TEXT,
	summary TEXT,
	accomplished TEXT,
	decisions TEXT,
	gotchas TEXT,
	user_insights TEXT,
	tags TEXT,
	key_files TEXT,
	tools_pattern TEXT,
	message_count INTEGER DEFAULT 0,
	user_turns INTEGER DEFAULT 0,
	tool_invocations INTEGER DEFAULT 0,
	total_tokens INTEGER DEFAULT 0,
	raw_jsonl_path TEXT,
	embedding BLOB,
	embedding_model TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	-- Lineage fields
	parent_session_id TEXT,
	agent_id TEXT NOT NULL DEFAULT 'agentctl',
	agent_type TEXT NOT NULL DEFAULT 'claude',
	status TEXT NOT NULL DEFAULT 'ok',
	-- Post-compact restore tracking
	pending_restore_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_sessions_workspace ON sessions(workspace_path);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_name);
CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_tags ON sessions(tags);

-- Session turns table for fine-grained retrieval
CREATE TABLE IF NOT EXISTS session_turns (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	turn_index INTEGER NOT NULL,
	role TEXT NOT NULL,
	content_preview TEXT,
	tool_calls TEXT,
	files_touched TEXT,
	has_error INTEGER DEFAULT 0,
	error_type TEXT,
	error_message TEXT,
	resolution TEXT,
	tokens_used INTEGER DEFAULT 0,
	timestamp TEXT NOT NULL,
	created_at TEXT NOT NULL,
	FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_turns_session ON session_turns(session_id);
CREATE INDEX IF NOT EXISTS idx_turns_session_index ON session_turns(session_id, turn_index);
CREATE INDEX IF NOT EXISTS idx_turns_error ON session_turns(session_id) WHERE has_error = 1;
CREATE INDEX IF NOT EXISTS idx_turns_role ON session_turns(session_id, role);

-- Session chunks table for JSONL archive deep retrieval
CREATE TABLE IF NOT EXISTS session_chunks (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	chunk_index INTEGER NOT NULL,
	chunk_type TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	content_preview TEXT,
	byte_offset INTEGER NOT NULL,
	byte_length INTEGER NOT NULL,
	tools_used TEXT,
	files_touched TEXT,
	has_error INTEGER DEFAULT 0,
	error_type TEXT,
	embedding BLOB,
	embedding_model TEXT,
	created_at TEXT NOT NULL,
	FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_chunks_session ON session_chunks(session_id);
CREATE INDEX IF NOT EXISTS idx_chunks_session_index ON session_chunks(session_id, chunk_index);
CREATE INDEX IF NOT EXISTS idx_chunks_error ON session_chunks(session_id) WHERE has_error = 1;
CREATE INDEX IF NOT EXISTS idx_chunks_hash ON session_chunks(content_hash);

-- Session chunk summaries table for persisted chunk-level summaries
CREATE TABLE IF NOT EXISTS session_chunk_summaries (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	window_index INTEGER NOT NULL,
	trigger TEXT,
	chunk_indices TEXT NOT NULL,
	chunk_index_min INTEGER,
	chunk_index_max INTEGER,
	tools TEXT,
	files TEXT,
	errors TEXT,
	summary TEXT NOT NULL,
	summary_model TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
	UNIQUE(session_id, window_index, chunk_indices)
);

CREATE INDEX IF NOT EXISTS idx_chunk_summaries_session ON session_chunk_summaries(session_id);
CREATE INDEX IF NOT EXISTS idx_chunk_summaries_window ON session_chunk_summaries(session_id, window_index);

-- Session edges table for lineage relationships
CREATE TABLE IF NOT EXISTS session_edges (
	id TEXT PRIMARY KEY,
	workspace TEXT NOT NULL,
	from_session TEXT NOT NULL,
	to_session TEXT NOT NULL,
	edge_type TEXT NOT NULL,
	created_at TEXT NOT NULL,
	metadata TEXT,
	FOREIGN KEY (from_session) REFERENCES sessions(id) ON DELETE CASCADE,
	FOREIGN KEY (to_session) REFERENCES sessions(id) ON DELETE CASCADE,
	UNIQUE(from_session, to_session, edge_type)
);

CREATE INDEX IF NOT EXISTS idx_session_edges_from ON session_edges(from_session);
CREATE INDEX IF NOT EXISTS idx_session_edges_to ON session_edges(to_session);
CREATE INDEX IF NOT EXISTS idx_session_edges_workspace ON session_edges(workspace);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("sessions: migrate: %w", err)
	}

	// Add user_insights column if it doesn't exist (for existing databases)
	var colCount int
	row := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'user_insights'")
	if err := row.Scan(&colCount); err == nil && colCount == 0 {
		if _, err := db.ExecContext(ctx, "ALTER TABLE sessions ADD COLUMN user_insights TEXT"); err != nil {
			// Ignore error if column already exists
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("sessions: add user_insights column: %w", err)
			}
		}
	}

	// Add key_questions column if it doesn't exist (for existing databases)
	row = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'key_questions'")
	if err := row.Scan(&colCount); err == nil && colCount == 0 {
		if _, err := db.ExecContext(ctx, "ALTER TABLE sessions ADD COLUMN key_questions TEXT"); err != nil {
			// Ignore error if column already exists
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("sessions: add key_questions column: %w", err)
			}
		}
	}

	// Add lineage columns for existing databases
	lineageColumns := []struct {
		name       string
		ddl        string
		hasDefault bool
	}{
		{"parent_session_id", "ALTER TABLE sessions ADD COLUMN parent_session_id TEXT", false},
		{"agent_id", "ALTER TABLE sessions ADD COLUMN agent_id TEXT NOT NULL DEFAULT 'agentctl'", true},
		{"agent_type", "ALTER TABLE sessions ADD COLUMN agent_type TEXT NOT NULL DEFAULT 'claude'", true},
		{"status", "ALTER TABLE sessions ADD COLUMN status TEXT NOT NULL DEFAULT 'ok'", true},
	}
	for _, col := range lineageColumns {
		row := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = ?", col.name)
		if err := row.Scan(&colCount); err == nil && colCount == 0 {
			if _, err := db.ExecContext(ctx, col.ddl); err != nil {
				if !strings.Contains(err.Error(), "duplicate column") {
					return fmt.Errorf("sessions: add %s column: %w", col.name, err)
				}
			}
		}
	}

	// Create indexes for lineage columns (after columns are added)
	lineageIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_sessions_parent ON sessions(parent_session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_workspace_agent ON sessions(workspace_path, agent_id, started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status)`,
	}
	for _, idx := range lineageIndexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			// Ignore if index already exists
			if !strings.Contains(err.Error(), "already exists") {
				log.Printf("sessions: warning: failed to create lineage index: %v", err)
			}
		}
	}

	// Create embedding_metadata table to track provider/model/dimensions
	// This enables detection of dimension mismatches if embedding models change
	metadataDDL := `
CREATE TABLE IF NOT EXISTS embedding_metadata (
	table_name TEXT PRIMARY KEY,
	column_name TEXT NOT NULL,
	provider TEXT NOT NULL,
	model TEXT NOT NULL,
	dimensions INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`
	if _, err := db.ExecContext(ctx, metadataDDL); err != nil {
		return fmt.Errorf("sessions: create embedding_metadata: %w", err)
	}

	// Create context_windows table for granular sub-session retrieval
	// Each window represents work between compaction boundaries
	contextWindowsDDL := `
CREATE TABLE IF NOT EXISTS session_context_windows (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	window_index INTEGER NOT NULL,
	started_at TEXT,
	ended_at TEXT,
	pre_compact_tokens INTEGER DEFAULT 0,
	trigger TEXT,
	chunk_start INTEGER,
	chunk_end INTEGER,
	message_count INTEGER DEFAULT 0,
	summary TEXT,
	embedding BLOB,
	embedding_model TEXT,
	created_at TEXT NOT NULL,
	FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
	UNIQUE(session_id, window_index)
);

CREATE INDEX IF NOT EXISTS idx_context_windows_session ON session_context_windows(session_id);
CREATE INDEX IF NOT EXISTS idx_context_windows_ended ON session_context_windows(ended_at DESC);
`
	if _, err := db.ExecContext(ctx, contextWindowsDDL); err != nil {
		return fmt.Errorf("sessions: create context_windows: %w", err)
	}

	// Add context_window_index column to session_chunks if it doesn't exist
	var cwColCount int
	row = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('session_chunks') WHERE name = 'context_window_index'")
	if err := row.Scan(&cwColCount); err == nil && cwColCount == 0 {
		if _, err := db.ExecContext(ctx, "ALTER TABLE session_chunks ADD COLUMN context_window_index INTEGER DEFAULT 0"); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("sessions: add context_window_index column: %w", err)
			}
		}
	}

	// Add chunk_index_min/max columns for chunk summaries if missing (existing databases)
	var chunkSummaryColCount int
	row = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('session_chunk_summaries') WHERE name = 'chunk_index_min'")
	if err := row.Scan(&chunkSummaryColCount); err == nil && chunkSummaryColCount == 0 {
		if _, err := db.ExecContext(ctx, "ALTER TABLE session_chunk_summaries ADD COLUMN chunk_index_min INTEGER"); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("sessions: add chunk_index_min column: %w", err)
			}
		}
	}
	row = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('session_chunk_summaries') WHERE name = 'chunk_index_max'")
	if err := row.Scan(&chunkSummaryColCount); err == nil && chunkSummaryColCount == 0 {
		if _, err := db.ExecContext(ctx, "ALTER TABLE session_chunk_summaries ADD COLUMN chunk_index_max INTEGER"); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("sessions: add chunk_index_max column: %w", err)
			}
		}
	}
	if _, err := db.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_chunk_summaries_range ON session_chunk_summaries(session_id, window_index, chunk_index_min, chunk_index_max)"); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			log.Printf("sessions: warning: failed to create chunk summaries range index: %v", err)
		}
	}

	// Add content_hash column for conversation content deduplication (forked sessions)
	var contentHashColCount int
	row = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'content_hash'")
	if err := row.Scan(&contentHashColCount); err == nil && contentHashColCount == 0 {
		if _, err := db.ExecContext(ctx, "ALTER TABLE sessions ADD COLUMN content_hash TEXT"); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("sessions: add content_hash column: %w", err)
			}
		}
	}
	if _, err := db.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_sessions_content_hash ON sessions(content_hash)"); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			log.Printf("sessions: warning: failed to create content_hash index: %v", err)
		}
	}

	// Add pending_restore_at column for post-compact context injection
	var pendingRestoreColCount int
	row = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'pending_restore_at'")
	if err := row.Scan(&pendingRestoreColCount); err == nil && pendingRestoreColCount == 0 {
		if _, err := db.ExecContext(ctx, "ALTER TABLE sessions ADD COLUMN pending_restore_at TEXT"); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("sessions: add pending_restore_at column: %w", err)
			}
		}
	}
	if _, err := db.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_sessions_pending_restore ON sessions(workspace_path, pending_restore_at) WHERE pending_restore_at IS NOT NULL"); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			log.Printf("sessions: warning: failed to create pending_restore index: %v", err)
		}
	}

	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanSession(row scannable) (Session, error) {
	var session Session
	var startedAt, endedAt, createdAt, updatedAt sql.NullString
	var summary, accomplished, decisions, gotchas, userInsights, tags, keyFiles sql.NullString
	var toolsPattern sql.NullString
	var embedding []byte
	var embeddingModel sql.NullString
	var parentSessionID, agentID, agentType, status sql.NullString
	var keyQuestions sql.NullString
	var contentHash sql.NullString
	// Nullable string fields that might be NULL in database
	var projectName, gitBranch, claudeVersion, rawJSONLPath sql.NullString

	err := row.Scan(
		&session.ID, &session.WorkspacePath, &projectName, &gitBranch, &claudeVersion,
		&startedAt, &endedAt, &summary, &accomplished, &decisions, &gotchas, &userInsights,
		&tags, &keyFiles, &toolsPattern, &session.MessageCount, &session.UserTurns,
		&session.ToolInvocations, &session.TotalTokens, &rawJSONLPath, &contentHash, &embedding, &embeddingModel,
		&createdAt, &updatedAt, &parentSessionID, &agentID, &agentType, &status, &keyQuestions,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Session{}, ErrNotFound
		}
		return Session{}, fmt.Errorf("sessions: scan: %w", err)
	}

	// Parse timestamps (errors are non-critical for optional fields)
	if startedAt.Valid {
		ts, err := sqlutil.ScanTimestamp(startedAt.String)
		errs.Ignore(err, "parse started_at timestamp")
		session.StartedAt = ts
	}
	if endedAt.Valid {
		ts, err := sqlutil.ScanTimestamp(endedAt.String)
		errs.Ignore(err, "parse ended_at timestamp")
		session.EndedAt = ts
	}
	if createdAt.Valid {
		ts, err := sqlutil.ScanTimestamp(createdAt.String)
		errs.Ignore(err, "parse created_at timestamp")
		session.CreatedAt = ts
	}
	if updatedAt.Valid {
		ts, err := sqlutil.ScanTimestamp(updatedAt.String)
		errs.Ignore(err, "parse updated_at timestamp")
		session.UpdatedAt = ts
	}

	// Assign nullable strings
	if projectName.Valid {
		session.ProjectName = projectName.String
	}
	if summary.Valid {
		session.Summary = summary.String
	}
	if toolsPattern.Valid {
		session.ToolsPattern = toolsPattern.String
	}

	// Parse JSON arrays (errors are non-critical for optional fields)
	if accomplished.Valid {
		errs.Ignore(sqlutil.ScanJSON(accomplished.String, &session.Accomplished), "parse accomplished JSON")
	}
	if decisions.Valid {
		errs.Ignore(sqlutil.ScanJSON(decisions.String, &session.Decisions), "parse decisions JSON")
	}
	if gotchas.Valid {
		errs.Ignore(sqlutil.ScanJSON(gotchas.String, &session.Gotchas), "parse gotchas JSON")
	}
	if userInsights.Valid {
		errs.Ignore(sqlutil.ScanJSON(userInsights.String, &session.UserInsights), "parse userInsights JSON")
	}
	if tags.Valid {
		errs.Ignore(sqlutil.ScanJSON(tags.String, &session.Tags), "parse tags JSON")
	}
	if keyFiles.Valid {
		errs.Ignore(sqlutil.ScanJSON(keyFiles.String, &session.KeyFiles), "parse keyFiles JSON")
	}
	if keyQuestions.Valid {
		errs.Ignore(sqlutil.ScanJSON(keyQuestions.String, &session.KeyQuestions), "parse keyQuestions JSON")
	}

	session.Embedding = embedding
	if embeddingModel.Valid {
		session.EmbeddingModel = embeddingModel.String
	}
	if contentHash.Valid {
		session.ContentHash = contentHash.String
	}

	// Nullable basic fields
	if projectName.Valid {
		session.ProjectName = projectName.String
	}
	if gitBranch.Valid {
		session.GitBranch = gitBranch.String
	}
	if claudeVersion.Valid {
		session.ClaudeVersion = claudeVersion.String
	}
	if rawJSONLPath.Valid {
		session.RawJSONLPath = rawJSONLPath.String
	}

	// Lineage fields
	if parentSessionID.Valid {
		session.ParentSessionID = parentSessionID.String
	}
	if agentID.Valid {
		session.AgentID = agentID.String
	} else {
		session.AgentID = "agentctl" // default
	}
	if agentType.Valid {
		session.AgentType = agentType.String
	} else {
		session.AgentType = "claude" // default
	}
	if status.Valid {
		session.Status = status.String
	} else {
		session.Status = StatusOK // default
	}

	return session, nil
}

func scanSessions(rows *sql.Rows) ([]Session, error) {
	var sessions []Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sessions: rows error: %w", err)
	}
	return sessions, nil
}
