package embedding

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/embeddingtext"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/queue"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
)

const embeddingQueueTable = "embedding_queue_jobs"

// ErrNotFound indicates the requested resource doesn't exist.
var ErrNotFound = errors.New("not found")

// Store manages the embedding job queue and results in SQLite.
type Store struct {
	db    *sql.DB
	queue *queue.Store
	close func() error
}

type embeddingPayload struct {
	WorkspaceID   string `json:"workspace_id"`
	SymbolID      string `json:"symbol_id"`
	FilePath      string `json:"file_path"`
	SymbolName    string `json:"symbol_name"`
	Content       string `json:"content"`
	ContentDigest string `json:"content_digest"`
}

// OpenStore opens or creates the embedding store at the given root directory and returns a Store
// for managing embedding jobs and stored embeddings.
//
// It creates or opens root/embedding_queue.db, runs necessary schema migrations, and
// initializes the internal queue store. If initialization fails, any opened resources are closed
// before returning an error. The returned Store contains a cleanup function that must be called
// via Store.Close when the store is no longer needed.
func OpenStore(ctx context.Context, root string) (*Store, error) {
	dbPath := filepath.Join(root, "embedding_queue.db")
	db, closeFn, err := sqliteutil.OpenDBShared(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("embedding: open db: %w", err)
	}

	queueStore, err := queue.NewStore(db, queue.Options{Table: embeddingQueueTable})
	if err != nil {
		_ = closeFn()
		return nil, err
	}

	return &Store{db: db, queue: queueStore, close: closeFn}, nil
}

