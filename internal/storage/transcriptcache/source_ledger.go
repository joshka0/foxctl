package transcriptcache

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

// SourceState is the canonical transcript dream processing state.
type SourceState string

const (
	SourceStateDiscovered SourceState = "discovered"
	SourceStateQueued     SourceState = "queued"
	SourceStateProcessing SourceState = "processing"
	SourceStateProcessed  SourceState = "processed"
	SourceStateFailed     SourceState = "failed"
)

const DefaultSourceMaxAttempts = 3

// SourceIdentity is the stable dedupe key for a transcript source.
type SourceIdentity struct {
	Provider   string
	SourcePath string
}

// SourceDiscovery is one discovered transcript source candidate.
type SourceDiscovery struct {
	Provider      string
	SourcePath    string
	SessionID     string
	WorkspacePath string
	SizeBytes     int64
	ModTime       time.Time
	SourceDigest  string
	MaxAttempts   int
}

// SourceRecord is the durable processing ledger row for one transcript source.
type SourceRecord struct {
	Provider      string
	SourcePath    string
	SessionID     string
	WorkspacePath string
	SizeBytes     int64
	ModTime       time.Time
	SourceDigest  string
	State         SourceState
	Attempts      int
	MaxAttempts   int
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	QueuedAt      *time.Time
	ProcessingAt  *time.Time
	ProcessedAt   *time.Time
	FailedAt      *time.Time
}

// SourceWorkOptions controls candidate listing.
type SourceWorkOptions struct {
	Limit int
}

