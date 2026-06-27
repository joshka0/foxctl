package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/joshka0/foxctl/internal/adapters/artifacts"
	"github.com/joshka0/foxctl/internal/intelligence/turbovec"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/platform/timeutil"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/cache"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	"github.com/joshka0/foxctl/internal/storage/vector"
)

// Ensure Store implements storage.MemoryStore.
var _ storage.MemoryStore = (*Store)(nil)

// NamedEntry aliases the shared storage type for backwards compatibility.
type NamedEntry = storage.NamedEntry

// ScoredEntry aliases the shared scored entry type.
type ScoredEntry = storage.ScoredEntry

// Store handles named memory persistence.
type Store struct {
	db              *sql.DB
	cas             *cas.Store
	artifactManager artifacts.Manager
	path            string
	close           func() error

	// searcher is lazily initialized on first vector search.
	searcher     vector.VectorSearcher
	searcherOnce sync.Once
}

// Stats aliases the shared memory stats type.
type Stats = storage.MemoryStats

// ListFilter aliases the shared filter type for backwards compatibility.
type ListFilter = storage.MemoryListFilter

// LifecycleUpdate aliases the shared lifecycle update type.
type LifecycleUpdate = storage.MemoryLifecycleUpdate

// TelemetryUpdate aliases the shared telemetry update type.
type TelemetryUpdate = storage.MemoryTelemetryUpdate

// SaveOptions aliases the shared save options type for backwards compatibility.
type SaveOptions = storage.MemorySaveOptions

// EmbeddingMetadata aliases the shared embedding metadata type.
type EmbeddingMetadata = storage.EmbeddingMetadata

// WorkspaceMigrationSummary reports the results of migrating named memories between workspaces.
type WorkspaceMigrationSummary struct {
	From          string `json:"from"`
	To            string `json:"to"`
	Total         int    `json:"total"`
	Conflicts     int    `json:"conflicts"`
	Migrated      int    `json:"migrated"`
	MetadataMoved bool   `json:"metadata_moved"`
	DryRun        bool   `json:"dry_run"`
}

const namedEntrySelectColumns = `
	id, name, type, workspace, summary, result, digests,
	created_at, updated_at, last_accessed, access_count, session_id,
	lifecycle_state, pinned, review_status, superseded_by, review_notes,
	last_used_at, last_validated_at,
	selected_count, use_count, success_count, failure_count, patch_count, restore_count,
	last_selected_at, last_succeeded_at, last_failed_at, last_patched_at, last_restored_at,
	atomic_text, entities, keywords`

// Connection pool defaults for SQLite file-based storage
// These values provide reasonable defaults for typical workloads with moderate concurrency
const (
	defaultMaxOpenConns    = 10               // Max concurrent connections (balances concurrency with SQLite write serialization)
	defaultMaxIdleConns    = 5                // Idle connections kept ready
	defaultConnMaxLifetime = 10 * time.Minute // Connection recycling interval (SQLite best practice: 5-15 minutes)
	defaultConnMaxIdleTime = 15 * time.Minute // Idle connection timeout
	maxAccessBatchNames    = 100
)

const (
	memoryLifecycleStateCandidate   = "candidate"
	memoryLifecycleStateActive      = "active"
	memoryLifecycleStateStale       = "stale"
	memoryLifecycleStateArchived    = "archived"
	memoryLifecycleStateDeprecated  = "deprecated"
	memoryLifecycleStateQuarantined = "quarantined"
)

const (
	memoryReviewStatusUnreviewed       = "unreviewed"
	memoryReviewStatusNeedsReview      = "needs_review"
	memoryReviewStatusReviewed         = "reviewed"
	memoryReviewStatusValidated        = "validated"
	memoryReviewStatusFailedValidation = "failed_validation"
)

func normalizeMemoryLifecycleState(value string) (string, error) {
	state := strings.TrimSpace(value)
	if state == "" {
		return memoryLifecycleStateActive, nil
	}
	switch state {
	case memoryLifecycleStateCandidate,
		memoryLifecycleStateActive,
		memoryLifecycleStateStale,
		memoryLifecycleStateArchived,
		memoryLifecycleStateDeprecated,
		memoryLifecycleStateQuarantined:
		return state, nil
	default:
		return "", fmt.Errorf("memory: invalid memory lifecycle state %q", state)
	}
}

func normalizeMemoryReviewStatus(value string) (string, error) {
	status := strings.TrimSpace(value)
	if status == "" {
		return memoryReviewStatusUnreviewed, nil
	}
	switch status {
	case memoryReviewStatusUnreviewed,
		memoryReviewStatusNeedsReview,
		memoryReviewStatusReviewed,
		memoryReviewStatusValidated,
		memoryReviewStatusFailedValidation:
		return status, nil
	default:
		return "", fmt.Errorf("memory: invalid memory review status %q", status)
	}
}

func validateMemoryTelemetryCounters(entry NamedEntry) error {
	counters := []struct {
		name  string
		value int
	}{
		{name: "selected_count", value: entry.SelectedCount},
		{name: "use_count", value: entry.UseCount},
		{name: "success_count", value: entry.SuccessCount},
		{name: "failure_count", value: entry.FailureCount},
		{name: "patch_count", value: entry.PatchCount},
		{name: "restore_count", value: entry.RestoreCount},
	}
	for _, counter := range counters {
		if counter.value < 0 {
			return fmt.Errorf("memory: %s must be non-negative", counter.name)
		}
	}
	return nil
}

// Open initializes a memory-backed Store rooted at the provided filesystem path.
// It opens the database via dbutil.OpenStoreDB, runs migrations, and configures the DB connection pool; if casPath is non-empty it also initializes a CAS store and an artifacts.Manager.
func Open(ctx context.Context, root string, casPath string) (store *Store, err error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "MEMORY", "memory.db", migrate)
	if err != nil {
		return nil, fmt.Errorf("memory: open db: %w", err)
	}
	defer func() {
		if err != nil {
			_ = closeFn()
		}
	}()

	// Configure connection pool for optimal performance
	// Note: For SQLite, too many concurrent connections can cause contention
	// These defaults balance responsiveness with resource usage
	db.SetMaxOpenConns(defaultMaxOpenConns)
	db.SetMaxIdleConns(defaultMaxIdleConns)
	db.SetConnMaxLifetime(defaultConnMaxLifetime)
	db.SetConnMaxIdleTime(defaultConnMaxIdleTime)

	// dbutil.OpenStoreDB handles directory creation, WAL configuration, and migration execution.

	var casStore *cas.Store
	var artifactMgr artifacts.Manager
	if casPath != "" {
		if casStore, err = cas.NewStore(casPath); err != nil {
			return nil, err
		}
		artifactMgr = artifacts.NewManager(casStore)
	}
	store = &Store{db: db, cas: casStore, artifactManager: artifactMgr, path: filepath.Join(root, "memory.db"), close: closeFn}
	store.repairWorkspaceIDs(ctx)
	return store, nil
}

// OpenFromConfig opens the memory store using paths from config.
// OpenFromConfig opens a Store using paths from the provided configuration.
// It uses cfg.Storage.Root as the storage root and cfg.Paths.CAS as the CAS path.
// It returns the opened Store or an error if initialization fails.
func OpenFromConfig(ctx context.Context, cfg config.Config) (*Store, error) {
	return Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
}

// Close releases resources.
func (s *Store) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// DB returns the underlying *sql.DB for advanced operations like search.
// This allows callers to use WrapSQLDB for creating a dbdriver.DB.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Stats returns entry counts for named memories.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM named_memory`).Scan(&count); err != nil {
		return Stats{}, fmt.Errorf("memory: stats: %w", err)
	}
	return Stats{Named: count, Path: s.path}, nil
}

