package snapshotstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

const (
	storeName   = "REFACTOR_SNAPSHOTS"
	defaultFile = "refactor_snapshots.db"
)

// Record captures the small lookup row for a refactor snapshot artifact.
type Record struct {
	SnapshotID     string    `json:"snapshot_id"`
	Workspace      string    `json:"workspace"`
	RepoRoot       string    `json:"repo_root"`
	Path           string    `json:"path"`
	Language       string    `json:"language"`
	IncludeTests   bool      `json:"include_tests"`
	Mode           string    `json:"mode"`
	GitHeadSHA     string    `json:"git_head_sha,omitempty"`
	IndexHeadSHA   string    `json:"index_head_sha,omitempty"`
	ArtifactDigest string    `json:"artifact_digest"`
	FileCount      int       `json:"file_count"`
	SymbolCount    int       `json:"symbol_count"`
	CreatedAt      time.Time `json:"created_at"`
}

// Store persists refactor snapshot metadata rows.
type Store struct {
	db    *sql.DB
	close func() error
}

// Open opens the refactor snapshot metadata store under the configured storage root.
func Open(ctx context.Context, storageRoot string) (*Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, storageRoot, storeName, defaultFile, migrate)
	if err != nil {
		return nil, fmt.Errorf("open refactor snapshot store: %w", err)
	}
	return &Store{db: db, close: closeFn}, nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// Put inserts or replaces a snapshot metadata record.
func (s *Store) Put(ctx context.Context, record Record) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("refactor snapshot store is not open")
	}
	if strings.TrimSpace(record.SnapshotID) == "" {
		return fmt.Errorf("snapshot id is required")
	}
	if strings.TrimSpace(record.ArtifactDigest) == "" {
		return fmt.Errorf("artifact digest is required")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = timeutil.NowUTC()
	}
	includeTests := 0
	if record.IncludeTests {
		includeTests = 1
	}
	_, err := s.db.ExecContext(
		ctx, `
INSERT INTO refactor_snapshot (
    snapshot_id, workspace, repo_root, path, language, include_tests, mode,
    git_head_sha, index_head_sha, artifact_digest, file_count, symbol_count, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(snapshot_id) DO UPDATE SET
    workspace = excluded.workspace,
    repo_root = excluded.repo_root,
    path = excluded.path,
    language = excluded.language,
    include_tests = excluded.include_tests,
    mode = excluded.mode,
    git_head_sha = excluded.git_head_sha,
    index_head_sha = excluded.index_head_sha,
    artifact_digest = excluded.artifact_digest,
    file_count = excluded.file_count,
    symbol_count = excluded.symbol_count,
    created_at = excluded.created_at
`,
		record.SnapshotID,
		record.Workspace,
		record.RepoRoot,
		record.Path,
		record.Language,
		includeTests,
		record.Mode,
		record.GitHeadSHA,
		record.IndexHeadSHA,
		record.ArtifactDigest,
		record.FileCount,
		record.SymbolCount,
		timeutil.FormatRFC3339Nano(record.CreatedAt.UTC()),
	)
	if err != nil {
		return fmt.Errorf("insert refactor snapshot metadata: %w", err)
	}
	return nil
}

// Get returns a snapshot metadata record by snapshot ID.
func (s *Store) Get(ctx context.Context, snapshotID string) (Record, error) {
	if s == nil || s.db == nil {
		return Record{}, fmt.Errorf("refactor snapshot store is not open")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT
    snapshot_id, workspace, repo_root, path, language, include_tests, mode,
    git_head_sha, index_head_sha, artifact_digest, file_count, symbol_count, created_at
FROM refactor_snapshot
WHERE snapshot_id = ?
`, strings.TrimSpace(snapshotID))

	var record Record
	var includeTests int
	var createdAtRaw string
	if err := row.Scan(
		&record.SnapshotID,
		&record.Workspace,
		&record.RepoRoot,
		&record.Path,
		&record.Language,
		&includeTests,
		&record.Mode,
		&record.GitHeadSHA,
		&record.IndexHeadSHA,
		&record.ArtifactDigest,
		&record.FileCount,
		&record.SymbolCount,
		&createdAtRaw,
	); err != nil {
		return Record{}, err
	}
	record.IncludeTests = includeTests != 0
	createdAt, err := timeutil.ParseRFC3339Nano(createdAtRaw)
	if err != nil {
		return Record{}, fmt.Errorf("parse created_at: %w", err)
	}
	record.CreatedAt = createdAt.UTC()
	return record, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS refactor_snapshot (
    snapshot_id TEXT PRIMARY KEY,
    workspace TEXT NOT NULL,
    repo_root TEXT NOT NULL,
    path TEXT NOT NULL,
    language TEXT NOT NULL,
    include_tests INTEGER NOT NULL DEFAULT 0,
    mode TEXT NOT NULL,
    git_head_sha TEXT,
    index_head_sha TEXT,
    artifact_digest TEXT NOT NULL,
    file_count INTEGER NOT NULL DEFAULT 0,
    symbol_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_refactor_snapshot_workspace_created_at
    ON refactor_snapshot(workspace, created_at DESC);
`)
	if err != nil {
		return fmt.Errorf("migrate refactor snapshot store: %w", err)
	}
	return nil
}
