package transcriptcache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SourceState string

const (
	SourceStateDiscovered SourceState = "discovered"
	SourceStateQueued     SourceState = "queued"
	SourceStateProcessing SourceState = "processing"
	SourceStateProcessed  SourceState = "processed"
	SourceStateFailed     SourceState = "failed"
)

const DefaultSourceMaxAttempts = 3

type SourceRecord struct {
	Provider      string
	SourcePath    string
	SessionID     string
	WorkspaceHint string
	SourceSize    int64
	SourceMTime   time.Time
	Fingerprint   string
	State         SourceState
	Attempts      int
	MaxAttempts   int
	LastError     string
	NextAttemptAt *time.Time
	DiscoveredAt  time.Time
	QueuedAt      *time.Time
	ProcessingAt  *time.Time
	ProcessedAt   *time.Time
	FailedAt      *time.Time
	UpdatedAt     time.Time
}

type SourceDiscovery struct {
	Provider      string
	SourcePath    string
	SessionID     string
	WorkspaceHint string
	SourceSize    int64
	SourceMTime   time.Time
	Fingerprint   string
	MaxAttempts   int
}

type SourceFailure struct {
	Provider    string
	SourcePath  string
	Error       string
	RetryAfter  time.Duration
	MaxAttempts int
	Now         time.Time
}

type ListSourceCandidatesOptions struct {
	Limit int
	Now   time.Time
}

type ResetStaleProcessingOptions struct {
	Before time.Time
	Now    time.Time
	Error  string
}

func (s *Store) UpsertDiscoveredSource(ctx context.Context, discovery SourceDiscovery) (SourceRecord, error) {
	if err := validateDiscovery(discovery); err != nil {
		return SourceRecord{}, err
	}
	now := time.Now().UTC()
	maxAttempts := discovery.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultSourceMaxAttempts
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO transcript_processed_sources (
			provider, source_path, session_id, workspace_hint, source_size, source_mtime,
			fingerprint, state, attempts, max_attempts, discovered_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 0, $9, $10, $10)
		ON CONFLICT(provider, source_path) DO UPDATE SET
			session_id = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.session_id
				ELSE excluded.session_id
			END,
			workspace_hint = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.workspace_hint
				ELSE excluded.workspace_hint
			END,
			source_size = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.source_size
				ELSE excluded.source_size
			END,
			source_mtime = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.source_mtime
				ELSE excluded.source_mtime
			END,
			fingerprint = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.fingerprint
				ELSE excluded.fingerprint
			END,
			state = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.state
				WHEN transcript_processed_sources.fingerprint = excluded.fingerprint
					THEN transcript_processed_sources.state
				ELSE excluded.state
			END,
			attempts = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.attempts
				WHEN transcript_processed_sources.fingerprint = excluded.fingerprint
					THEN transcript_processed_sources.attempts
				ELSE 0
			END,
			max_attempts = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.max_attempts
				ELSE excluded.max_attempts
			END,
			last_error = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.last_error
				WHEN transcript_processed_sources.fingerprint = excluded.fingerprint
					THEN transcript_processed_sources.last_error
				ELSE NULL
			END,
			next_attempt_at = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.next_attempt_at
				WHEN transcript_processed_sources.fingerprint = excluded.fingerprint
					THEN transcript_processed_sources.next_attempt_at
				ELSE NULL
			END,
			queued_at = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.queued_at
				WHEN transcript_processed_sources.fingerprint = excluded.fingerprint
					THEN transcript_processed_sources.queued_at
				ELSE NULL
			END,
			processing_at = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.processing_at
				WHEN transcript_processed_sources.fingerprint = excluded.fingerprint
					THEN transcript_processed_sources.processing_at
				ELSE NULL
			END,
			processed_at = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.processed_at
				WHEN transcript_processed_sources.fingerprint = excluded.fingerprint
					THEN transcript_processed_sources.processed_at
				ELSE NULL
			END,
			failed_at = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.failed_at
				WHEN transcript_processed_sources.fingerprint = excluded.fingerprint
					THEN transcript_processed_sources.failed_at
				ELSE NULL
			END,
			updated_at = CASE
				WHEN transcript_processed_sources.state = 'processing'
					THEN transcript_processed_sources.updated_at
				ELSE excluded.updated_at
			END
	`, strings.TrimSpace(discovery.Provider), strings.TrimSpace(discovery.SourcePath), strings.TrimSpace(discovery.SessionID),
		strings.TrimSpace(discovery.WorkspaceHint), discovery.SourceSize, formatSourceTime(discovery.SourceMTime),
		strings.TrimSpace(discovery.Fingerprint), SourceStateDiscovered, maxAttempts, formatSourceTime(now))
	if err != nil {
		return SourceRecord{}, fmt.Errorf("transcriptcache: upsert discovered source: %w", err)
	}
	return s.GetSource(ctx, discovery.Provider, discovery.SourcePath)
}

func (s *Store) MarkSourceQueued(ctx context.Context, provider, sourcePath string) error {
	return s.markSourceState(ctx, provider, sourcePath, SourceStateQueued)
}

func (s *Store) MarkSourceProcessing(ctx context.Context, provider, sourcePath string) error {
	provider, sourcePath, err := normalizeSourceKey(provider, sourcePath)
	if err != nil {
		return err
	}
	now := formatSourceTime(time.Now().UTC())
	res, err := s.db.ExecContext(ctx, `
		UPDATE transcript_processed_sources
		SET state = $3,
		    processing_at = $4,
		    next_attempt_at = NULL,
		    updated_at = $4
		WHERE provider = $1
		  AND source_path = $2
		  AND (
		    state = $5
		    OR (
		      state = $6
		      AND attempts < max_attempts
		      AND (next_attempt_at IS NULL OR next_attempt_at <= $4)
		    )
		  )
	`, provider, sourcePath, SourceStateProcessing, now, SourceStateQueued, SourceStateFailed)
	if err != nil {
		return fmt.Errorf("transcriptcache: mark source processing: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("transcriptcache: source not found or invalid transition")
	}
	return nil
}

func (s *Store) MarkSourceProcessed(ctx context.Context, provider, sourcePath string) error {
	return s.markSourceState(ctx, provider, sourcePath, SourceStateProcessed)
}

func (s *Store) MarkSourceFailed(ctx context.Context, failure SourceFailure) (SourceRecord, error) {
	provider, sourcePath, err := normalizeSourceKey(failure.Provider, failure.SourcePath)
	if err != nil {
		return SourceRecord{}, err
	}
	maxAttempts := failure.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultSourceMaxAttempts
	}
	now := failure.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	retryAt := sql.NullString{}
	if failure.RetryAfter > 0 {
		retryAt = sql.NullString{String: formatSourceTime(now.Add(failure.RetryAfter)), Valid: true}
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE transcript_processed_sources
		SET state = $3,
		    attempts = CASE
		        WHEN attempts < $4 THEN attempts + 1
		        ELSE attempts
		    END,
		    max_attempts = $4,
		    last_error = $5,
		    next_attempt_at = CASE
		        WHEN attempts + 1 < $4 THEN $6
		        ELSE NULL
		    END,
		    failed_at = $7,
		    updated_at = $7
		WHERE provider = $1
		  AND source_path = $2
		  AND state = $8
	`, provider, sourcePath, SourceStateFailed, maxAttempts, strings.TrimSpace(failure.Error), retryAt, formatSourceTime(now), SourceStateProcessing)
	if err != nil {
		return SourceRecord{}, fmt.Errorf("transcriptcache: mark source failed: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return SourceRecord{}, fmt.Errorf("transcriptcache: source not found or invalid transition")
	}
	return s.GetSource(ctx, provider, sourcePath)
}