// MigrateWorkspace reassigns named memory entries from one workspace ID to another.
//
// Entries that would collide with an existing name in the target workspace are skipped.
func (s *Store) MigrateWorkspace(ctx context.Context, from, to string, dryRun bool) (WorkspaceMigrationSummary, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	summary := WorkspaceMigrationSummary{
		From:   from,
		To:     to,
		DryRun: dryRun,
	}
	if from == "" || to == "" {
		return summary, fmt.Errorf("memory: migrate workspace: from and to must be set")
	}
	if from == to {
		return summary, fmt.Errorf("memory: migrate workspace: from and to must differ")
	}

	if dryRun {
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM named_memory WHERE workspace = $1`, from).Scan(&summary.Total); err != nil {
			return summary, fmt.Errorf("memory: migrate workspace count: %w", err)
		}
		if err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM named_memory src
			WHERE src.workspace = $1
			  AND EXISTS (
				SELECT 1 FROM named_memory dst
				WHERE dst.workspace = $2 AND dst.name = src.name
			  )`, from, to).Scan(&summary.Conflicts); err != nil {
			return summary, fmt.Errorf("memory: migrate workspace conflicts: %w", err)
		}
		summary.Migrated = summary.Total - summary.Conflicts
		if summary.Migrated < 0 {
			summary.Migrated = 0
		}
		var fromMeta, toMeta int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM embedding_metadata WHERE workspace = $1`, from).Scan(&fromMeta); err != nil {
			return summary, fmt.Errorf("memory: migrate workspace metadata source: %w", err)
		}
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM embedding_metadata WHERE workspace = $1`, to).Scan(&toMeta); err != nil {
			return summary, fmt.Errorf("memory: migrate workspace metadata target: %w", err)
		}
		summary.MetadataMoved = fromMeta > 0 && toMeta == 0
		return summary, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return summary, fmt.Errorf("memory: migrate workspace begin: %w", err)
	}
	defer func() {
		if tx != nil {
			errs.Ignore(tx.Rollback(), "rollback memory workspace migration")
		}
	}()

	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM named_memory WHERE workspace = $1`, from).Scan(&summary.Total); err != nil {
		return summary, fmt.Errorf("memory: migrate workspace count: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM named_memory src
		WHERE src.workspace = $1
		  AND EXISTS (
			SELECT 1 FROM named_memory dst
			WHERE dst.workspace = $2 AND dst.name = src.name
		  )`, from, to).Scan(&summary.Conflicts); err != nil {
		return summary, fmt.Errorf("memory: migrate workspace conflicts: %w", err)
	}

	updateResult, err := tx.ExecContext(ctx, `
		UPDATE named_memory AS src
		SET workspace = $1
		WHERE src.workspace = $2
		  AND NOT EXISTS (
			SELECT 1 FROM named_memory dst
			WHERE dst.workspace = $3 AND dst.name = src.name
		  )`, to, from, to)
	if err != nil {
		return summary, fmt.Errorf("memory: migrate workspace update: %w", err)
	}
	if rows, err := updateResult.RowsAffected(); err == nil {
		summary.Migrated = int(rows)
	}

	var fromMeta, toMeta int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM embedding_metadata WHERE workspace = $1`, from).Scan(&fromMeta); err != nil {
		return summary, fmt.Errorf("memory: migrate workspace metadata source: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM embedding_metadata WHERE workspace = $1`, to).Scan(&toMeta); err != nil {
		return summary, fmt.Errorf("memory: migrate workspace metadata target: %w", err)
	}
	if fromMeta > 0 && toMeta == 0 {
		metadataResult, err := tx.ExecContext(ctx, `UPDATE embedding_metadata SET workspace = $1 WHERE workspace = $2`, to, from)
		if err != nil {
			return summary, fmt.Errorf("memory: migrate workspace metadata update: %w", err)
		}
		if rows, err := metadataResult.RowsAffected(); err == nil {
			summary.MetadataMoved = rows > 0
		}
	}

	// Best-effort: migrate indexer state (conflict-safe).
	// We intentionally don't fail the migration if this table doesn't exist in older DBs.
	if _, err := tx.ExecContext(ctx, `
		UPDATE indexer_state AS src
		SET workspace = $1
		WHERE src.workspace = $2
		  AND NOT EXISTS (
			SELECT 1 FROM indexer_state dst
			WHERE dst.workspace = $3 AND dst.indexer_id = src.indexer_id
		  )`, to, from, to); err != nil {
		errMsg := strings.ToLower(err.Error())
		if !strings.Contains(errMsg, "no such table") && !strings.Contains(errMsg, "does not exist") {
			return summary, fmt.Errorf("memory: migrate indexer state: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return summary, fmt.Errorf("memory: migrate workspace commit: %w", err)
	}
	tx = nil
	return summary, nil
}

// Save inserts or updates a named memory.
func (s *Store) Save(ctx context.Context, entry NamedEntry) (NamedEntry, error) {
	now := timeutil.NowUTC()
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	entry.LastAccess = now
	if entry.Type == "" {
		entry.Type = "result"
	}
	var err error
	if entry.LifecycleState, err = normalizeMemoryLifecycleState(entry.LifecycleState); err != nil {
		return NamedEntry{}, err
	}
	if entry.ReviewStatus, err = normalizeMemoryReviewStatus(entry.ReviewStatus); err != nil {
		return NamedEntry{}, err
	}
	if err := validateMemoryTelemetryCounters(entry); err != nil {
		return NamedEntry{}, err
	}
	entry.Workspace = ws.CanonicalID(entry.Workspace)

	// Format digests with proper error handling
	digestsJSON, err := sqlutil.FormatJSON(entry.Digests)
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: format digests: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO named_memory (
	id, name, type, workspace, summary, result, digests, session_id,
	created_at, updated_at, last_accessed, access_count,
	lifecycle_state, pinned, review_status, superseded_by, review_notes, last_used_at, last_validated_at,
	selected_count, use_count, success_count, failure_count, patch_count, restore_count,
	last_selected_at, last_succeeded_at, last_failed_at, last_patched_at, last_restored_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 0, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29)
ON CONFLICT(name, workspace) DO UPDATE SET
	id = excluded.id,
	type = excluded.type,
	summary = excluded.summary,
	result = excluded.result,
	digests = excluded.digests,
	session_id = COALESCE(NULLIF(excluded.session_id, ''), session_id),
	updated_at = excluded.updated_at,
	last_accessed = excluded.last_accessed
`, entry.ID, entry.Name, entry.Type, entry.Workspace, entry.Summary, entry.Result, digestsJSON, entry.SessionID,
		sqlutil.FormatTimestamp(entry.CreatedAt), sqlutil.FormatTimestamp(entry.UpdatedAt), sqlutil.FormatTimestamp(entry.LastAccess),
		entry.LifecycleState, boolToInt(entry.Pinned), entry.ReviewStatus, entry.SupersededBy, entry.ReviewNotes,
		sqlutil.FormatTimestamp(entry.LastUsedAt), sqlutil.FormatTimestamp(entry.LastValidatedAt),
		entry.SelectedCount, entry.UseCount, entry.SuccessCount, entry.FailureCount, entry.PatchCount, entry.RestoreCount,
		sqlutil.FormatTimestamp(entry.LastSelectedAt), sqlutil.FormatTimestamp(entry.LastSucceededAt),
		sqlutil.FormatTimestamp(entry.LastFailedAt), sqlutil.FormatTimestamp(entry.LastPatchedAt),
		sqlutil.FormatTimestamp(entry.LastRestoredAt))
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: save: %w", err)
	}
	s.pin(ctx, entry.Digests)
	return entry, nil
}

// Get fetches a named memory by name+workspace.
func (s *Store) Get(ctx context.Context, name, workspace string) (NamedEntry, error) {
	workspace = ws.CanonicalID(workspace)
	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE name = $1 AND workspace = $2`, namedEntrySelectColumns), name, workspace)
	entry, err := scanEntry(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return NamedEntry{}, ErrNotFound
		}
		return NamedEntry{}, fmt.Errorf("memory: get: %w", err)
	}

	if _, updateErr := s.db.ExecContext(ctx, `
		UPDATE named_memory
		SET last_accessed = $1, access_count = access_count + 1
		WHERE id = $2`, sqlutil.FormatTimestamp(timeutil.NowUTC()), entry.ID); updateErr != nil {
		errs.Ignore(updateErr, "memory: refresh access metadata")
	}

	return entry, nil
}

