package embedding

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/oklog/ulid/v2"
)

// ErrNotFound indicates the requested resource doesn't exist.
var ErrNotFound = errors.New("not found")

// Store manages the embedding job queue and results in SQLite.
type Store struct {
	db *sql.DB
}

// OpenStore opens or creates the embedding queue database.
func OpenStore(ctx context.Context, root string) (*Store, error) {
	dbPath := filepath.Join(root, "embedding_queue.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("embedding: open db: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS embedding_jobs (
		id TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL,
		symbol_id TEXT NOT NULL,
		file_path TEXT NOT NULL,
		symbol_name TEXT NOT NULL,
		content TEXT NOT NULL,
		content_digest TEXT NOT NULL,
		state TEXT NOT NULL DEFAULT 'queued',
		priority INTEGER NOT NULL DEFAULT 50,
		attempts INTEGER NOT NULL DEFAULT 0,
		max_attempts INTEGER NOT NULL DEFAULT 3,
		error TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		scheduled_at TEXT,
		completed_at TEXT,
		UNIQUE(workspace_id, symbol_id, content_digest)
	);

	CREATE INDEX IF NOT EXISTS idx_jobs_state_priority
		ON embedding_jobs(state, priority DESC, created_at);

	CREATE INDEX IF NOT EXISTS idx_jobs_workspace_symbol
		ON embedding_jobs(workspace_id, symbol_id);

	CREATE TABLE IF NOT EXISTS symbol_embeddings (
		symbol_id TEXT NOT NULL,
		workspace_id TEXT NOT NULL,
		file_path TEXT NOT NULL,
		embedding BLOB NOT NULL,
		content_digest TEXT NOT NULL,
		model TEXT NOT NULL,
		dimensions INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (workspace_id, symbol_id)
	);

	CREATE INDEX IF NOT EXISTS idx_embeddings_file
		ON symbol_embeddings(workspace_id, file_path);
	`

	_, err := db.ExecContext(ctx, schema)
	return err
}

// Enqueue adds symbols to the embedding queue.
func (s *Store) Enqueue(ctx context.Context, req EnqueueRequest) (*EnqueueResult, error) {
	result := &EnqueueResult{}
	now := time.Now().UTC()

	priority := req.Priority
	if priority == 0 {
		priority = PriorityNormal
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO embedding_jobs
			(id, workspace_id, symbol_id, file_path, symbol_name, content,
			 content_digest, state, priority, max_attempts, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'queued', ?, 3, ?, ?)
		ON CONFLICT(workspace_id, symbol_id, content_digest) DO NOTHING
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, sym := range req.Symbols {
		contentDigest := computeDigest(sym.Content)

		// Check if embedding already exists with same digest (deduplication)
		if req.Deduplicate {
			var exists bool
			err := tx.QueryRowContext(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM symbol_embeddings
					WHERE workspace_id = ? AND symbol_id = ? AND content_digest = ?
				)
			`, req.WorkspaceID, sym.SymbolID, contentDigest).Scan(&exists)
			if err == nil && exists {
				result.Skipped++
				continue
			}
		}

		id := ulid.Make().String()
		nowStr := now.Format(time.RFC3339Nano)

		res, err := stmt.ExecContext(ctx,
			id, req.WorkspaceID, sym.SymbolID, sym.FilePath, sym.SymbolName,
			sym.Content, contentDigest, priority, nowStr, nowStr,
		)
		if err != nil {
			return nil, fmt.Errorf("insert job: %w", err)
		}

		affected, _ := res.RowsAffected()
		if affected > 0 {
			result.Queued++
			result.JobIDs = append(result.JobIDs, id)
		} else {
			result.Skipped++ // Conflict = already queued
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return result, nil
}

// ClaimNext claims the next available job for processing.
// Returns nil if no jobs are available.
func (s *Store) ClaimNext(ctx context.Context) (*EmbeddingJob, error) {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	// Atomically claim a job using UPDATE...RETURNING
	row := s.db.QueryRowContext(ctx, `
		UPDATE embedding_jobs
		SET state = 'running', updated_at = ?, attempts = attempts + 1
		WHERE id = (
			SELECT id FROM embedding_jobs
			WHERE state IN ('queued', 'retry')
			AND (scheduled_at IS NULL OR scheduled_at <= ?)
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
		)
		RETURNING id, workspace_id, symbol_id, file_path, symbol_name, content,
		          content_digest, state, priority, attempts, max_attempts, error,
		          created_at, updated_at, scheduled_at, completed_at
	`, nowStr, nowStr)

	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return job, nil
}