func (s *Store) ResetStaleProcessingSources(ctx context.Context, opts ResetStaleProcessingOptions) (int, error) {
	if opts.Before.IsZero() {
		return 0, fmt.Errorf("transcriptcache: stale processing cutoff is required")
	}
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	message := strings.TrimSpace(opts.Error)
	if message == "" {
		message = "processing timed out"
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE transcript_processed_sources
		SET state = $1,
		    attempts = CASE
		        WHEN attempts < max_attempts THEN attempts + 1
		        ELSE attempts
		    END,
		    last_error = $2,
		    next_attempt_at = NULL,
		    failed_at = $3,
		    updated_at = $3
		WHERE state = $4
		  AND processing_at IS NOT NULL
		  AND processing_at <= $5
	`, SourceStateFailed, message, formatSourceTime(now), SourceStateProcessing, formatSourceTime(opts.Before))
	if err != nil {
		return 0, fmt.Errorf("transcriptcache: reset stale processing sources: %w", err)
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

func (s *Store) ListSourceCandidates(ctx context.Context, opts ListSourceCandidatesOptions) ([]SourceRecord, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, source_path, session_id, workspace_hint, source_size, source_mtime,
		       fingerprint, state, attempts, max_attempts, last_error, next_attempt_at,
		       discovered_at, queued_at, processing_at, processed_at, failed_at, updated_at
		FROM transcript_processed_sources
		WHERE state = $1
		   OR (state = $2 AND attempts < max_attempts AND (next_attempt_at IS NULL OR next_attempt_at <= $3))
		ORDER BY provider ASC, source_path ASC
		LIMIT $4
	`, SourceStateQueued, SourceStateFailed, formatSourceTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("transcriptcache: list source candidates: %w", err)
	}
	defer rows.Close()

	var records []SourceRecord
	for rows.Next() {
		record, err := scanSourceRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("transcriptcache: list source candidates rows: %w", err)
	}
	return records, nil
}