// List returns named memories for a workspace.
func (s *Store) List(ctx context.Context, workspace string, limit int) ([]NamedEntry, error) {
	workspace = ws.CanonicalID(workspace)
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE workspace = $1
		ORDER BY updated_at DESC
		LIMIT $2`, namedEntrySelectColumns), workspace, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: list: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close memory list rows")
	}()

	return scanEntries(rows)
}

// ListFiltered returns named memories for a workspace with optional filters.
//
// This is intended for stable pagination and for session-scoped context injection
// (e.g., “latest 20 gotchas for this session”).
func (s *Store) ListFiltered(ctx context.Context, workspace string, filter ListFilter, limit, offset int) ([]NamedEntry, int, error) {
	workspace = ws.CanonicalID(workspace)
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	argIdx := 1
	where := []string{fmt.Sprintf("workspace = $%d", argIdx)}
	argIdx++
	args := []any{workspace}

	if strings.TrimSpace(filter.SessionID) != "" {
		where = append(where, fmt.Sprintf("session_id = $%d", argIdx))
		argIdx++
		args = append(args, strings.TrimSpace(filter.SessionID))
	}

	if strings.TrimSpace(filter.NamePrefix) != "" {
		where = append(where, fmt.Sprintf("name LIKE $%d || '%%'", argIdx))
		argIdx++
		args = append(args, strings.TrimSpace(filter.NamePrefix))
	}

	if len(filter.Types) > 0 {
		placeholders := make([]string, 0, len(filter.Types))
		for _, t := range filter.Types {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			placeholders = append(placeholders, fmt.Sprintf("$%d", argIdx))
			argIdx++
			args = append(args, t)
		}
		if len(placeholders) > 0 {
			where = append(where, fmt.Sprintf("type IN (%s)", strings.Join(placeholders, ",")))
		}
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM named_memory WHERE %s", whereSQL), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("memory: list filtered count: %w", err)
	}

	q := fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE %s
		ORDER BY updated_at DESC
		LIMIT $%d OFFSET $%d`, namedEntrySelectColumns, whereSQL, argIdx, argIdx+1)
	qArgs := append(append([]any{}, args...), limit, offset)

	rows, err := s.db.QueryContext(ctx, q, qArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("memory: list filtered: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close memory list filtered rows") }()

	entries, err := scanEntries(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// Delete removes a named memory and unpins digests.
func (s *Store) Delete(ctx context.Context, name, workspace string) error {
	workspace = ws.CanonicalID(workspace)
	row := s.db.QueryRowContext(ctx, `SELECT digests FROM named_memory WHERE name = $1 AND workspace = $2`, name, workspace)
	var digests string
	if err := row.Scan(&digests); err != nil {
		if dbutil.IsNoRows(err) {
			return ErrNotFound
		}
		return fmt.Errorf("memory: scan digests: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM named_memory WHERE name = $1 AND workspace = $2`, name, workspace); err != nil {
		return fmt.Errorf("memory: delete: %w", err)
	}

	var arr []string
	if err := sqlutil.ScanJSON(digests, &arr); err != nil {
		return fmt.Errorf("memory: scan delete digests: %w", err)
	}
	s.unpin(ctx, arr)
	return nil
}

// DeleteByNamePrefix deletes all entries whose name starts with the given prefix.
// Returns the number of entries deleted.
func (s *Store) DeleteByNamePrefix(ctx context.Context, workspace, namePrefix string) (int, error) {
	workspace = ws.CanonicalID(workspace)
	// First collect digests from matching entries to unpin later
	rows, err := s.db.QueryContext(ctx, `
			SELECT digests FROM named_memory 
			WHERE workspace = $1 AND name LIKE $2 || '%'`,
		workspace, namePrefix)
	if err != nil {
		return 0, fmt.Errorf("memory: query delete prefix: %w", err)
	}

	var allDigests []string
	for rows.Next() {
		var digests string
		if err := rows.Scan(&digests); err != nil {
			// Close rows on error; error is not actionable.
			_ = rows.Close() //nolint:errcheck
			return 0, fmt.Errorf("memory: scan delete prefix digests: %w", err)
		}
		var arr []string
		if err := sqlutil.ScanJSON(digests, &arr); err != nil {
			// Log JSON parse error but continue processing to remain resilient
			// This can indicate corrupted digest entries which may cause CAS leaks
			logger.Warn().Str("workspace", workspace).Str("prefix", namePrefix).Str("digests", digests).Err(err).Msg("failed to parse digests JSON")
			continue
		}
		allDigests = append(allDigests, arr...)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("memory: close rows: %w", err)
	}

	// Delete matching entries
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM named_memory 
		WHERE workspace = $1 AND name LIKE $2 || '%'`,
		workspace, namePrefix)
	if err != nil {
		return 0, fmt.Errorf("memory: delete prefix: %w", err)
	}

	// RowsAffected error is nil for SQLite.
	count, _ := result.RowsAffected() //nolint:errcheck

	// Unpin collected digests
	s.unpin(ctx, allDigests)

	return int(count), nil
}

// SaveResult stores a result envelope using structured options.
func (s *Store) SaveResult(ctx context.Context, opts SaveOptions) (NamedEntry, error) {
	entry := NamedEntry{
		Name:      opts.Name,
		Type:      opts.Type,
		Workspace: opts.Workspace,
		Summary:   opts.Summary,
		Result:    opts.Result,
		Digests:   cache.CollectDigests(opts.Result),
		SessionID: opts.SessionID,
	}
	return s.Save(ctx, entry)
}

// SaveFromResult is a helper that stores a result envelope.
// Deprecated: Use SaveResult with SaveOptions instead for better clarity.
func (s *Store) SaveFromResult(ctx context.Context, name, typ, workspace, summary string, result []byte) (NamedEntry, error) {
	return s.SaveResult(ctx, SaveOptions{
		Name:      name,
		Type:      typ,
		Workspace: workspace,
		Summary:   summary,
		Result:    result,
	})
}

// ErrNotFound indicates missing entries.
var ErrNotFound = fmt.Errorf("memory: not found")

func (s *Store) pin(ctx context.Context, digests []string) {
	if s.artifactManager == nil {
		return
	}
	// Check for context cancellation
	if ctx.Err() != nil {
		return
	}
	if err := s.artifactManager.Pin(ctx, digests...); err != nil {
		errs.Ignore(err, "memory: pin digests")
	}
}

func (s *Store) unpin(ctx context.Context, digests []string) {
	if s.artifactManager == nil {
		return
	}
	// Check for context cancellation
	if ctx.Err() != nil {
		return
	}
	if err := s.artifactManager.Unpin(ctx, digests...); err != nil {
		errs.Ignore(err, "memory: unpin digests")
	}
}

// MigrateSchema runs the memory store DDL migrations against the given database.
// This is exported so the CLI db migrate command can create PostgreSQL tables.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db)
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS named_memory (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	type TEXT NOT NULL,
	workspace TEXT NOT NULL,
	session_id TEXT,
	summary TEXT,
	result BLOB NOT NULL,
	digests TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	last_accessed TEXT NOT NULL,
	access_count INTEGER NOT NULL DEFAULT 0,
	UNIQUE(name, workspace)
);
CREATE INDEX IF NOT EXISTS idx_named_memory_ws_updated ON named_memory(workspace, updated_at DESC);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("memory: migrate: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS embedding_metadata (
			workspace TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			dimensions INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("memory: create embedding metadata: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS indexer_state (
			workspace TEXT NOT NULL,
			indexer_id TEXT NOT NULL,
			last_indexed_head_sha TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(workspace, indexer_id)
		);
	`); err != nil {
		return fmt.Errorf("memory: create indexer state: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_indexer_state_ws ON indexer_state(workspace);
	`); err != nil {
		return fmt.Errorf("memory: create indexer state index: %w", err)
	}

	// Add session_id column for pre-migration databases.
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE named_memory ADD COLUMN session_id TEXT;
	`); err != nil {
		errMsg := strings.ToLower(err.Error())
		if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
			return fmt.Errorf("memory: add session_id column: %w", err)
		}
	}

	// Add embedding column for vector search support (optional, requires CGO + vector build tag)
	// This column will remain NULL unless vector functionality is enabled and used
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE named_memory ADD COLUMN embedding BLOB DEFAULT NULL;
	`); err != nil {
		// Ignore error if column already exists
		// Check for duplicate column error message (works across SQLite versions)
		errMsg := strings.ToLower(err.Error())
		if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
			return fmt.Errorf("memory: add embedding column: %w", err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_named_memory_session ON named_memory(session_id);
	`); err != nil {
		return fmt.Errorf("memory: add session index: %w", err)
	}

	// Add atomic processing columns for SimpleMem-style semantic lossless compression.
	// See: https://github.com/aiming-lab/SimpleMem
	atomicColumns := []string{
		`ALTER TABLE named_memory ADD COLUMN atomic_text TEXT`, // Self-contained, disambiguated rewrite
		`ALTER TABLE named_memory ADD COLUMN entities TEXT`,    // JSON array of extracted entities
		`ALTER TABLE named_memory ADD COLUMN keywords TEXT`,    // JSON array of BM25 keywords
	}
	for _, stmt := range atomicColumns {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			errMsg := strings.ToLower(err.Error())
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				return fmt.Errorf("memory: add atomic column: %w", err)
			}
		}
	}

	lifecycleColumns := []string{
		`ALTER TABLE named_memory ADD COLUMN lifecycle_state TEXT NOT NULL DEFAULT 'active'`,
		`ALTER TABLE named_memory ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN review_status TEXT NOT NULL DEFAULT 'unreviewed'`,
		`ALTER TABLE named_memory ADD COLUMN superseded_by TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN review_notes TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_used_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_validated_at TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range lifecycleColumns {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			errMsg := strings.ToLower(err.Error())
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				return fmt.Errorf("memory: add lifecycle column: %w", err)
			}
		}
	}

	telemetryColumns := []string{
		`ALTER TABLE named_memory ADD COLUMN selected_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN use_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN success_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN failure_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN patch_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN restore_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN last_selected_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_succeeded_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_failed_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_patched_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_restored_at TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range telemetryColumns {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			errMsg := strings.ToLower(err.Error())
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				return fmt.Errorf("memory: add telemetry column: %w", err)
			}
		}
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_named_memory_lifecycle ON named_memory(workspace, lifecycle_state, updated_at DESC);
	`); err != nil {
		return fmt.Errorf("memory: add lifecycle index: %w", err)
	}

	return nil
}

// Search finds entries whose lexical memory text matches the query.
func (s *Store) Search(ctx context.Context, workspace, query string, limit int) ([]ScoredEntry, error) {
	workspace = ws.CanonicalID(workspace)
	if limit <= 0 {
		limit = 20
	}
	terms := memorySearchTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}

	args := []any{workspace}
	termClauses := make([]string, 0, len(terms))
	for i, term := range terms {
		placeholder := fmt.Sprintf("$%d", i+2)
		termClauses = append(termClauses, fmt.Sprintf(`(
			LOWER(name) LIKE %s OR
			LOWER(summary) LIKE %s OR
			LOWER(COALESCE(atomic_text, '')) LIKE %s OR
			LOWER(COALESCE(entities, '')) LIKE %s OR
			LOWER(COALESCE(keywords, '')) LIKE %s
		)`, placeholder, placeholder, placeholder, placeholder, placeholder))
		args = append(args, "%"+strings.ToLower(term)+"%")
	}
	args = append(args, memorySearchCandidateWindow(limit))
	limitPlaceholder := fmt.Sprintf("$%d", len(args))

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE workspace = $1 AND (%s)
		ORDER BY updated_at DESC
		LIMIT %s`, namedEntrySelectColumns, strings.Join(termClauses, " OR "), limitPlaceholder), args...)
	if err != nil {
		return nil, fmt.Errorf("memory: search: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close memory search rows")
	}()
	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}
	return scoreMemoryLexicalEntries(entries, query, limit), nil
}

