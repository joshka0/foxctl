// Package sessions implements storage for captured Claude Code conversation sessions.
package sessions

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
)

// Ensure Store implements storage.SessionStore.
var _ storage.SessionStore = (*Store)(nil)

// Session aliases the shared storage type.
type Session = storage.Session

// SessionTurn aliases the shared turn type.
type SessionTurn = storage.SessionTurn

// ToolCall aliases the shared tool call type.
type ToolCall = storage.ToolCall

// Stats aliases the shared stats type.
type Stats = storage.SessionStats

// ListOptions aliases the shared list options type.
type ListOptions = storage.SessionListOptions

// TurnListOptions aliases the shared turn list options type.
type TurnListOptions = storage.SessionTurnListOptions

// SessionChunk aliases the shared chunk type.
type SessionChunk = storage.SessionChunk

// ScoredChunk aliases the shared scored chunk type.
type ScoredChunk = storage.ScoredChunk

// ChunkListOptions aliases the shared chunk list options type.
type ChunkListOptions = storage.ChunkListOptions

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

	_, err = s.db.ExecContext(ctx, `
INSERT INTO sessions (
	id, workspace_path, project_name, git_branch, claude_version,
	started_at, ended_at, summary, accomplished, decisions, gotchas,
	tags, key_files, tools_pattern, message_count, user_turns,
	tool_invocations, total_tokens, raw_jsonl_path, embedding, embedding_model,
	created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
	embedding = COALESCE(excluded.embedding, sessions.embedding),
	embedding_model = COALESCE(excluded.embedding_model, sessions.embedding_model),
	updated_at = excluded.updated_at
`,
		session.ID, session.WorkspacePath, session.ProjectName, session.GitBranch, session.ClaudeVersion,
		sqlutil.FormatTimestamp(session.StartedAt), sqlutil.FormatTimestamp(session.EndedAt),
		session.Summary, accomplishedJSON, decisionsJSON, gotchasJSON,
		tagsJSON, keyFilesJSON, session.ToolsPattern, session.MessageCount, session.UserTurns,
		session.ToolInvocations, session.TotalTokens, session.RawJSONLPath, session.Embedding, session.EmbeddingModel,
		sqlutil.FormatTimestamp(session.CreatedAt), sqlutil.FormatTimestamp(session.UpdatedAt),
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
			tool_invocations, total_tokens, raw_jsonl_path, embedding, embedding_model,
			created_at, updated_at
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

	query := `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, embedding, embedding_model,
			created_at, updated_at
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
			tool_invocations, total_tokens, raw_jsonl_path, embedding, embedding_model,
			created_at, updated_at
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
			updated_at = ?
		WHERE id = ?`,
		summary, accomplishedJSON, decisionsJSON, gotchasJSON, userInsightsJSON, tagsJSON, keyFilesJSON, toolsPattern,
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
func (s *Store) SearchSimilar(ctx context.Context, queryEmbedding []float32, limit int) ([]storage.SimilarSession, error) {
	if limit <= 0 {
		limit = 10
	}

	// Load all sessions with embeddings
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, embedding, embedding_model,
			created_at, updated_at
		FROM sessions
		WHERE embedding IS NOT NULL AND LENGTH(embedding) > 0
		ORDER BY started_at DESC`)
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
		sessionEmb := deserializeEmbedding(session.Embedding)
		if len(sessionEmb) == 0 {
			continue
		}

		// Compute cosine similarity
		similarity := cosineSimilarity(queryEmbedding, sessionEmb)

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

// deserializeEmbedding converts binary bytes back to float32 slice.
func deserializeEmbedding(data []byte) []float32 {
	if len(data) < 4 || len(data)%4 != 0 {
		return nil
	}
	result := make([]float32, len(data)/4)
	for i := range result {
		bits := uint32(data[i*4]) |
			uint32(data[i*4+1])<<8 |
			uint32(data[i*4+2])<<16 |
			uint32(data[i*4+3])<<24
		result[i] = math.Float32frombits(bits)
	}
	return result
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
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
	embedding, embedding_model, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
	embedding = excluded.embedding,
	embedding_model = excluded.embedding_model`,
		chunk.ID, chunk.SessionID, chunk.ChunkIndex, chunk.ChunkType, chunk.ContentHash,
		chunk.ContentPreview, chunk.ByteOffset, chunk.ByteLength, toolsUsedJSON, filesTouchedJSON,
		boolToInt(chunk.HasError), chunk.ErrorType, chunk.Embedding, chunk.EmbeddingModel,
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
	embedding, embedding_model, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			boolToInt(chunk.HasError), chunk.ErrorType, chunk.Embedding, chunk.EmbeddingModel,
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
       embedding, embedding_model, created_at
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
       embedding, embedding_model, created_at
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
       embedding, embedding_model, created_at
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
		chunkEmb := deserializeEmbedding(chunk.Embedding)
		if len(chunkEmb) == 0 {
			continue
		}
		sim := cosineSimilarity(embedding, chunkEmb)
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
		expectedDims = 3072 // default for gemini-embedding-001
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

	err := row.Scan(
		&chunk.ID, &chunk.SessionID, &chunk.ChunkIndex, &chunk.ChunkType, &chunk.ContentHash,
		&contentPreview, &chunk.ByteOffset, &chunk.ByteLength, &toolsUsed, &filesTouched,
		&hasError, &errorType, &chunk.Embedding, &embeddingModel, &createdAt,
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
	updated_at TEXT NOT NULL
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

	err := row.Scan(
		&session.ID, &session.WorkspacePath, &session.ProjectName, &session.GitBranch, &session.ClaudeVersion,
		&startedAt, &endedAt, &summary, &accomplished, &decisions, &gotchas, &userInsights,
		&tags, &keyFiles, &toolsPattern, &session.MessageCount, &session.UserTurns,
		&session.ToolInvocations, &session.TotalTokens, &session.RawJSONLPath, &embedding, &embeddingModel,
		&createdAt, &updatedAt,
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

	session.Embedding = embedding
	if embeddingModel.Valid {
		session.EmbeddingModel = embeddingModel.String
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