// UpsertDiscoveredSource records a discovered transcript source without
// duplicating rediscovery of the same provider/path.
func (s *Store) UpsertDiscoveredSource(ctx context.Context, discovery SourceDiscovery) (SourceRecord, error) {
	discovery.Provider = strings.TrimSpace(discovery.Provider)
	discovery.SourcePath = strings.TrimSpace(discovery.SourcePath)
	discovery.SessionID = strings.TrimSpace(discovery.SessionID)
	discovery.WorkspacePath = strings.TrimSpace(discovery.WorkspacePath)
	discovery.SourceDigest = strings.TrimSpace(discovery.SourceDigest)
	if discovery.Provider == "" {
		return SourceRecord{}, fmt.Errorf("transcriptcache: source provider is required")
	}
	if discovery.SourcePath == "" {
		return SourceRecord{}, fmt.Errorf("transcriptcache: source path is required")
	}
	if discovery.SourceDigest == "" {
		return SourceRecord{}, fmt.Errorf("transcriptcache: source digest is required")
	}
	if discovery.MaxAttempts <= 0 {
		discovery.MaxAttempts = DefaultSourceMaxAttempts
	}

	now := sqlutil.FormatTimestamp(time.Now().UTC())
	modTime := sqlutil.FormatTimestamp(discovery.ModTime)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO transcript_source_ledger (
			provider, source_path, session_id, workspace_path, size_bytes, mod_time,
			source_digest, state, attempts, max_attempts, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 'discovered', 0, $8, $9, $9)
		ON CONFLICT(provider, source_path) DO UPDATE SET
			session_id = excluded.session_id,
			workspace_path = excluded.workspace_path,
			size_bytes = excluded.size_bytes,
			mod_time = excluded.mod_time,
			source_digest = excluded.source_digest,
			max_attempts = max(max_attempts, excluded.max_attempts, attempts),
			updated_at = excluded.updated_at
	`, discovery.Provider, discovery.SourcePath, discovery.SessionID, discovery.WorkspacePath,
		discovery.SizeBytes, modTime, discovery.SourceDigest, discovery.MaxAttempts, now)
	if err != nil {
		return SourceRecord{}, fmt.Errorf("transcriptcache: upsert discovered source: %w", err)
	}
	return s.GetSource(ctx, SourceIdentity{Provider: discovery.Provider, SourcePath: discovery.SourcePath})
}

// MarkSourceQueued marks a discovered or failed source ready for work.
func (s *Store) MarkSourceQueued(ctx context.Context, id SourceIdentity) (SourceRecord, error) {
	id = normalizeSourceIdentity(id)
	record, err := s.ensureSourceStateIn(ctx, id, SourceStateQueued, SourceStateDiscovered, SourceStateFailed)
	if err != nil {
		return SourceRecord{}, err
	}
	if err := ensureSourceRetryBudget(record, SourceStateQueued); err != nil {
		return SourceRecord{}, err
	}
	return s.markSourceState(ctx, id, SourceStateQueued, "queued_at", "", false)
}

// MarkSourceProcessing marks a source as actively being processed.
func (s *Store) MarkSourceProcessing(ctx context.Context, id SourceIdentity) (SourceRecord, error) {
	id = normalizeSourceIdentity(id)
	record, err := s.ensureSourceStateIn(ctx, id, SourceStateProcessing, SourceStateDiscovered, SourceStateQueued, SourceStateFailed)
	if err != nil {
		return SourceRecord{}, err
	}
	if err := ensureSourceRetryBudget(record, SourceStateProcessing); err != nil {
		return SourceRecord{}, err
	}
	return s.markSourceState(ctx, id, SourceStateProcessing, "processing_at", "", false)
}

// MarkSourceProcessed marks a source complete and clears retry metadata.
func (s *Store) MarkSourceProcessed(ctx context.Context, id SourceIdentity) (SourceRecord, error) {
	id = normalizeSourceIdentity(id)
	if _, err := s.ensureSourceStateIn(ctx, id, SourceStateProcessed, SourceStateProcessing); err != nil {
		return SourceRecord{}, err
	}
	return s.markSourceState(ctx, id, SourceStateProcessed, "processed_at", ", last_error = NULL", true)
}

// MarkSourceFailed records a bounded processing failure.
func (s *Store) MarkSourceFailed(ctx context.Context, id SourceIdentity, errText string) (SourceRecord, error) {
	id = normalizeSourceIdentity(id)
	if id.Provider == "" {
		return SourceRecord{}, fmt.Errorf("transcriptcache: source provider is required")
	}
	if id.SourcePath == "" {
		return SourceRecord{}, fmt.Errorf("transcriptcache: source path is required")
	}
	if _, err := s.ensureSourceStateIn(ctx, id, SourceStateFailed, SourceStateProcessing); err != nil {
		return SourceRecord{}, err
	}

	now := sqlutil.FormatTimestamp(time.Now().UTC())
	row := s.db.QueryRowContext(ctx, `
		UPDATE transcript_source_ledger
		SET state = 'failed',
		    attempts = CASE WHEN attempts < max_attempts THEN attempts + 1 ELSE attempts END,
		    last_error = $3,
		    failed_at = $4,
		    updated_at = $4
		WHERE provider = $1 AND source_path = $2
		RETURNING provider, source_path, session_id, workspace_path, size_bytes, mod_time,
		          source_digest, state, attempts, max_attempts, last_error, created_at, updated_at,
		          queued_at, processing_at, processed_at, failed_at
	`, id.Provider, id.SourcePath, strings.TrimSpace(errText), now)
	record, err := scanSourceRecord(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return SourceRecord{}, fmt.Errorf("transcriptcache: source not found")
		}
		return SourceRecord{}, fmt.Errorf("transcriptcache: mark source failed: %w", err)
	}
	return record, nil
}

// ListSourcesNeedingWork returns retryable source rows in deterministic order.
func (s *Store) ListSourcesNeedingWork(ctx context.Context, opts SourceWorkOptions) ([]SourceRecord, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, source_path, session_id, workspace_path, size_bytes, mod_time,
		       source_digest, state, attempts, max_attempts, last_error, created_at, updated_at,
		       queued_at, processing_at, processed_at, failed_at
		FROM transcript_source_ledger
		WHERE state IN ('queued', 'discovered', 'failed')
		  AND attempts < max_attempts
		ORDER BY
		  CASE state WHEN 'queued' THEN 0 WHEN 'discovered' THEN 1 WHEN 'failed' THEN 2 ELSE 3 END,
		  provider ASC,
		  source_path ASC,
		  updated_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("transcriptcache: list source work: %w", err)
	}
	defer rows.Close()

	var records []SourceRecord
	for rows.Next() {
		record, err := scanSourceRecordRows(rows)
		if err != nil {
			return nil, fmt.Errorf("transcriptcache: scan source work: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("transcriptcache: iterate source work: %w", err)
	}
	return records, nil
}

// GetSource fetches a source ledger row by its canonical identity.
func (s *Store) GetSource(ctx context.Context, id SourceIdentity) (SourceRecord, error) {
	id = normalizeSourceIdentity(id)
	row := s.db.QueryRowContext(ctx, `
		SELECT provider, source_path, session_id, workspace_path, size_bytes, mod_time,
		       source_digest, state, attempts, max_attempts, last_error, created_at, updated_at,
		       queued_at, processing_at, processed_at, failed_at
		FROM transcript_source_ledger
		WHERE provider = $1 AND source_path = $2
	`, id.Provider, id.SourcePath)
	record, err := scanSourceRecord(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return SourceRecord{}, fmt.Errorf("transcriptcache: source not found")
		}
		return SourceRecord{}, fmt.Errorf("transcriptcache: get source: %w", err)
	}
	return record, nil
}

func (s *Store) markSourceState(ctx context.Context, id SourceIdentity, state SourceState, timestampColumn, extraSet string, allowProcessed bool) (SourceRecord, error) {
	id = normalizeSourceIdentity(id)
	if id.Provider == "" {
		return SourceRecord{}, fmt.Errorf("transcriptcache: source provider is required")
	}
	if id.SourcePath == "" {
		return SourceRecord{}, fmt.Errorf("transcriptcache: source path is required")
	}
	if !allowProcessed {
		if err := s.ensureSourceCanLeaveTerminal(ctx, id, string(state)); err != nil {
			return SourceRecord{}, err
		}
	}
	now := sqlutil.FormatTimestamp(time.Now().UTC())
	query := fmt.Sprintf(`
		UPDATE transcript_source_ledger
		SET state = $3,
		    updated_at = $4,
		    %s = $4%s
		WHERE provider = $1 AND source_path = $2
		RETURNING provider, source_path, session_id, workspace_path, size_bytes, mod_time,
		          source_digest, state, attempts, max_attempts, last_error, created_at, updated_at,
		          queued_at, processing_at, processed_at, failed_at
	`, timestampColumn, extraSet)
	row := s.db.QueryRowContext(ctx, query, id.Provider, id.SourcePath, string(state), now)
	record, err := scanSourceRecord(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return SourceRecord{}, fmt.Errorf("transcriptcache: source not found")
		}
		return SourceRecord{}, fmt.Errorf("transcriptcache: mark source %s: %w", state, err)
	}
	return record, nil
}

func (s *Store) ensureSourceCanLeaveTerminal(ctx context.Context, id SourceIdentity, nextState string) error {
	record, err := s.GetSource(ctx, id)
	if err != nil {
		return err
	}
	if record.State == SourceStateProcessed {
		return fmt.Errorf("transcriptcache: processed source cannot transition to %s", nextState)
	}
	return nil
}

func (s *Store) ensureSourceStateIn(ctx context.Context, id SourceIdentity, nextState SourceState, allowed ...SourceState) (SourceRecord, error) {
	if id.Provider == "" {
		return SourceRecord{}, fmt.Errorf("transcriptcache: source provider is required")
	}
	if id.SourcePath == "" {
		return SourceRecord{}, fmt.Errorf("transcriptcache: source path is required")
	}
	record, err := s.GetSource(ctx, id)
	if err != nil {
		return SourceRecord{}, err
	}
	for _, state := range allowed {
		if record.State == state {
			return record, nil
		}
	}
	return SourceRecord{}, fmt.Errorf("transcriptcache: source cannot transition from %s to %s", record.State, nextState)
}

func ensureSourceRetryBudget(record SourceRecord, nextState SourceState) error {
	if record.State == SourceStateFailed && record.Attempts >= record.MaxAttempts {
		return fmt.Errorf("transcriptcache: exhausted source cannot transition to %s", nextState)
	}
	return nil
}

func normalizeSourceIdentity(id SourceIdentity) SourceIdentity {
	return SourceIdentity{
		Provider:   strings.TrimSpace(id.Provider),
		SourcePath: strings.TrimSpace(id.SourcePath),
	}
}

func scanSourceRecord(row *sql.Row) (SourceRecord, error) {
	var record SourceRecord
	var modTime, createdAt, updatedAt string
	var lastError, queuedAt, processingAt, processedAt, failedAt sql.NullString
	err := row.Scan(
		&record.Provider, &record.SourcePath, &record.SessionID, &record.WorkspacePath,
		&record.SizeBytes, &modTime, &record.SourceDigest, &record.State, &record.Attempts,
		&record.MaxAttempts, &lastError, &createdAt, &updatedAt, &queuedAt, &processingAt,
		&processedAt, &failedAt,
	)
	if err != nil {
		return SourceRecord{}, err
	}
	return finishSourceRecord(record, modTime, createdAt, updatedAt, lastError, queuedAt, processingAt, processedAt, failedAt)
}

func scanSourceRecordRows(rows *sql.Rows) (SourceRecord, error) {
	var record SourceRecord
	var modTime, createdAt, updatedAt string
	var lastError, queuedAt, processingAt, processedAt, failedAt sql.NullString
	err := rows.Scan(
		&record.Provider, &record.SourcePath, &record.SessionID, &record.WorkspacePath,
		&record.SizeBytes, &modTime, &record.SourceDigest, &record.State, &record.Attempts,
		&record.MaxAttempts, &lastError, &createdAt, &updatedAt, &queuedAt, &processingAt,
		&processedAt, &failedAt,
	)
	if err != nil {
		return SourceRecord{}, err
	}
	return finishSourceRecord(record, modTime, createdAt, updatedAt, lastError, queuedAt, processingAt, processedAt, failedAt)
}

func finishSourceRecord(record SourceRecord, modTime, createdAt, updatedAt string, lastError, queuedAt, processingAt, processedAt, failedAt sql.NullString) (SourceRecord, error) {
	var err error
	if record.ModTime, err = sqlutil.ScanTimestamp(modTime); err != nil {
		return SourceRecord{}, err
	}
	if record.CreatedAt, err = sqlutil.ScanTimestamp(createdAt); err != nil {
		return SourceRecord{}, err
	}
	if record.UpdatedAt, err = sqlutil.ScanTimestamp(updatedAt); err != nil {
		return SourceRecord{}, err
	}
	if lastError.Valid {
		record.LastError = lastError.String
	}
	if record.QueuedAt, err = scanSourceNullableTime(queuedAt); err != nil {
		return SourceRecord{}, err
	}
	if record.ProcessingAt, err = scanSourceNullableTime(processingAt); err != nil {
		return SourceRecord{}, err
	}
	if record.ProcessedAt, err = scanSourceNullableTime(processedAt); err != nil {
		return SourceRecord{}, err
	}
	if record.FailedAt, err = scanSourceNullableTime(failedAt); err != nil {
		return SourceRecord{}, err
	}
	return record, nil
}

func scanSourceNullableTime(src sql.NullString) (*time.Time, error) {
	if !src.Valid {
		return nil, nil
	}
	t, err := sqlutil.ScanTimestamp(src.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