// ExistsByNameSuffix checks if any entry exists with a name ending in the given suffix.
// Used for content-hash deduplication across sessions (e.g., suffix ":<type>:<digest>").
func (s *Store) ExistsByNameSuffix(ctx context.Context, workspace, suffix string) (bool, error) {
	workspace = ws.CanonicalID(workspace)
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM named_memory
		WHERE workspace = $1 AND name LIKE '%' || $2`,
		workspace, suffix).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("memory: exists by suffix: %w", err)
	}
	return count > 0, nil
}

// ListByType returns entries of a specific type for a workspace, ordered by recency.
func (s *Store) ListByType(ctx context.Context, workspace, entryType string, limit int) ([]ScoredEntry, error) {
	workspace = ws.CanonicalID(workspace)
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE workspace = $1 AND type = $2
		ORDER BY updated_at DESC
		LIMIT $3`, namedEntrySelectColumns), workspace, entryType, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: list by type: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close memory list rows") }()

	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}
	var scored []ScoredEntry
	for _, entry := range entries {
		scored = append(scored, ScoredEntry{
			Entry: entry,
			Score: scoreEntry(entry),
		})
	}
	return scored, nil
}

// ListWithoutEmbedding returns memories that don't have an embedding yet.
// Used for incremental embedding generation.
func (s *Store) ListWithoutEmbedding(ctx context.Context, workspace string, limit int) ([]NamedEntry, error) {
	return s.ListWithoutEmbeddingPage(ctx, workspace, limit, 0)
}

// ListWithoutEmbeddingPage returns memories that don't have embeddings yet using limit/offset pagination.
func (s *Store) ListWithoutEmbeddingPage(ctx context.Context, workspace string, limit, offset int) ([]NamedEntry, error) {
	workspace = ws.CanonicalID(workspace)
	if limit <= 0 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE workspace = $1 AND (embedding IS NULL OR LENGTH(embedding) = 0) AND summary IS NOT NULL AND summary != ''
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, namedEntrySelectColumns), workspace, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("memory: list without embedding: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close memory list rows")
	}()

	return scanEntries(rows)
}

// Update mutates summary and/or type for a named entry.
func (s *Store) Update(ctx context.Context, name, workspace string, summary, typ *string) (NamedEntry, error) {
	entry, err := s.Get(ctx, name, workspace)
	if err != nil {
		return NamedEntry{}, err
	}
	if summary != nil {
		entry.Summary = *summary
	}
	if typ != nil && *typ != "" {
		entry.Type = *typ
	}
	entry.UpdatedAt = timeutil.NowUTC()
	if _, err := s.db.ExecContext(ctx, `
		UPDATE named_memory
		SET summary = $1, type = $2, updated_at = $3
		WHERE id = $4`, entry.Summary, entry.Type, sqlutil.FormatTimestamp(entry.UpdatedAt), entry.ID); err != nil {
		return NamedEntry{}, fmt.Errorf("memory: update: %w", err)
	}
	return entry, nil
}

// UpdateLifecycle mutates lifecycle metadata for a named memory entry.
func (s *Store) UpdateLifecycle(ctx context.Context, name, workspace string, update LifecycleUpdate) (NamedEntry, error) {
	workspace = ws.CanonicalID(workspace)
	entry, err := s.getWithoutTracking(ctx, name, workspace)
	if err != nil {
		return NamedEntry{}, err
	}
	if state := strings.TrimSpace(update.LifecycleState); state != "" {
		entry.LifecycleState, err = normalizeMemoryLifecycleState(state)
	} else {
		entry.LifecycleState, err = normalizeMemoryLifecycleState(entry.LifecycleState)
	}
	if err != nil {
		return NamedEntry{}, err
	}
	if status := strings.TrimSpace(update.ReviewStatus); status != "" {
		entry.ReviewStatus, err = normalizeMemoryReviewStatus(status)
	} else {
		entry.ReviewStatus, err = normalizeMemoryReviewStatus(entry.ReviewStatus)
	}
	if err != nil {
		return NamedEntry{}, err
	}
	entry.SupersededBy = strings.TrimSpace(update.SupersededBy)
	entry.ReviewNotes = strings.TrimSpace(update.ReviewNotes)
	if update.LastUsedAt != nil {
		entry.LastUsedAt = update.LastUsedAt.UTC()
	}
	if update.LastValidatedAt != nil {
		entry.LastValidatedAt = update.LastValidatedAt.UTC()
	}
	entry.UpdatedAt = timeutil.NowUTC()
	if _, err := s.db.ExecContext(
		ctx, `
		UPDATE named_memory
		SET lifecycle_state = $1,
		    review_status = $2,
		    superseded_by = $3,
		    review_notes = $4,
		    last_used_at = $5,
		    last_validated_at = $6,
		    updated_at = $7
		WHERE id = $8`,
		entry.LifecycleState,
		entry.ReviewStatus,
		entry.SupersededBy,
		entry.ReviewNotes,
		sqlutil.FormatTimestamp(entry.LastUsedAt),
		sqlutil.FormatTimestamp(entry.LastValidatedAt),
		sqlutil.FormatTimestamp(entry.UpdatedAt),
		entry.ID,
	); err != nil {
		return NamedEntry{}, fmt.Errorf("memory: update lifecycle: %w", err)
	}
	return entry, nil
}

