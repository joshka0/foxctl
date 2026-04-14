package transcriptcache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

// Entry is one persisted prederived transcript artifact.
type Entry struct {
	ArtifactKind   string    `json:"artifact_kind"`
	NormalizedHash string    `json:"normalized_hash"`
	SourceHash     string    `json:"source_hash"`
	DerivationMode string    `json:"derivation_mode"`
	ModelID        string    `json:"model_id"`
	PromptVersion  string    `json:"prompt_version"`
	Summary        string    `json:"summary"`
	SourcePreview  string    `json:"source_preview,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	HitCount       int       `json:"hit_count"`
}

// Store persists cacheable transcript artifacts.
type Store struct {
	db    *sql.DB
	path  string
	close func() error
}

// Open initializes the transcript artifact cache store.
func Open(ctx context.Context, root string) (*Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "TRANSCRIPTCACHE", "transcript_cache.db", migrate)
	if err != nil {
		return nil, fmt.Errorf("transcriptcache: open db: %w", err)
	}
	return &Store{
		db:    db,
		path:  filepath.Join(root, "transcript_cache.db"),
		close: closeFn,
	}, nil
}

// OpenShared opens the shared transcript cache under the configured storage root,
// falling back to the historical Codex cache path when needed.
func OpenShared(ctx context.Context, storageRoot string) (*Store, string, error) {
	homeDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		homeDir = home
	}
	candidates := SharedRoots(storageRoot, homeDir)

	var errs []string
	for _, root := range candidates {
		if err := os.MkdirAll(root, 0o755); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", root, err))
			continue
		}
		store, err := Open(ctx, root)
		if err == nil {
			return store, root, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", root, err))
	}
	return nil, "", fmt.Errorf("open transcript cache store: %s", strings.Join(errs, " | "))
}

// SharedRoots returns the preferred transcript-cache roots in priority order.
func SharedRoots(storageRoot, homeDir string) []string {
	candidates := make([]string, 0, 2)
	if root := strings.TrimSpace(storageRoot); root != "" {
		candidates = append(candidates, root)
	}
	if home := strings.TrimSpace(homeDir); home != "" {
		candidates = append(candidates, filepath.Join(home, ".codex", "memories", "foxctl-transcript-cache"))
	}
	return candidates
}

// Close releases resources.
func (s *Store) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// Path returns the underlying sqlite file path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// GetByNormalizedHash returns a cached entry and bumps its hit count.
func (s *Store) GetByNormalizedHash(ctx context.Context, artifactKind, normalizedHash, promptVersion, modelID string) (Entry, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT artifact_kind, normalized_hash, source_hash, derivation_mode, model_id,
		       prompt_version, summary, source_preview, created_at, updated_at, hit_count
		FROM transcript_prederived
		WHERE artifact_kind = $1
		  AND normalized_hash = $2
		  AND prompt_version = $3
		  AND model_id = $4
	`, strings.TrimSpace(artifactKind), strings.TrimSpace(normalizedHash), strings.TrimSpace(promptVersion), strings.TrimSpace(modelID))

	var entry Entry
	var sourcePreview sql.NullString
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&entry.ArtifactKind,
		&entry.NormalizedHash,
		&entry.SourceHash,
		&entry.DerivationMode,
		&entry.ModelID,
		&entry.PromptVersion,
		&entry.Summary,
		&sourcePreview,
		&createdAt,
		&updatedAt,
		&entry.HitCount,
	); err != nil {
		if dbutil.IsNoRows(err) {
			return Entry{}, false, nil
		}
		return Entry{}, false, fmt.Errorf("transcriptcache: get: %w", err)
	}
	entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	entry.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	if sourcePreview.Valid {
		entry.SourcePreview = sourcePreview.String
	}

	_, _ = s.db.ExecContext(ctx, `
		UPDATE transcript_prederived
		SET hit_count = hit_count + 1,
		    updated_at = $5
		WHERE artifact_kind = $1
		  AND normalized_hash = $2
		  AND prompt_version = $3
		  AND model_id = $4
	`, entry.ArtifactKind, entry.NormalizedHash, entry.PromptVersion, entry.ModelID, time.Now().UTC().Format(time.RFC3339Nano))

	entry.HitCount++
	return entry, true, nil
}

// Put upserts a cached transcript artifact.
func (s *Store) Put(ctx context.Context, entry Entry) error {
	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO transcript_prederived (
			artifact_kind, normalized_hash, source_hash, derivation_mode, model_id,
			prompt_version, summary, source_preview, created_at, updated_at, hit_count
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 0)
		ON CONFLICT(artifact_kind, normalized_hash, prompt_version, model_id) DO UPDATE SET
			source_hash = excluded.source_hash,
			derivation_mode = excluded.derivation_mode,
			summary = excluded.summary,
			source_preview = excluded.source_preview,
			updated_at = excluded.updated_at
	`, strings.TrimSpace(entry.ArtifactKind), strings.TrimSpace(entry.NormalizedHash), strings.TrimSpace(entry.SourceHash),
		strings.TrimSpace(entry.DerivationMode), strings.TrimSpace(entry.ModelID), strings.TrimSpace(entry.PromptVersion),
		strings.TrimSpace(entry.Summary), strings.TrimSpace(entry.SourcePreview),
		entry.CreatedAt.Format(time.RFC3339Nano), entry.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("transcriptcache: put: %w", err)
	}
	return nil
}

// DigestText returns a stable sha256: digest for normalized keys.
func DigestText(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS transcript_prederived (
			artifact_kind TEXT NOT NULL,
			normalized_hash TEXT NOT NULL,
			source_hash TEXT NOT NULL,
			derivation_mode TEXT NOT NULL,
			model_id TEXT NOT NULL,
			prompt_version TEXT NOT NULL,
			summary TEXT NOT NULL,
			source_preview TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			hit_count INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (artifact_kind, normalized_hash, prompt_version, model_id)
		);

		CREATE INDEX IF NOT EXISTS idx_transcript_prederived_kind_hash
			ON transcript_prederived(artifact_kind, normalized_hash);
	`)
	return err
}
