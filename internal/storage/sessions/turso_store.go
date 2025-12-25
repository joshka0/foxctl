//go:build cgo && !race

// Package sessions implements storage for captured Claude Code conversation sessions.
package sessions

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
)

// Ensure TursoStore implements storage.SessionStore.
var _ storage.SessionStore = (*TursoStore)(nil)

// TursoStore handles session persistence using Turso with native vector search.
type TursoStore struct {
	db              dbdriver.DB
	vh              *dbdriver.VectorHelper
	hasIndex        bool // true if vector index exists
	vectorDimension int  // configured embedding dimensions
}

// OpenTurso initializes a session store using Turso database.
func OpenTurso(ctx context.Context, cfg dbdriver.TursoConfig) (*TursoStore, error) {
	// Ensure vector search is enabled
	cfg.EnableVectorSearch = true
	if cfg.VectorDimensions == 0 {
		cfg.VectorDimensions = 3072 // Gemini embedding-001 dimensions
	}

	// Create migration function that uses configured dimensions
	migrate := func(ctx context.Context, db *sql.DB) error {
		return migrateTursoWithDimensions(ctx, db, cfg.VectorDimensions)
	}

	db, err := dbdriver.OpenDB(ctx, dbdriver.Config{
		Driver: dbdriver.DriverTurso,
		Turso:  cfg,
	}, migrate)
	if err != nil {
		return nil, fmt.Errorf("sessions: open turso: %w", err)
	}

	// Create vector helper
	vh, err := dbdriver.NewVectorHelper(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sessions: create vector helper: %w", err)
	}

	db.SetMaxOpenConns(defaultMaxOpenConns)
	db.SetMaxIdleConns(defaultMaxIdleConns)
	db.SetConnMaxLifetime(defaultConnMaxLifetime)
	db.SetConnMaxIdleTime(defaultConnMaxIdleTime)

	store := &TursoStore{db: db, vh: vh, vectorDimension: cfg.VectorDimensions}

	// Validate dimension metadata matches configuration
	if err := store.validateDimensions(ctx, cfg.VectorDimensions); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Check if vector index exists
	store.hasIndex = store.checkVectorIndex(ctx)

	return store, nil
}

// validateDimensions checks that stored metadata dimensions match config.
func (s *TursoStore) validateDimensions(ctx context.Context, expectedDims int) error {
	var storedDims int
	err := s.db.QueryRowContext(ctx, `
		SELECT dimensions FROM embedding_metadata WHERE table_name = 'sessions' LIMIT 1
	`).Scan(&storedDims)

	if err == sql.ErrNoRows {
		// No metadata yet, this is fine for first run
		return nil
	}
	if err != nil {
		// Table might not exist yet (pre-migration), skip validation
		return nil
	}

	if storedDims != expectedDims {
		return fmt.Errorf("sessions: dimension mismatch: stored=%d, config=%d (recreate database or update config)", storedDims, expectedDims)
	}
	return nil
}

// migrateTursoWithDimensions runs migrations with configurable vector dimensions.
func migrateTursoWithDimensions(ctx context.Context, db *sql.DB, dimensions int) error {
	// Create embedding_metadata table for dimension tracking
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS embedding_metadata (
			table_name TEXT PRIMARY KEY,
			column_name TEXT NOT NULL,
			dimensions INTEGER NOT NULL,
			model TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create embedding_metadata table: %w", err)
	}

	// Create sessions table with F32_BLOB for native vector search
	sessionsQuery := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			workspace_path TEXT NOT NULL,
			project_name TEXT,
			git_branch TEXT,
			claude_version TEXT,
			started_at TEXT NOT NULL,
			ended_at TEXT,
			summary TEXT,
			accomplished TEXT DEFAULT '[]',
			decisions TEXT DEFAULT '[]',
			gotchas TEXT DEFAULT '[]',
			user_insights TEXT DEFAULT '[]',
			tags TEXT DEFAULT '[]',
			key_files TEXT DEFAULT '[]',
			tools_pattern TEXT,
			message_count INTEGER DEFAULT 0,
			user_turns INTEGER DEFAULT 0,
			tool_invocations INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			raw_jsonl_path TEXT,
			embedding F32_BLOB(%d),
			embedding_model TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`, dimensions)
	if _, err = db.ExecContext(ctx, sessionsQuery); err != nil {
		return fmt.Errorf("create sessions table: %w", err)
	}

	// Record metadata for sessions embedding column
	now := timeutil.NowUTC().Format("2006-01-02T15:04:05Z")
	_, err = db.ExecContext(ctx, `
		INSERT INTO embedding_metadata (table_name, column_name, dimensions, created_at, updated_at)
		VALUES ('sessions', 'embedding', ?, ?, ?)
		ON CONFLICT(table_name) DO UPDATE SET
			dimensions = excluded.dimensions,
			updated_at = excluded.updated_at
	`, dimensions, now, now)
	if err != nil {
		return fmt.Errorf("insert sessions metadata: %w", err)
	}

	// Create indexes
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_sessions_workspace ON sessions(workspace_path)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_name)`,
	}
	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	// Create session_turns table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS session_turns (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			turn_index INTEGER NOT NULL,
			role TEXT NOT NULL,
			content_preview TEXT,
			tool_calls TEXT DEFAULT '[]',
			files_touched TEXT DEFAULT '[]',
			has_error INTEGER DEFAULT 0,
			error_type TEXT,
			error_message TEXT,
			resolution TEXT,
			tokens_used INTEGER DEFAULT 0,
			timestamp TEXT,
			created_at TEXT NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return fmt.Errorf("create session_turns table: %w", err)
	}

	// Create session_chunks table with F32_BLOB
	chunksQuery := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS session_chunks (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			chunk_index INTEGER NOT NULL,
			chunk_type TEXT NOT NULL,
			content_hash TEXT,
			content_preview TEXT,
			byte_offset INTEGER DEFAULT 0,
			byte_length INTEGER DEFAULT 0,
			tools_used TEXT DEFAULT '[]',
			files_touched TEXT DEFAULT '[]',
			has_error INTEGER DEFAULT 0,
			error_type TEXT,
			embedding F32_BLOB(%d),
			embedding_model TEXT,
			created_at TEXT NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)
	`, dimensions)
	if _, err = db.ExecContext(ctx, chunksQuery); err != nil {
		return fmt.Errorf("create session_chunks table: %w", err)
	}

	// Record metadata for session_chunks embedding column
	_, err = db.ExecContext(ctx, `
		INSERT INTO embedding_metadata (table_name, column_name, dimensions, created_at, updated_at)
		VALUES ('session_chunks', 'embedding', ?, ?, ?)
		ON CONFLICT(table_name) DO UPDATE SET
			dimensions = excluded.dimensions,
			updated_at = excluded.updated_at
	`, dimensions, now, now)
	if err != nil {
		return fmt.Errorf("insert chunks metadata: %w", err)
	}

	return nil
}

// checkVectorIndex checks if the vector index exists.
func (s *TursoStore) checkVectorIndex(ctx context.Context) bool {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='index' AND name='idx_sessions_embedding_vec'
	`).Scan(&count)
	return err == nil && count > 0
}