// UpdateTelemetry records an explicit telemetry action for a named memory entry.
func (s *Store) UpdateTelemetry(ctx context.Context, name, workspace string, update TelemetryUpdate) (NamedEntry, error) {
	workspace = ws.CanonicalID(workspace)
	entry, err := s.getWithoutTracking(ctx, name, workspace)
	if err != nil {
		return NamedEntry{}, err
	}
	at := timeutil.NowUTC()
	if update.At != nil {
		at = update.At.UTC()
	}
	column, timestampColumn, err := telemetryColumnsForAction(update.Action)
	if err != nil {
		return NamedEntry{}, err
	}
	entry.UpdatedAt = timeutil.NowUTC()
	query := fmt.Sprintf(`
		UPDATE named_memory
		SET %s = %s + 1,
		    %s = $1,
		    updated_at = $2
		WHERE id = $3`, column, column, timestampColumn)
	if _, err := s.db.ExecContext(ctx, query, sqlutil.FormatTimestamp(at), sqlutil.FormatTimestamp(entry.UpdatedAt), entry.ID); err != nil {
		return NamedEntry{}, fmt.Errorf("memory: update telemetry: %w", err)
	}
	return s.getWithoutTracking(ctx, name, workspace)
}

// RecordAccessBatch records surfaced retrieval access without touching outcome telemetry.
//
// [[domain:memory-decay-ranking]]
// [[invariant:retrieval-access-not-outcome-telemetry]]
func (s *Store) RecordAccessBatch(ctx context.Context, workspace string, names []string, at time.Time) (int, error) {
	workspace = ws.CanonicalID(workspace)
	names = normalizedAccessBatchNames(names)
	if len(names) == 0 {
		return 0, nil
	}
	if at.IsZero() {
		at = timeutil.NowUTC()
	} else {
		at = at.UTC()
	}

	placeholders := make([]string, len(names))
	args := make([]any, 0, len(names)+2)
	args = append(args, sqlutil.FormatTimestamp(at), workspace)
	for i, name := range names {
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args = append(args, name)
	}

	result, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE named_memory
		SET last_accessed = $1,
		    access_count = access_count + 1
		WHERE workspace = $2 AND name IN (%s)`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return 0, fmt.Errorf("memory: record access batch: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return int(affected), nil
}

func normalizedAccessBatchNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	capacity := len(names)
	if capacity > maxAccessBatchNames {
		capacity = maxAccessBatchNames
	}
	out := make([]string, 0, capacity)
	seen := make(map[string]struct{}, capacity)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
		if len(out) >= maxAccessBatchNames {
			break
		}
	}
	return out
}

func telemetryColumnsForAction(action string) (string, string, error) {
	switch strings.TrimSpace(action) {
	case "viewed":
		return "access_count", "last_accessed", nil
	case "selected":
		return "selected_count", "last_selected_at", nil
	case "used":
		return "use_count", "last_used_at", nil
	case "succeeded":
		return "success_count", "last_succeeded_at", nil
	case "failed":
		return "failure_count", "last_failed_at", nil
	case "restored":
		return "restore_count", "last_restored_at", nil
	case "patched":
		return "patch_count", "last_patched_at", nil
	default:
		return "", "", fmt.Errorf("memory: unknown telemetry action %q", action)
	}
}

func (s *Store) getWithoutTracking(ctx context.Context, name, workspace string) (NamedEntry, error) {
	workspace = ws.CanonicalID(workspace)
	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE name = $1 AND workspace = $2`, namedEntrySelectColumns), name, workspace)
	entry, err := scanEntry(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return NamedEntry{}, ErrNotFound
		}
		return NamedEntry{}, fmt.Errorf("memory: get: %w", err)
	}
	return entry, nil
}

// UpdateEmbedding stores an embedding vector for a named memory entry.
// The embedding is stored as a JSON-encoded float32 array in the embedding BLOB column.
func (s *Store) UpdateEmbedding(ctx context.Context, name, workspace string, embedding []float32) error {
	workspace = ws.CanonicalID(workspace)
	if len(embedding) == 0 {
		return fmt.Errorf("memory: embedding must not be empty")
	}
	// Verify entry exists
	_, err := s.Get(ctx, name, workspace)
	if err != nil {
		return err
	}
	if err := s.ensureEmbeddingMetadata(ctx, workspace, "", len(embedding)); err != nil {
		return err
	}

	// Marshal embedding to JSON
	embeddingJSON, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("memory: marshal embedding: %w", err)
	}

	// Update embedding column
	if _, err := s.db.ExecContext(ctx, `
		UPDATE named_memory
		SET embedding = $1, updated_at = $2
		WHERE name = $3 AND workspace = $4
	`, embeddingJSON, sqlutil.FormatTimestamp(timeutil.NowUTC()), name, workspace); err != nil {
		return fmt.Errorf("memory: update embedding: %w", err)
	}

	return nil
}

func decodeStoredEmbeddingJSON(raw []byte, expectedDimensions int) ([]float32, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var embedding []float32
	if err := json.Unmarshal(raw, &embedding); err != nil {
		return nil, err
	}
	if embedding == nil {
		return nil, fmt.Errorf("embedding must be a JSON array")
	}
	if len(embedding) == 0 {
		return nil, fmt.Errorf("embedding must not be empty")
	}
	if expectedDimensions > 0 && len(embedding) != expectedDimensions {
		return nil, fmt.Errorf("embedding dimensions=%d expected=%d", len(embedding), expectedDimensions)
	}
	return embedding, nil
}

// SyncSymbolEmbeddingsOptions configures sync from symbol_embeddings into named_memory.
type SyncSymbolEmbeddingsOptions struct {
	WorkspaceID string
	SymbolIDs   []string
	OnlyMissing bool
}