// OpenStoreFromConfig opens the embedding store using config paths.
func OpenStoreFromConfig(ctx context.Context, cfg config.Config) (*Store, error) {
	return OpenStore(ctx, cfg.Paths.Cache)
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// migrate performs database schema migrations required by the embedding store.
// It ensures the embedding queue table exists, creates the symbol_embeddings
// table and its file-path index if they do not exist, and migrates legacy
// embedding job rows into the new queue schema.
func migrate(ctx context.Context, db *sql.DB) error {
	if err := queue.Migrate(ctx, db, queue.Options{Table: embeddingQueueTable}); err != nil {
		return err
	}

	schema := `
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

	if _, err := db.ExecContext(ctx, schema); err != nil {
		return err
	}

	return migrateLegacyJobs(ctx, db)
}

func migrateLegacyJobs(ctx context.Context, db *sql.DB) error {
	var legacyExists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='embedding_jobs'`).Scan(&legacyExists); err != nil {
		return err
	}
	if legacyExists == 0 {
		return nil
	}

	var queuedCount int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, embeddingQueueTable)).Scan(&queuedCount); err != nil {
		return err
	}
	if queuedCount > 0 {
		return nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, workspace_id, symbol_id, file_path, symbol_name, content, content_digest,
		       state, priority, attempts, max_attempts, error, created_at, updated_at,
		       scheduled_at, completed_at
		FROM embedding_jobs
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	stmt, err := db.PrepareContext(ctx, fmt.Sprintf(`
		INSERT OR IGNORE INTO %s
			(id, group_id, payload, dedupe_key, state, priority, attempts, max_attempts, error,
			 created_at, updated_at, scheduled_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, embeddingQueueTable))
	if err != nil {
		return err
	}
	defer stmt.Close()

	for rows.Next() {
		var (
			id            string
			workspaceID   string
			symbolID      string
			filePath      string
			symbolName    string
			content       string
			contentDigest string
			state         string
			priority      int
			attempts      int
			maxAttempts   int
			createdAt     string
			updatedAt     string
			errStr        sql.NullString
			scheduledStr  sql.NullString
			completedStr  sql.NullString
		)

		if err := rows.Scan(
			&id, &workspaceID, &symbolID, &filePath, &symbolName, &content, &contentDigest,
			&state, &priority, &attempts, &maxAttempts, &errStr, &createdAt, &updatedAt,
			&scheduledStr, &completedStr,
		); err != nil {
			return err
		}

		payloadBytes, err := json.Marshal(embeddingPayload{
			WorkspaceID:   workspaceID,
			SymbolID:      symbolID,
			FilePath:      filePath,
			SymbolName:    symbolName,
			Content:       content,
			ContentDigest: contentDigest,
		})
		if err != nil {
			return err
		}

		dedupeKey := dedupeKeyForSymbol(workspaceID, symbolID, contentDigest, "")

		if _, err := stmt.ExecContext(ctx,
			id, workspaceID, payloadBytes, dedupeKey, state, priority, attempts, maxAttempts,
			nullStringValue(errStr), createdAt, updatedAt, nullStringValue(scheduledStr), nullStringValue(completedStr),
		); err != nil {
			return err
		}
	}

	return rows.Err()
}

// Enqueue adds symbols to the embedding queue.
//
// Index:
// - Purpose: Queue embedding jobs with optional deduplication
// - Flow: compute digest → check existing embeddings → enqueue jobs → return counts
// - SideEffects: database reads; queue writes
// - FailureModes: marshal errors, queue errors, database errors
// - Related: queue.Store.EnqueueBatch, dedupeKeyForSymbol
// - Keywords: embedding_queue, dedupe, content_digest, workspace_id
func (s *Store) Enqueue(ctx context.Context, req EnqueueRequest) (*EnqueueResult, error) {
	result := &EnqueueResult{}
	if len(req.Symbols) == 0 {
		return result, nil
	}

	queueReqs := make([]queue.EnqueueRequest, 0, len(req.Symbols))
	model := req.Model
	for _, sym := range req.Symbols {
		contentDigest := strings.TrimSpace(sym.ContentDigest)
		if contentDigest == "" {
			contentDigest = computeDigest(sym.Content)
		}

		// Check if embedding already exists with same digest (deduplication)
		if req.Deduplicate {
			var exists bool
			var err error
			if model != "" {
				err = s.db.QueryRowContext(ctx, `
					SELECT EXISTS(
						SELECT 1 FROM symbol_embeddings
						WHERE workspace_id = ? AND symbol_id = ? AND content_digest = ? AND model = ?
					)
				`, req.WorkspaceID, sym.SymbolID, contentDigest, model).Scan(&exists)
			} else {
				err = s.db.QueryRowContext(ctx, `
					SELECT EXISTS(
						SELECT 1 FROM symbol_embeddings
						WHERE workspace_id = ? AND symbol_id = ? AND content_digest = ?
					)
				`, req.WorkspaceID, sym.SymbolID, contentDigest).Scan(&exists)
			}
			if err == nil && exists {
				result.Skipped++
				continue
			}
		}

		payloadBytes, err := json.Marshal(embeddingPayload{
			WorkspaceID:   req.WorkspaceID,
			SymbolID:      sym.SymbolID,
			FilePath:      sym.FilePath,
			SymbolName:    sym.SymbolName,
			Content:       sym.Content,
			ContentDigest: contentDigest,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}

		queueReqs = append(queueReqs, queue.EnqueueRequest{
			GroupID:   req.WorkspaceID,
			Payload:   payloadBytes,
			DedupeKey: dedupeKeyForSymbol(req.WorkspaceID, sym.SymbolID, contentDigest, model),
			Priority:  req.Priority,
		})
	}

	if len(queueReqs) == 0 {
		return result, nil
	}

	queued, err := s.queue.EnqueueBatch(ctx, queueReqs)
	if err != nil {
		return nil, err
	}
	result.Queued += queued.Queued
	result.Skipped += queued.Skipped
	result.JobIDs = append(result.JobIDs, queued.JobIDs...)

	return result, nil
}

// ClaimNext claims the next available job for processing.
func (s *Store) ClaimNext(ctx context.Context) (*EmbeddingJob, error) {
	job, err := s.queue.ClaimNext(ctx, queue.ClaimOptions{})
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}

	payload, err := decodeEmbeddingPayload(job.Payload)
	if err != nil {
		_ = s.queue.Fail(ctx, job.ID, fmt.Sprintf("decode payload: %v", err))
		return nil, err
	}

	return buildEmbeddingJob(job, payload), nil
}

// Complete stores the embedding result and marks the job as completed.
func (s *Store) Complete(ctx context.Context, jobID string, embedding []float32, model string) error {
	job, err := s.queue.GetJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, queue.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	payload, err := decodeEmbeddingPayload(job.Payload)
	if err != nil {
		return err
	}

	nowStr := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

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
	`, payload.SymbolID, payload.WorkspaceID, payload.FilePath, embeddingBytes, payload.ContentDigest, model, len(embedding), nowStr)
	if err != nil {
		return fmt.Errorf("save embedding: %w", err)
	}

	result, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET state = 'ok', updated_at = ?, completed_at = ?, error = NULL
		WHERE id = ?
	`, embeddingQueueTable), nowStr, nowStr, jobID)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("rows affected: %w", err)
	} else if affected == 0 {
		return ErrNotFound
	}

	return tx.Commit()
}

// Fail records a job failure with retry scheduling.
func (s *Store) Fail(ctx context.Context, jobID string, errMsg string) error {
	return s.queue.Fail(ctx, jobID, errMsg)
}

// GetJob fetches a job by ID.
func (s *Store) GetJob(ctx context.Context, id string) (*EmbeddingJob, error) {
	job, err := s.queue.GetJob(ctx, id)
	if err != nil {
		if errors.Is(err, queue.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	payload, err := decodeEmbeddingPayload(job.Payload)
	if err != nil {
		return nil, err
	}

	return buildEmbeddingJob(job, payload), nil
}

// GetEmbedding retrieves a stored embedding.
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

// GetContentDigest returns the latest content digest and model for a symbol, if present.
func (s *Store) GetContentDigest(ctx context.Context, workspaceID, symbolID string) (string, string, bool, error) {
	var digest string
	var model string
	err := s.db.QueryRowContext(ctx, `
		SELECT content_digest, model
		FROM symbol_embeddings WHERE workspace_id = ? AND symbol_id = ?
	`, workspaceID, symbolID).Scan(&digest, &model)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("query: %w", err)
	}
	return digest, model, true, nil
}

// GetEmbeddingsByFile retrieves embeddings for a given file.
func (s *Store) GetEmbeddingsByFile(ctx context.Context, workspaceID, filePath string) ([]*EmbeddingResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol_id, workspace_id, file_path, embedding, content_digest, model, dimensions, created_at
		FROM symbol_embeddings WHERE workspace_id = ? AND file_path = ?
	`, workspaceID, filePath)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	results := make([]*EmbeddingResult, 0)
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

// Stats summarizes the queue state.
func (s *Store) Stats(ctx context.Context) (*QueueStats, error) {
	stats := &QueueStats{}

	queueStats, err := s.queue.Stats(ctx, "")
	if err != nil {
		return nil, err
	}

	stats.QueuedCount = queueStats.QueuedCount
	stats.RunningCount = queueStats.RunningCount
	stats.CompletedCount = queueStats.CompletedCount
	stats.FailedCount = queueStats.FailedCount
	stats.OldestQueuedAt = queueStats.OldestQueuedAt

	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM symbol_embeddings`).Scan(&stats.EmbeddingsCount)
	if err != nil {
		return nil, fmt.Errorf("count embeddings: %w", err)
	}

	return stats, nil
}

// Cleanup deletes completed jobs older than the provided duration.
func (s *Store) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.queue.Cleanup(ctx, olderThan)
}

// DeleteEmbedding removes a single embedding.
func (s *Store) DeleteEmbedding(ctx context.Context, workspaceID, symbolID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM symbol_embeddings WHERE workspace_id = ? AND symbol_id = ?
	`, workspaceID, symbolID)
	return err
}