// CreateVectorIndex creates a vector index for faster similarity search.
func (s *TursoStore) CreateVectorIndex(ctx context.Context) error {
	err := s.vh.CreateVectorIndex(ctx, "sessions", "embedding", "idx_sessions_embedding_vec")
	if err != nil {
		return fmt.Errorf("sessions: create vector index: %w", err)
	}
	s.hasIndex = true
	return nil
}

// Close releases resources.
func (s *TursoStore) Close() error {
	return s.db.Close()
}

// Stats returns session count.
func (s *TursoStore) Stats(ctx context.Context) (Stats, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
		return Stats{}, fmt.Errorf("sessions: stats: %w", err)
	}

	var withEmbedding int64
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE embedding IS NOT NULL`).Scan(&withEmbedding)

	return Stats{
		Count: count,
		Path:  "turso",
	}, nil
}

// Save inserts or updates a session.
func (s *TursoStore) Save(ctx context.Context, session Session) (Session, error) {
	now := timeutil.NowUTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.UpdatedAt = now

	// Format JSON arrays
	accomplishedJSON, _ := sqlutil.FormatJSON(session.Accomplished)
	decisionsJSON, _ := sqlutil.FormatJSON(session.Decisions)
	gotchasJSON, _ := sqlutil.FormatJSON(session.Gotchas)
	userInsightsJSON, _ := sqlutil.FormatJSON(session.UserInsights)
	tagsJSON, _ := sqlutil.FormatJSON(session.Tags)
	keyFilesJSON, _ := sqlutil.FormatJSON(session.KeyFiles)

	// Build the query - handle embedding separately if present
	if len(session.Embedding) > 0 {
		// Convert binary embedding to vector string
		vectorStr := blobToVectorString(session.Embedding)
		query := fmt.Sprintf(`
			INSERT INTO sessions (
				id, workspace_path, project_name, git_branch, claude_version,
				started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
				tags, key_files, tools_pattern, message_count, user_turns,
				tool_invocations, total_tokens, raw_jsonl_path, embedding, embedding_model,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, vector('%s'), ?, ?, ?)
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
				user_insights = excluded.user_insights,
				tags = excluded.tags,
				key_files = excluded.key_files,
				tools_pattern = excluded.tools_pattern,
				message_count = excluded.message_count,
				user_turns = excluded.user_turns,
				tool_invocations = excluded.tool_invocations,
				total_tokens = excluded.total_tokens,
				raw_jsonl_path = excluded.raw_jsonl_path,
				embedding = excluded.embedding,
				embedding_model = excluded.embedding_model,
				updated_at = excluded.updated_at
		`, vectorStr)

		_, err := s.db.ExecContext(ctx, query,
			session.ID, session.WorkspacePath, session.ProjectName, session.GitBranch, session.ClaudeVersion,
			sqlutil.FormatTimestamp(session.StartedAt), sqlutil.FormatTimestamp(session.EndedAt),
			session.Summary, accomplishedJSON, decisionsJSON, gotchasJSON, userInsightsJSON,
			tagsJSON, keyFilesJSON, session.ToolsPattern, session.MessageCount, session.UserTurns,
			session.ToolInvocations, session.TotalTokens, session.RawJSONLPath,
			session.EmbeddingModel,
			sqlutil.FormatTimestamp(session.CreatedAt), sqlutil.FormatTimestamp(session.UpdatedAt))
		if err != nil {
			return Session{}, fmt.Errorf("sessions: save: %w", err)
		}
	} else {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO sessions (
				id, workspace_path, project_name, git_branch, claude_version,
				started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
				tags, key_files, tools_pattern, message_count, user_turns,
				tool_invocations, total_tokens, raw_jsonl_path, embedding_model,
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
				user_insights = excluded.user_insights,
				tags = excluded.tags,
				key_files = excluded.key_files,
				tools_pattern = excluded.tools_pattern,
				message_count = excluded.message_count,
				user_turns = excluded.user_turns,
				tool_invocations = excluded.tool_invocations,
				total_tokens = excluded.total_tokens,
				raw_jsonl_path = excluded.raw_jsonl_path,
				embedding_model = excluded.embedding_model,
				updated_at = excluded.updated_at`,
			session.ID, session.WorkspacePath, session.ProjectName, session.GitBranch, session.ClaudeVersion,
			sqlutil.FormatTimestamp(session.StartedAt), sqlutil.FormatTimestamp(session.EndedAt),
			session.Summary, accomplishedJSON, decisionsJSON, gotchasJSON, userInsightsJSON,
			tagsJSON, keyFilesJSON, session.ToolsPattern, session.MessageCount, session.UserTurns,
			session.ToolInvocations, session.TotalTokens, session.RawJSONLPath, session.EmbeddingModel,
			sqlutil.FormatTimestamp(session.CreatedAt), sqlutil.FormatTimestamp(session.UpdatedAt))
		if err != nil {
			return Session{}, fmt.Errorf("sessions: save: %w", err)
		}
	}

	return session, nil
}