// SyncSymbolEmbeddings fills named_memory.embedding from symbol_embeddings for a workspace.
// If SymbolIDs is non-empty, only those symbols are synced.
func (s *Store) SyncSymbolEmbeddings(ctx context.Context, embeddingDBPath string, opts SyncSymbolEmbeddingsOptions) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("memory: sync embeddings: nil store")
	}
	embeddingDBPath = strings.TrimSpace(embeddingDBPath)
	if embeddingDBPath == "" {
		return 0, fmt.Errorf("memory: sync embeddings: embedding db path required")
	}
	workspaceID := strings.TrimSpace(opts.WorkspaceID)
	if workspaceID == "" {
		return 0, fmt.Errorf("memory: sync embeddings: workspace required")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("memory: sync embeddings: conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE $1 AS embeddb", embeddingDBPath); err != nil {
		return 0, fmt.Errorf("memory: sync embeddings: attach: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, "DETACH DATABASE embeddb")
	}()

	argIdx := 1
	where := []string{fmt.Sprintf("workspace = $%d", argIdx)}
	argIdx++
	where = append(where, fmt.Sprintf("name LIKE $%d", argIdx))
	argIdx++
	args := []any{workspaceID, fmt.Sprintf("symbol://%s/%%", workspaceID)}
	if opts.OnlyMissing {
		where = append(where, "(embedding IS NULL OR LENGTH(embedding) = 0)")
	}
	if len(opts.SymbolIDs) > 0 {
		names := make([]string, 0, len(opts.SymbolIDs))
		for _, id := range opts.SymbolIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			names = append(names, fmt.Sprintf("symbol://%s/%s", workspaceID, id))
		}
		if len(names) > 0 {
			pl := make([]string, 0, len(names))
			for range names {
				pl = append(pl, fmt.Sprintf("$%d", argIdx))
				argIdx++
			}
			where = append(where, fmt.Sprintf("name IN (%s)", strings.Join(pl, ", ")))
			for _, name := range names {
				args = append(args, name)
			}
		}
	}

	validationWhere := strings.ReplaceAll(
		strings.Join(where, " AND "),
		"(embedding IS NULL OR LENGTH(embedding) = 0)",
		"(named_memory.embedding IS NULL OR LENGTH(named_memory.embedding) = 0)",
	)
	syncMetadata, err := validateSQLiteSymbolEmbeddingsForSync(ctx, conn, validationWhere, args)
	if err != nil {
		return 0, err
	}

	stmt := fmt.Sprintf(`
UPDATE named_memory
SET embedding = (
	SELECT e.embedding
	FROM embeddb.symbol_embeddings e
	WHERE e.workspace_id = named_memory.workspace
		AND e.symbol_id = replace(named_memory.name, 'symbol://' || named_memory.workspace || '/', '')
)
WHERE %s
	AND EXISTS (
		SELECT 1
		FROM embeddb.symbol_embeddings e
		WHERE e.workspace_id = named_memory.workspace
			AND e.symbol_id = replace(named_memory.name, 'symbol://' || named_memory.workspace || '/', '')
	)
`, strings.Join(where, " AND "))

	result, err := conn.ExecContext(ctx, stmt, args...)
	if err != nil {
		return 0, fmt.Errorf("memory: sync embeddings: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("memory: sync embeddings: rows affected: %w", err)
	}
	if updated > 0 && syncMetadata.dimensions > 0 {
		if err := s.ensureEmbeddingMetadata(ctx, workspaceID, syncMetadata.model, syncMetadata.dimensions); err != nil {
			return 0, err
		}
	}
	return int(updated), nil
}

type sqliteSymbolEmbeddingSyncMetadata struct {
	model      string
	dimensions int
}

func validateSQLiteSymbolEmbeddingsForSync(ctx context.Context, conn *sql.Conn, where string, args []any) (sqliteSymbolEmbeddingSyncMetadata, error) {
	query := fmt.Sprintf(`
SELECT
	replace(named_memory.name, 'symbol://' || named_memory.workspace || '/', '') AS symbol_id,
	e.embedding,
	e.model,
	e.dimensions
FROM named_memory
JOIN embeddb.symbol_embeddings e
	ON e.workspace_id = named_memory.workspace
	AND e.symbol_id = replace(named_memory.name, 'symbol://' || named_memory.workspace || '/', '')
WHERE %s
`, where)

	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return sqliteSymbolEmbeddingSyncMetadata{}, fmt.Errorf("memory: sync embeddings: validate queued embeddings: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close queued symbol embedding validation rows")
	}()

	var metadata sqliteSymbolEmbeddingSyncMetadata
	for rows.Next() {
		var symbolID string
		var embeddingJSON []byte
		var model sql.NullString
		var dimensions sql.NullInt64
		if err := rows.Scan(&symbolID, &embeddingJSON, &model, &dimensions); err != nil {
			return sqliteSymbolEmbeddingSyncMetadata{}, fmt.Errorf("memory: sync embeddings: scan queued embedding: %w", err)
		}

		expectedDimensions := 0
		if dimensions.Valid {
			expectedDimensions = int(dimensions.Int64)
		}
		embedding, err := decodeStoredEmbeddingJSON(embeddingJSON, expectedDimensions)
		if err != nil {
			return sqliteSymbolEmbeddingSyncMetadata{}, fmt.Errorf("memory: sync embeddings: decode %s: %w", symbolID, err)
		}
		if metadata.dimensions == 0 {
			metadata.model = strings.TrimSpace(model.String)
			metadata.dimensions = len(embedding)
		}
	}
	if err := rows.Err(); err != nil {
		return sqliteSymbolEmbeddingSyncMetadata{}, fmt.Errorf("memory: sync embeddings: validate queued embedding rows: %w", err)
	}
	return metadata, nil
}

// GetEmbedding retrieves the embedding vector for a named memory entry.
// Returns nil if no embedding is stored.
func (s *Store) GetEmbedding(ctx context.Context, name, workspace string) ([]float32, error) {
	workspace = ws.CanonicalID(workspace)
	var embeddingJSON []byte
	err := s.db.QueryRowContext(ctx, `
			SELECT embedding FROM named_memory WHERE name = $1 AND workspace = $2
		`, name, workspace).Scan(&embeddingJSON)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("memory: get embedding: %w", err)
	}

	if len(embeddingJSON) == 0 {
		return nil, nil
	}

	expectedDimensions := 0
	meta, err := s.GetEmbeddingMetadata(ctx, workspace)
	if err != nil {
		return nil, err
	}
	if meta != nil {
		expectedDimensions = meta.Dimensions
	}
	embedding, err := decodeStoredEmbeddingJSON(embeddingJSON, expectedDimensions)
	if err != nil {
		return nil, fmt.Errorf("memory: decode stored embedding: %w", err)
	}

	return embedding, nil
}

// GetEmbeddingMetadata retrieves embedding metadata for a workspace.
// Returns nil if no metadata exists.
func (s *Store) GetEmbeddingMetadata(ctx context.Context, workspace string) (*EmbeddingMetadata, error) {
	workspace = ws.CanonicalID(workspace)
	var meta EmbeddingMetadata
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `
			SELECT workspace, provider, model, dimensions, created_at, updated_at
			FROM embedding_metadata WHERE workspace = $1
	`, workspace).Scan(&meta.Workspace, &meta.Provider, &meta.Model, &meta.Dimensions, &createdAt, &updatedAt)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("memory: get embedding metadata: %w", err)
	}
	meta.CreatedAt, err = sqlutil.ScanTimestamp(createdAt)
	if err != nil {
		return nil, fmt.Errorf("memory: scan created_at: %w", err)
	}
	meta.UpdatedAt, err = sqlutil.ScanTimestamp(updatedAt)
	if err != nil {
		return nil, fmt.Errorf("memory: scan updated_at: %w", err)
	}
	return &meta, nil
}

// SetEmbeddingMetadata stores or updates embedding metadata for a workspace.
func (s *Store) SetEmbeddingMetadata(ctx context.Context, meta EmbeddingMetadata) error {
	meta.Workspace = ws.CanonicalID(meta.Workspace)
	now := timeutil.NowUTC()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO embedding_metadata (workspace, provider, model, dimensions, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT(workspace) DO UPDATE SET
			provider = excluded.provider,
			model = excluded.model,
			dimensions = excluded.dimensions,
			updated_at = excluded.updated_at
	`, meta.Workspace, meta.Provider, meta.Model, meta.Dimensions,
		sqlutil.FormatTimestamp(meta.CreatedAt), sqlutil.FormatTimestamp(meta.UpdatedAt))
	if err != nil {
		return fmt.Errorf("memory: set embedding metadata: %w", err)
	}
	return nil
}

func (s *Store) ensureEmbeddingMetadata(ctx context.Context, workspace, model string, dimensions int) error {
	workspace = ws.CanonicalID(workspace)
	model = strings.TrimSpace(model)
	if workspace == "" || dimensions <= 0 {
		return nil
	}
	meta, err := s.GetEmbeddingMetadata(ctx, workspace)
	if err != nil {
		return err
	}
	if meta == nil {
		return s.SetEmbeddingMetadata(ctx, EmbeddingMetadata{
			Workspace:  workspace,
			Model:      model,
			Dimensions: dimensions,
		})
	}
	if meta.Dimensions != dimensions {
		return fmt.Errorf("memory: embedding dimension mismatch: workspace %q expects %d dimensions (model: %s), got %d; run `foxctl index init --workspace <workspace-path> --scope memory` to rebuild memory embeddings",
			workspace, meta.Dimensions, meta.Model, dimensions)
	}
	if meta.Model != "" && model != "" && meta.Model != model {
		return fmt.Errorf("memory: embedding model mismatch: workspace %q expects model %q with %d dimensions, got %q; run `foxctl index init --workspace <workspace-path> --scope memory` to rebuild memory embeddings",
			workspace, meta.Model, meta.Dimensions, model)
	}
	if meta.Model == "" && model != "" {
		meta.Model = model
		return s.SetEmbeddingMetadata(ctx, *meta)
	}
	return nil
}

// ValidateEmbeddingDimensions checks if embedding dimensions match stored metadata.
// Returns an error if there's a dimension mismatch. Returns nil if no metadata exists.
func (s *Store) ValidateEmbeddingDimensions(ctx context.Context, workspace string, dimensions int) error {
	workspace = ws.CanonicalID(workspace)
	meta, err := s.GetEmbeddingMetadata(ctx, workspace)
	if err != nil {
		return err
	}
	if meta == nil {
		return nil // No metadata, allow any dimensions
	}
	if meta.Dimensions != dimensions {
		return fmt.Errorf("memory: embedding dimension mismatch: workspace %q expects %d dimensions (model: %s), got %d; run `foxctl index init --workspace <workspace-path> --scope memory` to rebuild memory embeddings",
			workspace, meta.Dimensions, meta.Model, dimensions)
	}
	return nil
}

// Relevant ranks entries by recency/access frequency.
func (s *Store) Relevant(ctx context.Context, workspace string, limit int) ([]ScoredEntry, error) {
	workspace = ws.CanonicalID(workspace)
	if limit <= 0 {
		limit = 10
	}
	// Fetch a larger window (500 most recently accessed entries) to score client-side.
	// This prevents loading all entries for large datasets while still providing good ranking
	const maxWindow = 500
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE workspace = $1
		ORDER BY last_accessed DESC, updated_at DESC
		LIMIT $2`, namedEntrySelectColumns), workspace, maxWindow)
	if err != nil {
		return nil, fmt.Errorf("memory: relevant: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close memory relevant rows")
	}()
	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}
	scored := make([]ScoredEntry, 0, len(entries))
	for _, entry := range entries {
		scored = append(scored, ScoredEntry{
			Entry: entry,
			Score: scoreEntry(entry),
		})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Entry.UpdatedAt.After(scored[j].Entry.UpdatedAt)
		}
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored, nil
}