// DeleteEmbeddingsByFile removes embeddings for a file.
func (s *Store) DeleteEmbeddingsByFile(ctx context.Context, workspaceID, filePath string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM symbol_embeddings WHERE workspace_id = ? AND file_path = ?
	`, workspaceID, filePath)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// GetAllEmbeddings returns all embeddings for a workspace.
func (s *Store) GetAllEmbeddings(ctx context.Context, workspaceID string) ([]EmbeddingResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol_id, workspace_id, file_path, embedding, content_digest, model, dimensions, created_at
		FROM symbol_embeddings WHERE workspace_id = ?
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	results := make([]EmbeddingResult, 0)
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

func buildEmbeddingJob(job *queue.Job, payload embeddingPayload) *EmbeddingJob {
	return &EmbeddingJob{
		ID:            job.ID,
		WorkspaceID:   payload.WorkspaceID,
		SymbolID:      payload.SymbolID,
		FilePath:      payload.FilePath,
		SymbolName:    payload.SymbolName,
		Content:       payload.Content,
		ContentDigest: payload.ContentDigest,
		State:         job.State,
		Priority:      job.Priority,
		Attempts:      job.Attempts,
		MaxAttempts:   job.MaxAttempts,
		Error:         job.Error,
		CreatedAt:     job.CreatedAt,
		UpdatedAt:     job.UpdatedAt,
		ScheduledAt:   job.ScheduledAt,
		CompletedAt:   job.CompletedAt,
	}
}

func decodeEmbeddingPayload(data []byte) (embeddingPayload, error) {
	var payload embeddingPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return embeddingPayload{}, err
	}
	if payload.ContentDigest == "" && payload.Content != "" {
		payload.ContentDigest = computeDigest(payload.Content)
	}
	return payload, nil
}

func dedupeKeyForSymbol(workspaceID, symbolID, contentDigest, model string) string {
	if model == "" {
		return computeDigest(fmt.Sprintf("%s:%s:%s", workspaceID, symbolID, contentDigest))
	}
	return computeDigest(fmt.Sprintf("%s:%s:%s:%s", workspaceID, symbolID, model, contentDigest))
}

func nullStringValue(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func computeDigest(content string) string {
	return embeddingtext.DigestSHA256(content)
}