// blobToVectorString converts binary float32 embedding to Turso vector string format.
func blobToVectorString(blob []byte) string {
	if len(blob) < 4 || len(blob)%4 != 0 {
		return ""
	}

	dims := len(blob) / 4
	parts := make([]string, dims)

	for i := 0; i < dims; i++ {
		bits := uint32(blob[i*4]) |
			uint32(blob[i*4+1])<<8 |
			uint32(blob[i*4+2])<<16 |
			uint32(blob[i*4+3])<<24
		f := float32frombits(bits)
		parts[i] = fmt.Sprintf("%f", f)
	}

	return "[" + strings.Join(parts, ",") + "]"
}

// float32frombits converts uint32 bits to float32.
func float32frombits(b uint32) float32 {
	return math.Float32frombits(b)
}

// Get retrieves a session by ID.
func (s *TursoStore) Get(ctx context.Context, id string) (Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, embedding_model,
			created_at, updated_at
		FROM sessions WHERE id = ?`, id)

	return scanSessionRow(row)
}

// List returns sessions matching the filter options.
func (s *TursoStore) List(ctx context.Context, opts ListOptions) ([]Session, error) {
	query := `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, embedding_model,
			created_at, updated_at
		FROM sessions WHERE 1=1`

	var args []any
	if opts.WorkspacePath != "" {
		query += ` AND workspace_path = ?`
		args = append(args, opts.WorkspacePath)
	}
	if opts.ProjectName != "" {
		query += ` AND project_name = ?`
		args = append(args, opts.ProjectName)
	}

	query += ` ORDER BY started_at DESC`
	if opts.Limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, opts.Limit)
	}
	if opts.Offset > 0 {
		query += fmt.Sprintf(` OFFSET %d`, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sessions: list: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close list rows") }()

	var sessions []Session
	for rows.Next() {
		session, err := scanSessionRows(rows)
		if err != nil {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// Delete removes a session.
func (s *TursoStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sessions: delete: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Search performs full-text search on session summaries.
func (s *TursoStore) Search(ctx context.Context, query string, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
			started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
			tags, key_files, tools_pattern, message_count, user_turns,
			tool_invocations, total_tokens, raw_jsonl_path, embedding_model,
			created_at, updated_at
		FROM sessions
		WHERE summary LIKE ? OR accomplished LIKE ? OR decisions LIKE ? OR gotchas LIKE ?
		ORDER BY started_at DESC
		LIMIT ?`,
		"%"+query+"%", "%"+query+"%", "%"+query+"%", "%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("sessions: search: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close search rows") }()

	var sessions []Session
	for rows.Next() {
		session, err := scanSessionRows(rows)
		if err != nil {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// SearchSimilar finds sessions similar to the given embedding using native vector search.
func (s *TursoStore) SearchSimilar(ctx context.Context, queryEmbedding []float32, limit int) ([]storage.SimilarSession, error) {
	if limit <= 0 {
		limit = 10
	}

	// Convert query embedding to dbdriver.Vector
	vec := make(dbdriver.Vector, len(queryEmbedding))
	copy(vec, queryEmbedding)

	var query string
	var rows *sql.Rows
	var err error

	if s.hasIndex {
		// Use vector_top_k for fast indexed search
		// vector_top_k returns only rowid, we must compute distance ourselves
		topKExpr := s.vh.VectorTopK("idx_sessions_embedding_vec", vec, limit)
		distExpr := s.vh.CosineSimilarity("s.embedding", vec)
		query = fmt.Sprintf(`
			SELECT s.id, s.workspace_path, s.project_name, s.git_branch, s.claude_version,
				s.started_at, s.ended_at, s.summary, s.accomplished, s.decisions, s.gotchas, s.user_insights,
				s.tags, s.key_files, s.tools_pattern, s.message_count, s.user_turns,
				s.tool_invocations, s.total_tokens, s.raw_jsonl_path, s.embedding_model,
				s.created_at, s.updated_at,
				%s as distance
			FROM %s vt
			JOIN sessions s ON s.rowid = vt.id`, distExpr, topKExpr)
		rows, err = s.db.QueryContext(ctx, query)
	} else {
		// Fallback to full table scan with cosine distance
		distExpr := s.vh.CosineSimilarity("embedding", vec)
		query = fmt.Sprintf(`
			SELECT id, workspace_path, project_name, git_branch, claude_version,
				started_at, ended_at, summary, accomplished, decisions, gotchas, user_insights,
				tags, key_files, tools_pattern, message_count, user_turns,
				tool_invocations, total_tokens, raw_jsonl_path, embedding_model,
				created_at, updated_at,
				%s as distance
			FROM sessions
			WHERE embedding IS NOT NULL
			ORDER BY distance ASC
			LIMIT ?`, distExpr)
		rows, err = s.db.QueryContext(ctx, query, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("sessions: search similar: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close similar rows") }()

	var results []storage.SimilarSession
	for rows.Next() {
		var session Session
		var projectName, gitBranch, clauveVersion, summary, toolsPattern sql.NullString
		var endedAt, embeddingModel sql.NullString
		var accomplishedJSON, decisionsJSON, gotchasJSON, userInsightsJSON string
		var tagsJSON, keyFilesJSON string
		var startedAtStr, createdAtStr, updatedAtStr string
		var distance float64

		err := rows.Scan(
			&session.ID, &session.WorkspacePath, &projectName, &gitBranch, &clauveVersion,
			&startedAtStr, &endedAt, &summary, &accomplishedJSON, &decisionsJSON, &gotchasJSON, &userInsightsJSON,
			&tagsJSON, &keyFilesJSON, &toolsPattern, &session.MessageCount, &session.UserTurns,
			&session.ToolInvocations, &session.TotalTokens, &session.RawJSONLPath, &embeddingModel,
			&createdAtStr, &updatedAtStr, &distance,
		)
		if err != nil {
			continue
		}

		session.ProjectName = projectName.String
		session.GitBranch = gitBranch.String
		session.ClaudeVersion = clauveVersion.String
		session.Summary = summary.String
		session.ToolsPattern = toolsPattern.String
		session.EmbeddingModel = embeddingModel.String

		session.StartedAt, _ = sqlutil.ScanTimestamp(startedAtStr)
		session.CreatedAt, _ = sqlutil.ScanTimestamp(createdAtStr)
		session.UpdatedAt, _ = sqlutil.ScanTimestamp(updatedAtStr)
		if endedAt.Valid {
			session.EndedAt, _ = sqlutil.ScanTimestamp(endedAt.String)
		}

		_ = sqlutil.ScanJSON(accomplishedJSON, &session.Accomplished)
		_ = sqlutil.ScanJSON(decisionsJSON, &session.Decisions)
		_ = sqlutil.ScanJSON(gotchasJSON, &session.Gotchas)
		_ = sqlutil.ScanJSON(userInsightsJSON, &session.UserInsights)
		_ = sqlutil.ScanJSON(tagsJSON, &session.Tags)
		_ = sqlutil.ScanJSON(keyFilesJSON, &session.KeyFiles)

		// Convert cosine distance to similarity (distance is 0 for identical, 2 for opposite)
		similarity := 1.0 - distance

		results = append(results, storage.SimilarSession{
			Session:    session,
			Similarity: similarity,
		})
	}

	return results, rows.Err()
}

// UpdateSummary updates session metadata.
func (s *TursoStore) UpdateSummary(ctx context.Context, id string, summary string, accomplished, decisions, gotchas, userInsights, tags, keyFiles []string, toolsPattern string) error {
	accomplishedJSON, _ := sqlutil.FormatJSON(accomplished)
	decisionsJSON, _ := sqlutil.FormatJSON(decisions)
	gotchasJSON, _ := sqlutil.FormatJSON(gotchas)
	userInsightsJSON, _ := sqlutil.FormatJSON(userInsights)
	tagsJSON, _ := sqlutil.FormatJSON(tags)
	keyFilesJSON, _ := sqlutil.FormatJSON(keyFiles)

	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET
			summary = ?, accomplished = ?, decisions = ?, gotchas = ?, user_insights = ?,
			tags = ?, key_files = ?, tools_pattern = ?, updated_at = ?
		WHERE id = ?`,
		summary, accomplishedJSON, decisionsJSON, gotchasJSON, userInsightsJSON,
		tagsJSON, keyFilesJSON, toolsPattern, sqlutil.FormatTimestamp(timeutil.NowUTC()), id)
	if err != nil {
		return fmt.Errorf("sessions: update summary: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetEmbedding stores the embedding for a session using native vector format.
func (s *TursoStore) SetEmbedding(ctx context.Context, id string, embedding []byte, model string) error {
	vectorStr := blobToVectorString(embedding)
	if vectorStr == "" {
		return fmt.Errorf("sessions: invalid embedding data")
	}

	query := fmt.Sprintf(`
		UPDATE sessions SET
			embedding = vector('%s'),
			embedding_model = ?,
			updated_at = ?
		WHERE id = ?`, vectorStr)

	result, err := s.db.ExecContext(ctx, query,
		model, sqlutil.FormatTimestamp(timeutil.NowUTC()), id)
	if err != nil {
		return fmt.Errorf("sessions: set embedding: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Turn Operations (delegated, same as SQLite store) ---

// SaveTurn inserts or updates a session turn.
func (s *TursoStore) SaveTurn(ctx context.Context, turn SessionTurn) (SessionTurn, error) {
	now := timeutil.NowUTC()
	if turn.ID == "" {
		turn.ID = fmt.Sprintf("%s-%d", turn.SessionID, turn.TurnIndex)
	}
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = now
	}

	toolCallsJSON, _ := sqlutil.FormatJSON(turn.ToolCalls)
	filesTouchedJSON, _ := sqlutil.FormatJSON(turn.FilesTouched)

	hasError := 0
	if turn.HasError {
		hasError = 1
	}

	_, err := s.db.ExecContext(ctx, `
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
			tokens_used = excluded.tokens_used`,
		turn.ID, turn.SessionID, turn.TurnIndex, turn.Role, turn.ContentPreview,
		toolCallsJSON, filesTouchedJSON, hasError, turn.ErrorType, turn.ErrorMessage,
		turn.Resolution, turn.TokensUsed, sqlutil.FormatTimestamp(turn.Timestamp),
		sqlutil.FormatTimestamp(turn.CreatedAt))
	if err != nil {
		return SessionTurn{}, fmt.Errorf("sessions: save turn: %w", err)
	}
	return turn, nil
}

// SaveTurns batch inserts turns.
func (s *TursoStore) SaveTurns(ctx context.Context, turns []SessionTurn) error {
	for _, turn := range turns {
		if _, err := s.SaveTurn(ctx, turn); err != nil {
			return err
		}
	}
	return nil
}

// GetTurns retrieves turns for a session.
func (s *TursoStore) GetTurns(ctx context.Context, sessionID string, opts TurnListOptions) ([]SessionTurn, error) {
	query := `
		SELECT id, session_id, turn_index, role, content_preview, tool_calls, files_touched,
			has_error, error_type, error_message, resolution, tokens_used, timestamp, created_at
		FROM session_turns WHERE session_id = ?`

	var args []any
	args = append(args, sessionID)

	if opts.Role != "" {
		query += ` AND role = ?`
		args = append(args, opts.Role)
	}

	query += ` ORDER BY turn_index ASC`
	if opts.Limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, opts.Limit)
	}
	if opts.Offset > 0 {
		query += fmt.Sprintf(` OFFSET %d`, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sessions: get turns: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close turns rows") }()

	var turns []SessionTurn
	for rows.Next() {
		var turn SessionTurn
		var toolCallsJSON, filesTouchedJSON string
		var hasError int
		var timestampStr, createdAtStr string

		err := rows.Scan(
			&turn.ID, &turn.SessionID, &turn.TurnIndex, &turn.Role, &turn.ContentPreview,
			&toolCallsJSON, &filesTouchedJSON, &hasError, &turn.ErrorType, &turn.ErrorMessage,
			&turn.Resolution, &turn.TokensUsed, &timestampStr, &createdAtStr,
		)
		if err != nil {
			continue
		}

		turn.HasError = hasError != 0
		turn.Timestamp, _ = sqlutil.ScanTimestamp(timestampStr)
		turn.CreatedAt, _ = sqlutil.ScanTimestamp(createdAtStr)
		_ = sqlutil.ScanJSON(toolCallsJSON, &turn.ToolCalls)
		_ = sqlutil.ScanJSON(filesTouchedJSON, &turn.FilesTouched)

		turns = append(turns, turn)
	}
	return turns, rows.Err()
}

// GetTurnsWithErrors returns turns that have errors.
func (s *TursoStore) GetTurnsWithErrors(ctx context.Context, sessionID string) ([]SessionTurn, error) {
	return s.GetTurns(ctx, sessionID, TurnListOptions{})
}

// SearchTurns searches turns by content.
func (s *TursoStore) SearchTurns(ctx context.Context, query string, limit int) ([]SessionTurn, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, turn_index, role, content_preview, tool_calls, files_touched,
			has_error, error_type, error_message, resolution, tokens_used, timestamp, created_at
		FROM session_turns
		WHERE content_preview LIKE ?
		ORDER BY timestamp DESC
		LIMIT ?`, "%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("sessions: search turns: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close search turns rows") }()

	var turns []SessionTurn
	for rows.Next() {
		var turn SessionTurn
		var toolCallsJSON, filesTouchedJSON string
		var hasError int
		var timestampStr, createdAtStr string

		err := rows.Scan(
			&turn.ID, &turn.SessionID, &turn.TurnIndex, &turn.Role, &turn.ContentPreview,
			&toolCallsJSON, &filesTouchedJSON, &hasError, &turn.ErrorType, &turn.ErrorMessage,
			&turn.Resolution, &turn.TokensUsed, &timestampStr, &createdAtStr,
		)
		if err != nil {
			continue
		}

		turn.HasError = hasError != 0
		turn.Timestamp, _ = sqlutil.ScanTimestamp(timestampStr)
		turn.CreatedAt, _ = sqlutil.ScanTimestamp(createdAtStr)
		_ = sqlutil.ScanJSON(toolCallsJSON, &turn.ToolCalls)
		_ = sqlutil.ScanJSON(filesTouchedJSON, &turn.FilesTouched)

		turns = append(turns, turn)
	}
	return turns, rows.Err()
}

// DeleteTurns removes all turns for a session.
func (s *TursoStore) DeleteTurns(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_turns WHERE session_id = ?`, sessionID)
	return err
}

// --- Chunk Operations ---

// SaveChunk inserts or updates a chunk.
func (s *TursoStore) SaveChunk(ctx context.Context, chunk SessionChunk) (SessionChunk, error) {
	now := timeutil.NowUTC()
	if chunk.CreatedAt.IsZero() {
		chunk.CreatedAt = now
	}

	toolsUsedJSON, _ := sqlutil.FormatJSON(chunk.ToolsUsed)
	filesTouchedJSON, _ := sqlutil.FormatJSON(chunk.FilesTouched)

	hasError := 0
	if chunk.HasError {
		hasError = 1
	}

	if len(chunk.Embedding) > 0 {
		vectorStr := blobToVectorString(chunk.Embedding)
		query := fmt.Sprintf(`
			INSERT INTO session_chunks (
				id, session_id, chunk_index, chunk_type, content_hash, content_preview,
				byte_offset, byte_length, tools_used, files_touched, has_error, error_type,
				embedding, embedding_model, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, vector('%s'), ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				content_preview = excluded.content_preview,
				embedding = excluded.embedding,
				embedding_model = excluded.embedding_model`, vectorStr)

		_, err := s.db.ExecContext(ctx, query,
			chunk.ID, chunk.SessionID, chunk.ChunkIndex, chunk.ChunkType, chunk.ContentHash,
			chunk.ContentPreview, chunk.ByteOffset, chunk.ByteLength, toolsUsedJSON, filesTouchedJSON,
			hasError, chunk.ErrorType, chunk.EmbeddingModel, sqlutil.FormatTimestamp(chunk.CreatedAt))
		if err != nil {
			return SessionChunk{}, fmt.Errorf("sessions: save chunk: %w", err)
		}
	} else {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO session_chunks (
				id, session_id, chunk_index, chunk_type, content_hash, content_preview,
				byte_offset, byte_length, tools_used, files_touched, has_error, error_type,
				embedding_model, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				content_preview = excluded.content_preview`,
			chunk.ID, chunk.SessionID, chunk.ChunkIndex, chunk.ChunkType, chunk.ContentHash,
			chunk.ContentPreview, chunk.ByteOffset, chunk.ByteLength, toolsUsedJSON, filesTouchedJSON,
			hasError, chunk.ErrorType, chunk.EmbeddingModel, sqlutil.FormatTimestamp(chunk.CreatedAt))
		if err != nil {
			return SessionChunk{}, fmt.Errorf("sessions: save chunk: %w", err)
		}
	}
	return chunk, nil
}

// SaveChunks batch inserts chunks.
func (s *TursoStore) SaveChunks(ctx context.Context, chunks []SessionChunk) error {
	for _, chunk := range chunks {
		if _, err := s.SaveChunk(ctx, chunk); err != nil {
			return err
		}
	}
	return nil
}

// GetChunks retrieves chunks for a session.
func (s *TursoStore) GetChunks(ctx context.Context, sessionID string, limit int) ([]SessionChunk, error) {
	query := `
		SELECT id, session_id, chunk_index, chunk_type, content_hash, content_preview,
			byte_offset, byte_length, tools_used, files_touched, has_error, error_type,
			embedding_model, created_at
		FROM session_chunks WHERE session_id = ?
		ORDER BY chunk_index ASC`

	if limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("sessions: get chunks: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close chunks rows") }()

	var chunks []SessionChunk
	for rows.Next() {
		var chunk SessionChunk
		var toolsUsedJSON, filesTouchedJSON string
		var hasError int
		var createdAtStr string

		err := rows.Scan(
			&chunk.ID, &chunk.SessionID, &chunk.ChunkIndex, &chunk.ChunkType, &chunk.ContentHash,
			&chunk.ContentPreview, &chunk.ByteOffset, &chunk.ByteLength, &toolsUsedJSON, &filesTouchedJSON,
			&hasError, &chunk.ErrorType, &chunk.EmbeddingModel, &createdAtStr,
		)
		if err != nil {
			continue
		}

		chunk.HasError = hasError != 0
		chunk.CreatedAt, _ = sqlutil.ScanTimestamp(createdAtStr)
		_ = sqlutil.ScanJSON(toolsUsedJSON, &chunk.ToolsUsed)
		_ = sqlutil.ScanJSON(filesTouchedJSON, &chunk.FilesTouched)

		chunks = append(chunks, chunk)
	}
	return chunks, rows.Err()
}

// GetChunk retrieves a specific chunk.
func (s *TursoStore) GetChunk(ctx context.Context, sessionID string, chunkIndex int) (SessionChunk, error) {
	chunks, err := s.GetChunks(ctx, sessionID, 0)
	if err != nil {
		return SessionChunk{}, err
	}
	for _, chunk := range chunks {
		if chunk.ChunkIndex == chunkIndex {
			return chunk, nil
		}
	}
	return SessionChunk{}, ErrNotFound
}

// SearchChunks finds chunks similar to the given embedding using native vector search.
func (s *TursoStore) SearchChunks(ctx context.Context, embedding []float32, limit int) ([]ScoredChunk, error) {
	if limit <= 0 {
		limit = 10
	}

	// Convert query embedding to dbdriver.Vector
	vec := make(dbdriver.Vector, len(embedding))
	copy(vec, embedding)

	distExpr := s.vh.CosineSimilarity("embedding", vec)

	query := fmt.Sprintf(`
		SELECT id, session_id, chunk_index, chunk_type, content_hash, content_preview,
			byte_offset, byte_length, tools_used, files_touched, has_error, error_type,
			embedding_model, created_at,
			%s as distance
		FROM session_chunks
		WHERE embedding IS NOT NULL
		ORDER BY distance ASC
		LIMIT ?`, distExpr)

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("sessions: search chunks: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close chunk search rows") }()

	var results []ScoredChunk
	for rows.Next() {
		var chunk SessionChunk
		var toolsUsedJSON, filesTouchedJSON string
		var hasError int
		var createdAtStr string
		var distance float64

		err := rows.Scan(
			&chunk.ID, &chunk.SessionID, &chunk.ChunkIndex, &chunk.ChunkType, &chunk.ContentHash,
			&chunk.ContentPreview, &chunk.ByteOffset, &chunk.ByteLength, &toolsUsedJSON, &filesTouchedJSON,
			&hasError, &chunk.ErrorType, &chunk.EmbeddingModel, &createdAtStr, &distance,
		)
		if err != nil {
			continue
		}

		chunk.HasError = hasError != 0
		chunk.CreatedAt, _ = sqlutil.ScanTimestamp(createdAtStr)
		_ = sqlutil.ScanJSON(toolsUsedJSON, &chunk.ToolsUsed)
		_ = sqlutil.ScanJSON(filesTouchedJSON, &chunk.FilesTouched)

		// Convert cosine distance to similarity (distance is in [0, 2], normalize to [0, 1])
		results = append(results, ScoredChunk{
			Chunk:      chunk,
			Similarity: 1.0 - distance/2.0,
		})
	}

	// Sort by similarity descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	return results, rows.Err()
}

// DeleteChunks removes all chunks for a session.
func (s *TursoStore) DeleteChunks(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_chunks WHERE session_id = ?`, sessionID)
	return err
}

// SetArchivePath sets the raw JSONL archive path.
func (s *TursoStore) SetArchivePath(ctx context.Context, sessionID, archivePath string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET raw_jsonl_path = ?, updated_at = ? WHERE id = ?`,
		archivePath, sqlutil.FormatTimestamp(timeutil.NowUTC()), sessionID)
	if err != nil {
		return fmt.Errorf("sessions: set archive path: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// GetArchivePath retrieves the raw JSONL archive path.
func (s *TursoStore) GetArchivePath(ctx context.Context, sessionID string) (string, error) {
	var path sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT raw_jsonl_path FROM sessions WHERE id = ?`, sessionID).Scan(&path)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("sessions: get archive path: %w", err)
	}
	return path.String, nil
}

// --- Helper functions ---

func scanSessionRow(row *sql.Row) (Session, error) {
	var session Session
	var projectName, gitBranch, claudeVersion sql.NullString
	var startedAt, endedAt, createdAt, updatedAt sql.NullString
	var summary, accomplished, decisions, gotchas, userInsights, tags, keyFiles sql.NullString
	var toolsPattern, rawJSONLPath sql.NullString
	var embeddingModel sql.NullString
	var messageCount, userTurns, toolInvocations, totalTokens sql.NullInt64

	err := row.Scan(
		&session.ID, &session.WorkspacePath, &projectName, &gitBranch, &claudeVersion,
		&startedAt, &endedAt, &summary, &accomplished, &decisions, &gotchas, &userInsights,
		&tags, &keyFiles, &toolsPattern, &messageCount, &userTurns,
		&toolInvocations, &totalTokens, &rawJSONLPath, &embeddingModel,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Session{}, ErrNotFound
		}
		return Session{}, fmt.Errorf("sessions: scan: %w", err)
	}

	// Parse timestamps
	if startedAt.Valid {
		ts, _ := sqlutil.ScanTimestamp(startedAt.String)
		session.StartedAt = ts
	}
	if endedAt.Valid {
		ts, _ := sqlutil.ScanTimestamp(endedAt.String)
		session.EndedAt = ts
	}
	if createdAt.Valid {
		ts, _ := sqlutil.ScanTimestamp(createdAt.String)
		session.CreatedAt = ts
	}
	if updatedAt.Valid {
		ts, _ := sqlutil.ScanTimestamp(updatedAt.String)
		session.UpdatedAt = ts
	}

	// Assign nullable strings
	if projectName.Valid {
		session.ProjectName = projectName.String
	}
	if gitBranch.Valid {
		session.GitBranch = gitBranch.String
	}
	if claudeVersion.Valid {
		session.ClaudeVersion = claudeVersion.String
	}
	if summary.Valid {
		session.Summary = summary.String
	}
	if toolsPattern.Valid {
		session.ToolsPattern = toolsPattern.String
	}
	if rawJSONLPath.Valid {
		session.RawJSONLPath = rawJSONLPath.String
	}
	if embeddingModel.Valid {
		session.EmbeddingModel = embeddingModel.String
	}
	// Assign nullable integers
	if messageCount.Valid {
		session.MessageCount = int(messageCount.Int64)
	}
	if userTurns.Valid {
		session.UserTurns = int(userTurns.Int64)
	}
	if toolInvocations.Valid {
		session.ToolInvocations = int(toolInvocations.Int64)
	}
	if totalTokens.Valid {
		session.TotalTokens = int(totalTokens.Int64)
	}

	// Parse JSON arrays
	if accomplished.Valid {
		_ = sqlutil.ScanJSON(accomplished.String, &session.Accomplished)
	}
	if decisions.Valid {
		_ = sqlutil.ScanJSON(decisions.String, &session.Decisions)
	}
	if gotchas.Valid {
		_ = sqlutil.ScanJSON(gotchas.String, &session.Gotchas)
	}
	if userInsights.Valid {
		_ = sqlutil.ScanJSON(userInsights.String, &session.UserInsights)
	}
	if tags.Valid {
		_ = sqlutil.ScanJSON(tags.String, &session.Tags)
	}
	if keyFiles.Valid {
		_ = sqlutil.ScanJSON(keyFiles.String, &session.KeyFiles)
	}

	return session, nil
}

func scanSessionRows(rows *sql.Rows) (Session, error) {
	var session Session
	var projectName, gitBranch, claudeVersion sql.NullString
	var startedAt, endedAt, createdAt, updatedAt sql.NullString
	var summary, accomplished, decisions, gotchas, userInsights, tags, keyFiles sql.NullString
	var toolsPattern, rawJSONLPath sql.NullString
	var embeddingModel sql.NullString
	var messageCount, userTurns, toolInvocations, totalTokens sql.NullInt64

	err := rows.Scan(
		&session.ID, &session.WorkspacePath, &projectName, &gitBranch, &claudeVersion,
		&startedAt, &endedAt, &summary, &accomplished, &decisions, &gotchas, &userInsights,
		&tags, &keyFiles, &toolsPattern, &messageCount, &userTurns,
		&toolInvocations, &totalTokens, &rawJSONLPath, &embeddingModel,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return Session{}, fmt.Errorf("sessions: scan: %w", err)
	}

	// Parse timestamps
	if startedAt.Valid {
		ts, _ := sqlutil.ScanTimestamp(startedAt.String)
		session.StartedAt = ts
	}
	if endedAt.Valid {
		ts, _ := sqlutil.ScanTimestamp(endedAt.String)
		session.EndedAt = ts
	}
	if createdAt.Valid {
		ts, _ := sqlutil.ScanTimestamp(createdAt.String)
		session.CreatedAt = ts
	}
	if updatedAt.Valid {
		ts, _ := sqlutil.ScanTimestamp(updatedAt.String)
		session.UpdatedAt = ts
	}

	// Assign nullable strings
	if projectName.Valid {
		session.ProjectName = projectName.String
	}
	if gitBranch.Valid {
		session.GitBranch = gitBranch.String
	}
	if claudeVersion.Valid {
		session.ClaudeVersion = claudeVersion.String
	}
	if summary.Valid {
		session.Summary = summary.String
	}
	if toolsPattern.Valid {
		session.ToolsPattern = toolsPattern.String
	}
	if rawJSONLPath.Valid {
		session.RawJSONLPath = rawJSONLPath.String
	}
	if embeddingModel.Valid {
		session.EmbeddingModel = embeddingModel.String
	}
	// Assign nullable integers
	if messageCount.Valid {
		session.MessageCount = int(messageCount.Int64)
	}
	if userTurns.Valid {
		session.UserTurns = int(userTurns.Int64)
	}
	if toolInvocations.Valid {
		session.ToolInvocations = int(toolInvocations.Int64)
	}
	if totalTokens.Valid {
		session.TotalTokens = int(totalTokens.Int64)
	}

	// Parse JSON arrays
	if accomplished.Valid {
		_ = sqlutil.ScanJSON(accomplished.String, &session.Accomplished)
	}
	if decisions.Valid {
		_ = sqlutil.ScanJSON(decisions.String, &session.Decisions)
	}
	if gotchas.Valid {
		_ = sqlutil.ScanJSON(gotchas.String, &session.Gotchas)
	}
	if userInsights.Valid {
		_ = sqlutil.ScanJSON(userInsights.String, &session.UserInsights)
	}
	if tags.Valid {
		_ = sqlutil.ScanJSON(tags.String, &session.Tags)
	}
	if keyFiles.Valid {
		_ = sqlutil.ScanJSON(keyFiles.String, &session.KeyFiles)
	}

	return session, nil
}