func (s *Store) GetSource(ctx context.Context, provider, sourcePath string) (SourceRecord, error) {
	provider, sourcePath, err := normalizeSourceKey(provider, sourcePath)
	if err != nil {
		return SourceRecord{}, err
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT provider, source_path, session_id, workspace_hint, source_size, source_mtime,
		       fingerprint, state, attempts, max_attempts, last_error, next_attempt_at,
		       discovered_at, queued_at, processing_at, processed_at, failed_at, updated_at
		FROM transcript_processed_sources
		WHERE provider = $1
		  AND source_path = $2
	`, provider, sourcePath)
	record, err := scanSourceRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SourceRecord{}, fmt.Errorf("transcriptcache: source not found")
		}
		return SourceRecord{}, err
	}
	return record, nil
}

func (s *Store) markSourceState(ctx context.Context, provider, sourcePath string, state SourceState) error {
	provider, sourcePath, err := normalizeSourceKey(provider, sourcePath)
	if err != nil {
		return err
	}
	column, allowed, ok := sourceTransitionSpec(state)
	if !ok {
		return fmt.Errorf("transcriptcache: unsupported source state transition %q", state)
	}
	now := formatSourceTime(time.Now().UTC())
	stateClause := "state = $5"
	args := []any{provider, sourcePath, state, now}
	for _, item := range allowed {
		args = append(args, item)
	}
	if len(allowed) == 2 {
		stateClause = "state IN ($5, $6)"
	}
	query := fmt.Sprintf(`
		UPDATE transcript_processed_sources
		SET state = $3,
		    %s = $4,
		    updated_at = $4
		WHERE provider = $1
		  AND source_path = $2
		  AND %s
	`, column, stateClause)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("transcriptcache: mark source %s: %w", state, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("transcriptcache: source not found or invalid transition")
	}
	return nil
}

func sourceTransitionSpec(state SourceState) (string, []SourceState, bool) {
	switch state {
	case SourceStateQueued:
		return "queued_at", []SourceState{SourceStateDiscovered}, true
	case SourceStateProcessed:
		return "processed_at", []SourceState{SourceStateProcessing}, true
	default:
		return "", nil, false
	}
}

type sourceScanner interface {
	Scan(dest ...any) error
}

func scanSourceRecord(scanner sourceScanner) (SourceRecord, error) {
	var record SourceRecord
	var sessionID, workspaceHint, lastError sql.NullString
	var nextAttemptAt, queuedAt, processingAt, processedAt, failedAt sql.NullString
	var sourceMTime, discoveredAt, updatedAt string
	if err := scanner.Scan(
		&record.Provider,
		&record.SourcePath,
		&sessionID,
		&workspaceHint,
		&record.SourceSize,
		&sourceMTime,
		&record.Fingerprint,
		&record.State,
		&record.Attempts,
		&record.MaxAttempts,
		&lastError,
		&nextAttemptAt,
		&discoveredAt,
		&queuedAt,
		&processingAt,
		&processedAt,
		&failedAt,
		&updatedAt,
	); err != nil {
		return SourceRecord{}, err
	}
	record.SessionID = sessionID.String
	record.WorkspaceHint = workspaceHint.String
	record.LastError = lastError.String
	record.SourceMTime = parseSourceTime(sourceMTime)
	record.NextAttemptAt = parseOptionalSourceTime(nextAttemptAt)
	record.DiscoveredAt = parseSourceTime(discoveredAt)
	record.QueuedAt = parseOptionalSourceTime(queuedAt)
	record.ProcessingAt = parseOptionalSourceTime(processingAt)
	record.ProcessedAt = parseOptionalSourceTime(processedAt)
	record.FailedAt = parseOptionalSourceTime(failedAt)
	record.UpdatedAt = parseSourceTime(updatedAt)
	return record, nil
}

func validateDiscovery(discovery SourceDiscovery) error {
	if _, _, err := normalizeSourceKey(discovery.Provider, discovery.SourcePath); err != nil {
		return err
	}
	if discovery.SourceSize < 0 {
		return fmt.Errorf("transcriptcache: source_size must be non-negative")
	}
	if discovery.SourceMTime.IsZero() {
		return fmt.Errorf("transcriptcache: source_mtime is required")
	}
	if strings.TrimSpace(discovery.Fingerprint) == "" {
		return fmt.Errorf("transcriptcache: fingerprint is required")
	}
	return nil
}

func normalizeSourceKey(provider, sourcePath string) (string, string, error) {
	provider = strings.TrimSpace(provider)
	sourcePath = strings.TrimSpace(sourcePath)
	if provider == "" {
		return "", "", fmt.Errorf("transcriptcache: provider is required")
	}
	if sourcePath == "" {
		return "", "", fmt.Errorf("transcriptcache: source_path is required")
	}
	return provider, sourcePath, nil
}

func formatSourceTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseSourceTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func parseOptionalSourceTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed := parseSourceTime(value.String)
	return &parsed
}