// getSearcher lazily initializes the vector searcher on first call.
// It uses the embedding dimensions from the query to configure the searcher.
func (s *Store) getSearcher(dim int) vector.VectorSearcher {
	s.searcherOnce.Do(func() {
		socketPath := turbovec.DefaultSocketPath()
		s.searcher = vector.NewSearcher("memory", socketPath, dim)
	})
	return s.searcher
}

// SearchSimilar finds entries similar to the given embedding using in-memory cosine similarity.
// This is a fallback for SQLite which doesn't support native vector search.
// For better performance with large datasets, use Turso with native vector search.
func (s *Store) SearchSimilar(ctx context.Context, workspace string, queryEmbedding []float32, limit int) ([]ScoredEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	workspace = ws.CanonicalID(workspace)
	if err := s.ValidateEmbeddingDimensions(ctx, workspace, len(queryEmbedding)); err != nil {
		return nil, err
	}

	// Load entries with embeddings from this workspace
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s, embedding
		FROM named_memory
		WHERE workspace = $1 AND embedding IS NOT NULL AND LENGTH(embedding) > 0
		LIMIT 1000
	`, namedEntrySelectColumns), workspace)
	if err != nil {
		return nil, fmt.Errorf("memory: search similar: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close search similar rows") }()

	type entryWithEmbedding struct {
		entry     NamedEntry
		embedding []float32
	}

	var candidates []entryWithEmbedding
	for rows.Next() {
		var entry NamedEntry
		var embeddingJSON []byte

		if err := scanEntryValues(rows, &entry, &embeddingJSON); err != nil {
			return nil, fmt.Errorf("memory: search similar scan entry: %w", err)
		}

		embedding, err := decodeStoredEmbeddingJSON(embeddingJSON, len(queryEmbedding))
		if err != nil {
			return nil, fmt.Errorf("memory: search similar decode embedding for %q: %w", entry.Name, err)
		}

		candidates = append(candidates, entryWithEmbedding{entry: entry, embedding: embedding})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: search similar rows: %w", err)
	}

	// Build candidate list for the vector searcher.
	idEmbeds := make([]vector.IDEmbedding, len(candidates))
	for i, c := range candidates {
		idEmbeds[i] = vector.IDEmbedding{ID: c.entry.ID, Embedding: c.embedding}
	}

	// Use the vector searcher for cosine similarity ranking.
	searcher := s.getSearcher(len(queryEmbedding))
	scored, err := searcher.Search(ctx, queryEmbedding, idEmbeds, limit)
	if err != nil {
		// Fallback to inline cosine if searcher fails.
		scored = nil
		for _, c := range candidates {
			similarity := vector.Cosine(queryEmbedding, c.embedding)
			if similarity > 0.5 {
				scored = append(scored, vector.ScoredID{ID: c.entry.ID, Score: similarity})
			}
		}
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].Score > scored[j].Score
		})
		if len(scored) > limit {
			scored = scored[:limit]
		}
	}

	// Build lookup map for entries by ID.
	entryMap := make(map[string]NamedEntry, len(candidates))
	for _, c := range candidates {
		entryMap[c.entry.ID] = c.entry
	}

	// Map scored results back to ScoredEntry, applying threshold.
	results := make([]ScoredEntry, 0, len(scored))
	for _, s := range scored {
		if s.Score <= 0.5 {
			continue
		}
		if entry, ok := entryMap[s.ID]; ok {
			results = append(results, ScoredEntry{
				Entry: entry,
				Score: s.Score,
			})
		}
	}

	return results, nil
}

// SearchSimilarByType finds entries similar to the given embedding within a specific entry type.
// This avoids scanning unrelated entries when a type-specific search is needed.
func (s *Store) SearchSimilarByType(ctx context.Context, workspace, entryType string, queryEmbedding []float32, limit int) ([]ScoredEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	workspace = ws.CanonicalID(workspace)
	if err := s.ValidateEmbeddingDimensions(ctx, workspace, len(queryEmbedding)); err != nil {
		return nil, err
	}
	if entryType == "" {
		return nil, fmt.Errorf("memory: search similar by type: entry type required")
	}

	// Load entries with embeddings from this workspace and type
	// Use a wider candidate window to ensure enough same-type matches are considered.
	candidateLimit := limit * 50
	if candidateLimit < 1000 {
		candidateLimit = 1000
	}
	if candidateLimit > 5000 {
		candidateLimit = 5000
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s, embedding
		FROM named_memory
		WHERE workspace = $1 AND type = $2 AND embedding IS NOT NULL AND LENGTH(embedding) > 0
		LIMIT $3
	`, namedEntrySelectColumns), workspace, entryType, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("memory: search similar by type: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close search similar by type rows") }()

	type entryWithEmbedding struct {
		entry     NamedEntry
		embedding []float32
	}

	var candidates []entryWithEmbedding
	for rows.Next() {
		var entry NamedEntry
		var embeddingJSON []byte

		if err := scanEntryValues(rows, &entry, &embeddingJSON); err != nil {
			return nil, fmt.Errorf("memory: search similar by type scan entry: %w", err)
		}

		embedding, err := decodeStoredEmbeddingJSON(embeddingJSON, len(queryEmbedding))
		if err != nil {
			return nil, fmt.Errorf("memory: search similar by type decode embedding for %q: %w", entry.Name, err)
		}

		candidates = append(candidates, entryWithEmbedding{entry: entry, embedding: embedding})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: search similar by type rows: %w", err)
	}

	// Build candidate list for the vector searcher.
	idEmbeds := make([]vector.IDEmbedding, len(candidates))
	for i, c := range candidates {
		idEmbeds[i] = vector.IDEmbedding{ID: c.entry.ID, Embedding: c.embedding}
	}

	// Use the vector searcher for cosine similarity ranking.
	searcher := s.getSearcher(len(queryEmbedding))
	scored, err := searcher.Search(ctx, queryEmbedding, idEmbeds, limit)
	if err != nil {
		// Fallback to inline cosine if searcher fails.
		scored = nil
		for _, c := range candidates {
			similarity := vector.Cosine(queryEmbedding, c.embedding)
			if similarity > 0.5 {
				scored = append(scored, vector.ScoredID{ID: c.entry.ID, Score: similarity})
			}
		}
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].Score > scored[j].Score
		})
		if len(scored) > limit {
			scored = scored[:limit]
		}
	}

	// Build lookup map for entries by ID.
	entryMap := make(map[string]NamedEntry, len(candidates))
	for _, c := range candidates {
		entryMap[c.entry.ID] = c.entry
	}

	// Map scored results back to ScoredEntry, applying threshold.
	results := make([]ScoredEntry, 0, len(scored))
	for _, s := range scored {
		if s.Score <= 0.5 {
			continue
		}
		if entry, ok := entryMap[s.ID]; ok {
			results = append(results, ScoredEntry{
				Entry: entry,
				Score: s.Score,
			})
		}
	}

	return results, nil
}