// Complete marks a job as completed and stores the embedding.
func (s *Store) Complete(ctx context.Context, jobID string, embedding []float32, model string) error {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Get job details
	var job EmbeddingJob
	var createdStr string

	err = tx.QueryRowContext(ctx, `
		SELECT workspace_id, symbol_id, file_path, content_digest, created_at
		FROM embedding_jobs WHERE id = ?
	`, jobID).Scan(&job.WorkspaceID, &job.SymbolID, &job.FilePath, &job.ContentDigest, &createdStr)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}

	// Store the embedding
	embeddingBytes, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO symbol_embeddings
			(symbol_id, workspace_id, file_path, embedding, content_digest, model, dimensions, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, symbol_id) DO UPDATE SET
			embedding = excluded.embedding,
			content_digest = excluded.content_digest,
			model = excluded.model,
			dimensions = excluded.dimensions,
			created_at = excluded.created_at
	`, job.SymbolID, job.WorkspaceID, job.FilePath, embeddingBytes, job.ContentDigest, model, len(embedding), nowStr)
	if err != nil {
		return fmt.Errorf("save embedding: %w", err)
	}

	// Mark job complete
	_, err = tx.ExecContext(ctx, `
		UPDATE embedding_jobs
		SET state = 'ok', updated_at = ?, completed_at = ?, error = NULL
		WHERE id = ?
	`, nowStr, nowStr, jobID)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}

	return tx.Commit()
}

// Fail marks a job as failed, potentially scheduling a retry.
func (s *Store) Fail(ctx context.Context, jobID string, errMsg string) error {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	// Get current attempts and max
	var attempts, maxAttempts int
	err := s.db.QueryRowContext(ctx, `
		SELECT attempts, max_attempts FROM embedding_jobs WHERE id = ?
	`, jobID).Scan(&attempts, &maxAttempts)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}

	if attempts < maxAttempts {
		// Schedule retry with exponential backoff
		backoff := time.Duration(1<<uint(attempts-1)) * time.Minute
		scheduledAt := now.Add(backoff).Format(time.RFC3339Nano)

		_, err = s.db.ExecContext(ctx, `
			UPDATE embedding_jobs
			SET state = 'retry', updated_at = ?, scheduled_at = ?, error = ?
			WHERE id = ?
		`, nowStr, scheduledAt, errMsg, jobID)
	} else {
		// Max retries exceeded
		_, err = s.db.ExecContext(ctx, `
			UPDATE embedding_jobs
			SET state = 'error', updated_at = ?, completed_at = ?, error = ?
			WHERE id = ?
		`, nowStr, nowStr, errMsg, jobID)
	}

	return err
}

// GetJob retrieves a job by ID.
func (s *Store) GetJob(ctx context.Context, id string) (*EmbeddingJob, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, symbol_id, file_path, symbol_name, content,
		       content_digest, state, priority, attempts, max_attempts, error,
		       created_at, updated_at, scheduled_at, completed_at
		FROM embedding_jobs WHERE id = ?
	`, id)

	return scanJob(row)
}

