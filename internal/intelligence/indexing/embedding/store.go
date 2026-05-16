package embedding

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embeddingtext"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/platform/config"
	platformsymbol "github.com/joshka0/foxctl/internal/platform/symbolutil"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/queue"
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
	Kind          embedqueue.TaskKind `json:"kind,omitempty"`
	WorkspaceID   string              `json:"workspace_id"`
	SymbolID      string              `json:"symbol_id"`
	FilePath      string              `json:"file_path"`
	SymbolName    string              `json:"symbol_name"`
	Language      string              `json:"language,omitempty"`
	PackageID     string              `json:"package_id,omitempty"`
	SymbolKey     string              `json:"symbol_key,omitempty"`
	MemoryName    string              `json:"memory_name,omitempty"`
	MemoryType    string              `json:"memory_type,omitempty"`
	Content       string              `json:"content"`
	ContentDigest string              `json:"content_digest"`
	Model         string              `json:"model,omitempty"`
}

// OpenStore opens or creates the embedding store at the given root directory and returns a Store
// for managing embedding jobs and stored embeddings.
//
// It creates or opens root/embedding_queue.db, runs necessary schema migrations, and
// initializes the internal queue store. If initialization fails, any opened resources are closed
// before returning an error. The returned Store contains a cleanup function that must be called
// via Store.Close when the store is no longer needed.
func OpenStore(ctx context.Context, root string) (*Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, embedqueue.StoreName, embedqueue.DefaultDBFile, migrate)
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

		dedupeKey := dedupeKeyForSymbol(workspaceID, SymbolInput{SymbolID: symbolID}, contentDigest, "")

		if _, err := stmt.ExecContext(
			ctx,
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
//
//	Purpose: Queue embedding jobs with optional deduplication
//	Keywords: embedding_queue, dedupe, content_digest, workspace_id
//	Related: queue.Store.EnqueueBatch, dedupeKeyForSymbol
//	Flow: compute digest → check existing embeddings → enqueue jobs → return counts
//	Resources: embedding_queue.db, symbol_embeddings table
//	Events: embedding-enqueue
//	OutputFields: EnqueueResult
//
// [[protocol:embedding-job-enqueue]]
// [[invariant:dedupe-by-content-digest]]
func (s *Store) Enqueue(ctx context.Context, req EnqueueRequest) (*EnqueueResult, error) {
	result := &EnqueueResult{}
	if len(req.Symbols) == 0 {
		return result, nil
	}

	queueReqs := make([]queue.EnqueueRequest, 0, len(req.Symbols))
	model := req.Model
	for _, sym := range req.Symbols {
		storageSymbolID := symbolStorageID(sym)
		memoryName := symbolMemoryName(req.WorkspaceID, sym)
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
				`, req.WorkspaceID, storageSymbolID, contentDigest, model).Scan(&exists)
			} else {
				err = s.db.QueryRowContext(ctx, `
					SELECT EXISTS(
						SELECT 1 FROM symbol_embeddings
						WHERE workspace_id = ? AND symbol_id = ? AND content_digest = ?
					)
				`, req.WorkspaceID, storageSymbolID, contentDigest).Scan(&exists)
			}
			if err == nil && exists {
				result.Skipped++
				continue
			}
		}

		payloadBytes, err := json.Marshal(embeddingPayload{
			Kind:          embedqueue.TaskKindSymbol,
			WorkspaceID:   req.WorkspaceID,
			SymbolID:      storageSymbolID,
			FilePath:      sym.FilePath,
			SymbolName:    sym.SymbolName,
			Language:      strings.TrimSpace(sym.Language),
			PackageID:     strings.TrimSpace(sym.PackageID),
			SymbolKey:     strings.TrimSpace(sym.SymbolKey),
			MemoryName:    memoryName,
			Content:       sym.Content,
			ContentDigest: contentDigest,
			Model:         model,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}

		queueReqs = append(queueReqs, queue.EnqueueRequest{
			GroupID:   req.WorkspaceID,
			Payload:   payloadBytes,
			DedupeKey: dedupeKeyForSymbol(req.WorkspaceID, symbolInputWithCanonicalFields(sym, storageSymbolID, memoryName), contentDigest, model),
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

// EnqueueMemories adds named memories to the embedding queue.
//
// Index:
//
//	Purpose: Queue named-memory embedding jobs for paced background processing.
//	Keywords: embedding queue, named memory, turso, qwen, paced embeddings
//	Related: Enqueue, CompleteJob, dedupeKeyForMemory
//	Flow: normalize memory content → compute digest → enqueue memory payloads
//	OutputFields: EnqueueResult
//
// [[domain:memory-embedding-queue]]
// [[invariant:memory-embedding-dedupe-by-workspace-name-model-digest]]
func (s *Store) EnqueueMemories(ctx context.Context, req MemoryEnqueueRequest) (*EnqueueResult, error) {
	result := &EnqueueResult{}
	if len(req.Memories) == 0 {
		return result, nil
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required")
	}

	queueReqs := make([]queue.EnqueueRequest, 0, len(req.Memories))
	model := strings.TrimSpace(req.Model)
	for _, mem := range req.Memories {
		name := strings.TrimSpace(mem.Name)
		if name == "" {
			return nil, fmt.Errorf("memory name is required")
		}
		content := strings.TrimSpace(mem.Content)
		if content == "" {
			result.Skipped++
			continue
		}
		contentDigest := strings.TrimSpace(mem.ContentDigest)
		if contentDigest == "" {
			contentDigest = computeDigest(content)
		}

		payloadBytes, err := json.Marshal(embeddingPayload{
			Kind:          embedqueue.TaskKindMemory,
			WorkspaceID:   workspaceID,
			MemoryName:    name,
			MemoryType:    strings.TrimSpace(mem.Type),
			Content:       content,
			ContentDigest: contentDigest,
			Model:         model,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}

		queueReqs = append(queueReqs, queue.EnqueueRequest{
			GroupID:   workspaceID,
			Payload:   payloadBytes,
			DedupeKey: dedupeKeyForMemory(workspaceID, name, contentDigest, model),
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
	return s.claimNext(ctx, "", "")
}

// ClaimNextInWorkspace claims the next available job for a workspace/group.
func (s *Store) ClaimNextInWorkspace(ctx context.Context, workspaceID string) (*EmbeddingJob, error) {
	return s.claimNext(ctx, strings.TrimSpace(workspaceID), "")
}

// ClaimNextKind claims the next available job for a task kind.
func (s *Store) ClaimNextKind(ctx context.Context, kind embedqueue.TaskKind) (*EmbeddingJob, error) {
	return s.claimNext(ctx, "", string(kind))
}

// ClaimNextInWorkspaceKind claims the next available job for a workspace/group and task kind.
func (s *Store) ClaimNextInWorkspaceKind(ctx context.Context, workspaceID string, kind embedqueue.TaskKind) (*EmbeddingJob, error) {
	return s.claimNext(ctx, strings.TrimSpace(workspaceID), string(kind))
}

func (s *Store) claimNext(ctx context.Context, workspaceID, kind string) (*EmbeddingJob, error) {
	job, err := s.queue.ClaimNext(ctx, queue.ClaimOptions{GroupID: workspaceID, PayloadKind: strings.TrimSpace(kind)})
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

// RequeueStaleRunning moves stale running jobs back to retry after a worker crash.
func (s *Store) RequeueStaleRunning(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.queue.RequeueStaleRunning(ctx, olderThan)
}

// RequeueStaleRunningKind moves stale running jobs for one task kind back to retry.
func (s *Store) RequeueStaleRunningKind(ctx context.Context, kind embedqueue.TaskKind, olderThan time.Duration) (int64, error) {
	return s.queue.RequeueStaleRunningForKind(ctx, olderThan, string(kind))
}

// RequeueStaleRunningInWorkspace moves stale running jobs for one workspace/group back to retry.
func (s *Store) RequeueStaleRunningInWorkspace(ctx context.Context, workspaceID string, olderThan time.Duration) (int64, error) {
	return s.queue.RequeueStaleRunningForGroup(ctx, olderThan, strings.TrimSpace(workspaceID))
}

// RequeueStaleRunningInWorkspaceKind moves stale running jobs for one workspace/group and task kind back to retry.
func (s *Store) RequeueStaleRunningInWorkspaceKind(ctx context.Context, workspaceID string, kind embedqueue.TaskKind, olderThan time.Duration) (int64, error) {
	return s.queue.RequeueStaleRunningForGroupKind(ctx, olderThan, strings.TrimSpace(workspaceID), string(kind))
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

// CompleteJob marks a queued job complete when the embedding result is stored externally.
func (s *Store) CompleteJob(ctx context.Context, jobID string) error {
	return s.queue.Complete(ctx, jobID)
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
	return s.stats(ctx, "", "")
}

// StatsInWorkspace summarizes the queue state for one workspace/group.
func (s *Store) StatsInWorkspace(ctx context.Context, workspaceID string) (*QueueStats, error) {
	return s.stats(ctx, strings.TrimSpace(workspaceID), "")
}

// StatsKind summarizes queue state for one task kind.
func (s *Store) StatsKind(ctx context.Context, kind embedqueue.TaskKind) (*QueueStats, error) {
	return s.stats(ctx, "", string(kind))
}

// StatsInWorkspaceKind summarizes queue state for one workspace/group and task kind.
func (s *Store) StatsInWorkspaceKind(ctx context.Context, workspaceID string, kind embedqueue.TaskKind) (*QueueStats, error) {
	return s.stats(ctx, strings.TrimSpace(workspaceID), string(kind))
}

func (s *Store) stats(ctx context.Context, workspaceID, kind string) (*QueueStats, error) {
	stats := &QueueStats{}

	var queueStats *queue.Stats
	var err error
	if strings.TrimSpace(kind) == "" {
		queueStats, err = s.queue.Stats(ctx, workspaceID)
	} else {
		queueStats, err = s.queue.StatsForKind(ctx, workspaceID, kind)
	}
	if err != nil {
		return nil, err
	}

	stats.QueuedCount = queueStats.QueuedCount
	stats.RunningCount = queueStats.RunningCount
	stats.CompletedCount = queueStats.CompletedCount
	stats.FailedCount = queueStats.FailedCount
	stats.OldestQueuedAt = queueStats.OldestQueuedAt

	if embedqueue.TaskKind(strings.TrimSpace(kind)) == embedqueue.TaskKindMemory {
		return stats, nil
	}

	if workspaceID == "" {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM symbol_embeddings`).Scan(&stats.EmbeddingsCount)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM symbol_embeddings WHERE workspace_id = ?`, workspaceID).Scan(&stats.EmbeddingsCount)
	}
	if err != nil {
		return nil, fmt.Errorf("count embeddings: %w", err)
	}

	return stats, nil
}

// Cleanup deletes completed jobs older than the provided duration.
func (s *Store) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.queue.Cleanup(ctx, olderThan)
}

// CleanupInWorkspace deletes completed jobs for one workspace/group.
func (s *Store) CleanupInWorkspace(ctx context.Context, workspaceID string, olderThan time.Duration) (int64, error) {
	return s.queue.CleanupForGroup(ctx, olderThan, strings.TrimSpace(workspaceID))
}

// CleanupKind deletes completed jobs for one task kind.
func (s *Store) CleanupKind(ctx context.Context, kind embedqueue.TaskKind, olderThan time.Duration) (int64, error) {
	return s.queue.CleanupForKind(ctx, olderThan, string(kind))
}

// CleanupInWorkspaceKind deletes completed jobs for one workspace/group and task kind.
func (s *Store) CleanupInWorkspaceKind(ctx context.Context, workspaceID string, kind embedqueue.TaskKind, olderThan time.Duration) (int64, error) {
	return s.queue.CleanupForGroupKind(ctx, olderThan, strings.TrimSpace(workspaceID), string(kind))
}

// Purge deletes queued job records matching the optional workspace and kind filters.
func (s *Store) Purge(ctx context.Context, workspaceID string, kind embedqueue.TaskKind) (int64, error) {
	return s.queue.Purge(ctx, strings.TrimSpace(workspaceID), string(kind))
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
		Kind:          payload.Kind,
		WorkspaceID:   payload.WorkspaceID,
		SymbolID:      payload.SymbolID,
		FilePath:      payload.FilePath,
		SymbolName:    payload.SymbolName,
		Language:      payload.Language,
		PackageID:     payload.PackageID,
		SymbolKey:     payload.SymbolKey,
		MemoryName:    payload.MemoryName,
		MemoryType:    payload.MemoryType,
		Content:       payload.Content,
		ContentDigest: payload.ContentDigest,
		Model:         payload.Model,
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
	if payload.Kind == "" {
		payload.Kind = embedqueue.TaskKindSymbol
	}
	return payload, nil
}

func dedupeKeyForSymbol(workspaceID string, sym SymbolInput, contentDigest, model string) string {
	return embedqueue.StableDedupeKey(
		string(embedqueue.TaskKindSymbol),
		workspaceID,
		symbolDedupeIdentity(sym),
		model,
		contentDigest,
	)
}

func symbolInputWithCanonicalFields(sym SymbolInput, storageSymbolID, memoryName string) SymbolInput {
	sym.SymbolID = storageSymbolID
	sym.MemoryName = memoryName
	return sym
}

func symbolStorageID(sym SymbolInput) string {
	packageID := strings.TrimSpace(sym.PackageID)
	symbolKey := strings.TrimSpace(sym.SymbolKey)
	if packageID != "" && symbolKey != "" {
		return platformsymbol.ScopedSymbolID(packageID, symbolKey)
	}
	return strings.TrimSpace(sym.SymbolID)
}

func symbolMemoryName(workspaceID string, sym SymbolInput) string {
	if name := strings.TrimSpace(sym.MemoryName); name != "" {
		return name
	}
	workspaceID = strings.TrimSpace(workspaceID)
	packageID := strings.TrimSpace(sym.PackageID)
	symbolKey := strings.TrimSpace(sym.SymbolKey)
	if workspaceID != "" && packageID != "" && symbolKey != "" {
		return platformsymbol.KeyEntryName(workspaceID, packageID, symbolKey)
	}
	return ""
}

func symbolDedupeIdentity(sym SymbolInput) string {
	if name := strings.TrimSpace(sym.MemoryName); name != "" {
		return name
	}
	language := strings.TrimSpace(sym.Language)
	packageID := strings.TrimSpace(sym.PackageID)
	symbolKey := strings.TrimSpace(sym.SymbolKey)
	if packageID != "" && symbolKey != "" {
		return strings.Join([]string{language, packageID, symbolKey}, "\x00")
	}
	return strings.TrimSpace(sym.SymbolID)
}

func dedupeKeyForMemory(workspaceID, name, contentDigest, model string) string {
	return embedqueue.StableDedupeKey(
		string(embedqueue.TaskKindMemory),
		workspaceID,
		name,
		model,
		contentDigest,
	)
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