type entryScanner interface {
	Scan(dest ...any) error
}

func scanEntries(rows *sql.Rows) ([]NamedEntry, error) {
	var out []NamedEntry
	for rows.Next() {
		var entry NamedEntry
		if err := scanEntryValues(rows, &entry); err != nil {
			return nil, fmt.Errorf("memory: scan: %w", err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: rows: %w", err)
	}
	return out, nil
}

func scanEntry(scanner entryScanner) (NamedEntry, error) {
	var entry NamedEntry
	if err := scanEntryValues(scanner, &entry); err != nil {
		return NamedEntry{}, err
	}
	return entry, nil
}

func scanEntryValues(scanner entryScanner, entry *NamedEntry, extra ...any) error {
	var digests string
	var created, updated, last string
	var sessionID sql.NullString
	var lifecycleState, reviewStatus, supersededBy, reviewNotes sql.NullString
	var lastUsedAt, lastValidatedAt sql.NullString
	var lastSelectedAt, lastSucceededAt, lastFailedAt, lastPatchedAt, lastRestoredAt sql.NullString
	var atomicText, entities, keywords sql.NullString
	var pinned int
	dest := []any{
		&entry.ID,
		&entry.Name,
		&entry.Type,
		&entry.Workspace,
		&entry.Summary,
		&entry.Result,
		&digests,
		&created,
		&updated,
		&last,
		&entry.AccessCount,
		&sessionID,
		&lifecycleState,
		&pinned,
		&reviewStatus,
		&supersededBy,
		&reviewNotes,
		&lastUsedAt,
		&lastValidatedAt,
		&entry.SelectedCount,
		&entry.UseCount,
		&entry.SuccessCount,
		&entry.FailureCount,
		&entry.PatchCount,
		&entry.RestoreCount,
		&lastSelectedAt,
		&lastSucceededAt,
		&lastFailedAt,
		&lastPatchedAt,
		&lastRestoredAt,
		&atomicText,
		&entities,
		&keywords,
	}
	dest = append(dest, extra...)
	if err := scanner.Scan(dest...); err != nil {
		return err
	}
	if err := validateMemoryTelemetryCounters(*entry); err != nil {
		return fmt.Errorf("scan telemetry: %w", err)
	}
	if err := sqlutil.ScanJSON(digests, &entry.Digests); err != nil {
		return fmt.Errorf("scan digests: %w", err)
	}
	if atomicText.Valid {
		entry.AtomicText = atomicText.String
	}
	if entities.Valid {
		if err := sqlutil.ScanJSON(entities.String, &entry.Entities); err != nil {
			return fmt.Errorf("scan entities: %w", err)
		}
	}
	if keywords.Valid {
		if err := sqlutil.ScanJSON(keywords.String, &entry.Keywords); err != nil {
			return fmt.Errorf("scan keywords: %w", err)
		}
	}
	var err error
	entry.CreatedAt, err = sqlutil.ScanTimestamp(created)
	if err != nil {
		return fmt.Errorf("scan created_at: %w", err)
	}
	entry.UpdatedAt, err = sqlutil.ScanTimestamp(updated)
	if err != nil {
		return fmt.Errorf("scan updated_at: %w", err)
	}
	entry.LastAccess, err = sqlutil.ScanTimestamp(last)
	if err != nil {
		return fmt.Errorf("scan last_accessed: %w", err)
	}
	if sessionID.Valid {
		entry.SessionID = sessionID.String
	}
	entry.LifecycleState, err = normalizeMemoryLifecycleState("")
	if err != nil {
		return fmt.Errorf("scan lifecycle_state: %w", err)
	}
	if lifecycleState.Valid {
		entry.LifecycleState, err = normalizeMemoryLifecycleState(lifecycleState.String)
		if err != nil {
			return fmt.Errorf("scan lifecycle_state: %w", err)
		}
	}
	entry.Pinned = pinned != 0
	entry.ReviewStatus, err = normalizeMemoryReviewStatus("")
	if err != nil {
		return fmt.Errorf("scan review_status: %w", err)
	}
	if reviewStatus.Valid {
		entry.ReviewStatus, err = normalizeMemoryReviewStatus(reviewStatus.String)
		if err != nil {
			return fmt.Errorf("scan review_status: %w", err)
		}
	}
	if supersededBy.Valid {
		entry.SupersededBy = supersededBy.String
	}
	if reviewNotes.Valid {
		entry.ReviewNotes = reviewNotes.String
	}
	if lastUsedAt.Valid {
		entry.LastUsedAt, err = sqlutil.ScanTimestamp(lastUsedAt.String)
		if err != nil {
			return fmt.Errorf("scan last_used_at: %w", err)
		}
	}
	if lastValidatedAt.Valid {
		entry.LastValidatedAt, err = sqlutil.ScanTimestamp(lastValidatedAt.String)
		if err != nil {
			return fmt.Errorf("scan last_validated_at: %w", err)
		}
	}
	if lastSelectedAt.Valid {
		entry.LastSelectedAt, err = sqlutil.ScanTimestamp(lastSelectedAt.String)
		if err != nil {
			return fmt.Errorf("scan last_selected_at: %w", err)
		}
	}
	if lastSucceededAt.Valid {
		entry.LastSucceededAt, err = sqlutil.ScanTimestamp(lastSucceededAt.String)
		if err != nil {
			return fmt.Errorf("scan last_succeeded_at: %w", err)
		}
	}
	if lastFailedAt.Valid {
		entry.LastFailedAt, err = sqlutil.ScanTimestamp(lastFailedAt.String)
		if err != nil {
			return fmt.Errorf("scan last_failed_at: %w", err)
		}
	}
	if lastPatchedAt.Valid {
		entry.LastPatchedAt, err = sqlutil.ScanTimestamp(lastPatchedAt.String)
		if err != nil {
			return fmt.Errorf("scan last_patched_at: %w", err)
		}
	}
	if lastRestoredAt.Valid {
		entry.LastRestoredAt, err = sqlutil.ScanTimestamp(lastRestoredAt.String)
		if err != nil {
			return fmt.Errorf("scan last_restored_at: %w", err)
		}
	}
	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func scoreEntry(entry NamedEntry) float64 {
	recencyHours := time.Since(entry.LastAccess).Hours()
	if recencyHours < 0 {
		recencyHours = 0
	}
	recency := 1.0 / (1.0 + recencyHours/24.0)
	frequency := math.Log1p(float64(entry.AccessCount))
	return 0.6*recency + 0.4*frequency
}

// UpdateAtomic stores atomic processing results for a named memory entry.
// atomicText is the self-contained rewrite, entities are extracted identifiers,
// keywords are BM25-optimized search terms.
func (s *Store) UpdateAtomic(ctx context.Context, name, workspace, atomicText string, entities, keywords []string) error {
	workspace = ws.CanonicalID(workspace)
	entitiesJSON, err := sqlutil.FormatJSON(entities)
	if err != nil {
		return fmt.Errorf("memory: marshal entities: %w", err)
	}
	keywordsJSON, err := sqlutil.FormatJSON(keywords)
	if err != nil {
		return fmt.Errorf("memory: marshal keywords: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE named_memory
		SET atomic_text = $1, entities = $2, keywords = $3, updated_at = $4
		WHERE name = $5 AND workspace = $6
	`, atomicText, entitiesJSON, keywordsJSON, sqlutil.FormatTimestamp(timeutil.NowUTC()), name, workspace)
	if err != nil {
		return fmt.Errorf("memory: update atomic: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("memory: entry not found: %s in workspace %s", name, workspace)
	}
	return nil
}