// GetEmbedding retrieves a stored embedding by symbol ID.
func (s *Store) GetEmbedding(ctx context.Context, workspaceID, symbolID string) (*EmbeddingResult, error) {
	var result EmbeddingResult
	var embeddingBytes []byte
	var createdStr string

	err := s.db.QueryRowContext(ctx, `
		SELECT symbol_id, workspace_id, file_path, embedding, content_digest, model, dimensions, created_at
		FROM symbol_embeddings WHERE workspace_id = ? AND symbol_id = ?
	`, workspaceID, symbolID).Scan(
		&result.SymbolID, &result.WorkspaceID, &result.FilePath,
		&embeddingBytes, &result.ContentDigest, &result.Model, &result.Dimensions, &createdStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	if err := json.Unmarshal(embeddingBytes, &result.Embedding); err != nil {
		return nil, fmt.Errorf("unmarshal embedding: %w", err)
	}

	result.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	return &result, nil
}

// GetEmbeddingsByFile retrieves all embeddings for a file.
func (s *Store) GetEmbeddingsByFile(ctx context.Context, workspaceID, filePath string) ([]*EmbeddingResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol_id, workspace_id, file_path, embedding, content_digest, model, dimensions, created_at
		FROM symbol_embeddings WHERE workspace_id = ? AND file_path = ?
	`, workspaceID, filePath)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	results := make([]*EmbeddingResult, 0) // Initialize as empty slice for JSON serialization
	for rows.Next() {
		var result EmbeddingResult
		var embeddingBytes []byte
		var createdStr string

		if err := rows.Scan(
			&result.SymbolID, &result.WorkspaceID, &result.FilePath,
			&embeddingBytes, &result.ContentDigest, &result.Model, &result.Dimensions, &createdStr,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		if err := json.Unmarshal(embeddingBytes, &result.Embedding); err != nil {
			return nil, fmt.Errorf("unmarshal embedding: %w", err)
		}

		result.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
		results = append(results, &result)
	}

	return results, rows.Err()
}

// Stats returns queue statistics.
func (s *Store) Stats(ctx context.Context) (*QueueStats, error) {
	stats := &QueueStats{}

	// Count jobs by state
	rows, err := s.db.QueryContext(ctx, `
		SELECT state, COUNT(*) FROM embedding_jobs GROUP BY state
	`)
	if err != nil {
		return nil, fmt.Errorf("count jobs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		switch JobState(state) {
		case StateQueued, StateRetry:
			stats.QueuedCount += count
		case StateRunning:
			stats.RunningCount = count
		case StateOK:
			stats.CompletedCount = count
		case StateError:
			stats.FailedCount = count
		}
	}

	// Count embeddings
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM symbol_embeddings`).Scan(&stats.EmbeddingsCount)
	if err != nil {
		return nil, fmt.Errorf("count embeddings: %w", err)
	}

	// Get oldest queued job
	var oldestStr sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT MIN(created_at) FROM embedding_jobs WHERE state IN ('queued', 'retry')
	`).Scan(&oldestStr)
	if err == nil && oldestStr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, oldestStr.String)
		stats.OldestQueuedAt = &t
	}

	return stats, nil
}

// Cleanup removes old completed/failed jobs.
func (s *Store) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM embedding_jobs
		WHERE state IN ('ok', 'error') AND completed_at < ?
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteEmbedding removes an embedding.
func (s *Store) DeleteEmbedding(ctx context.Context, workspaceID, symbolID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM symbol_embeddings WHERE workspace_id = ? AND symbol_id = ?
	`, workspaceID, symbolID)
	return err
}

// DeleteEmbeddingsByFile removes all embeddings for a file.
func (s *Store) DeleteEmbeddingsByFile(ctx context.Context, workspaceID, filePath string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM symbol_embeddings WHERE workspace_id = ? AND file_path = ?
	`, workspaceID, filePath)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// GetAllEmbeddings retrieves all embeddings for a workspace.
func (s *Store) GetAllEmbeddings(ctx context.Context, workspaceID string) ([]EmbeddingResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol_id, workspace_id, file_path, embedding, content_digest, model, dimensions, created_at
		FROM symbol_embeddings WHERE workspace_id = ?
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	results := make([]EmbeddingResult, 0) // Initialize as empty slice for JSON serialization
	for rows.Next() {
		var result EmbeddingResult
		var embeddingBytes []byte
		var createdStr string

		if err := rows.Scan(
			&result.SymbolID, &result.WorkspaceID, &result.FilePath,
			&embeddingBytes, &result.ContentDigest, &result.Model, &result.Dimensions, &createdStr,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		if err := json.Unmarshal(embeddingBytes, &result.Embedding); err != nil {
			return nil, fmt.Errorf("unmarshal embedding: %w", err)
		}

		result.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
		results = append(results, result)
	}

	return results, rows.Err()
}

func scanJob(row *sql.Row) (*EmbeddingJob, error) {
	var job EmbeddingJob
	var createdStr, updatedStr string
	var scheduledStr, completedStr, errStr sql.NullString

	err := row.Scan(
		&job.ID, &job.WorkspaceID, &job.SymbolID, &job.FilePath, &job.SymbolName,
		&job.Content, &job.ContentDigest, &job.State, &job.Priority, &job.Attempts,
		&job.MaxAttempts, &errStr, &createdStr, &updatedStr, &scheduledStr, &completedStr,
	)
	if err != nil {
		return nil, err
	}

	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	job.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	if scheduledStr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, scheduledStr.String)
		job.ScheduledAt = t
	}
	if completedStr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, completedStr.String)
		job.CompletedAt = &t
	}
	if errStr.Valid {
		job.Error = errStr.String
	}

	return &job, nil
}

func computeDigest(content string) string {
	h := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(h[:])
}
