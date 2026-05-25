package cas

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/platform/metrics"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
)

// SQLiteStore implements CASStore using SQLite as the backend.
type SQLiteStore struct {
	db            dbdriver.DB
	blobThreshold int64 // Size threshold for inline storage
	mu            sync.RWMutex
}

// Ensure SQLiteStore implements storage.CASStore.
var _ storage.CASStore = (*SQLiteStore)(nil)

// NewSQLiteStore creates a new SQLite-backed CAS store.
func NewSQLiteStore(ctx context.Context, cfg SQLiteConfig) (*SQLiteStore, error) {
	threshold := cfg.BlobThreshold
	if threshold == 0 {
		threshold = 1 << 20 // Default 1MB
	}

	dbCfg := dbdriver.Config{
		Driver: dbdriver.DriverSQLite,
		SQLite: dbdriver.SQLiteConfig{
			Path:        cfg.DBPath,
			EnableWAL:   cfg.EnableWAL,
			BusyTimeout: cfg.BusyTimeout,
		},
	}

	db, err := dbdriver.OpenDB(ctx, dbCfg, migrateSQLiteSchema)
	if err != nil {
		return nil, fmt.Errorf("cas: open sqlite: %w", err)
	}

	return &SQLiteStore{
		db:            db,
		blobThreshold: threshold,
	}, nil
}

// migrateSQLiteSchema creates the CAS tables.
func migrateSQLiteSchema(ctx context.Context, db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS cas_objects (
		digest TEXT PRIMARY KEY,
		size INTEGER NOT NULL,
		kind TEXT DEFAULT 'application/octet-stream',
		tags TEXT,
		pinned INTEGER DEFAULT 0,
		content BLOB,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_cas_created ON cas_objects(created_at);
	CREATE INDEX IF NOT EXISTS idx_cas_pinned ON cas_objects(pinned);
	CREATE INDEX IF NOT EXISTS idx_cas_kind ON cas_objects(kind);
	`

	_, err := db.ExecContext(ctx, schema)
	return err
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Put stores content and returns metadata.
func (s *SQLiteStore) Put(ctx context.Context, r io.Reader, kind string, tags []string) (Object, error) {
	if kind == "" {
		kind = "application/octet-stream"
	}

	// Read all content to compute digest
	content, err := io.ReadAll(r)
	if err != nil {
		return Object{}, fmt.Errorf("cas: read input: %w", err)
	}

	h := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(h[:])
	size := int64(len(content))
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if object already exists
	var existingDigest string
	err = s.db.QueryRowContext(ctx, "SELECT digest FROM cas_objects WHERE digest = ?", digest).Scan(&existingDigest)
	if err == nil {
		// Object exists, merge tags
		return s.mergeTagsAndReturn(ctx, digest, tags)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Object{}, fmt.Errorf("cas: check existing: %w", err)
	}

	// Marshal tags
	var tagsJSON []byte
	if len(tags) > 0 {
		tagsJSON, err = json.Marshal(mergeTags(nil, tags))
		if err != nil {
			return Object{}, fmt.Errorf("cas: marshal tags: %w", err)
		}
	}

	// Insert new object
	timeStr := now.Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO cas_objects (digest, size, kind, tags, pinned, content, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?)
	`, digest, size, kind, tagsJSON, content, timeStr, timeStr)
	if err != nil {
		return Object{}, fmt.Errorf("cas: insert: %w", err)
	}

	metrics.Global().RecordCASOperation()

	parsedTags := make([]string, 0)
	if tagsJSON != nil {
		_ = json.Unmarshal(tagsJSON, &parsedTags)
	}

	return Object{
		Metadata: Metadata{
			Digest:    digest,
			Size:      size,
			Kind:      kind,
			Tags:      parsedTags,
			CreatedAt: now,
		},
		Pinned: false,
	}, nil
}

func (s *SQLiteStore) mergeTagsAndReturn(ctx context.Context, digest string, newTags []string) (Object, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT size, kind, tags, pinned, created_at
		FROM cas_objects WHERE digest = ?
	`, digest)

	var size int64
	var kind string
	var tagsJSON sql.NullString
	var pinned int
	var createdAtStr string

	if err := row.Scan(&size, &kind, &tagsJSON, &pinned, &createdAtStr); err != nil {
		return Object{}, fmt.Errorf("cas: read existing: %w", err)
	}

	existingTags, err := decodeCASTags(tagsJSON)
	if err != nil {
		return Object{}, err
	}

	merged := mergeTags(existingTags, newTags)
	if len(merged) != len(existingTags) {
		mergedJSON, err := json.Marshal(merged)
		if err != nil {
			return Object{}, fmt.Errorf("cas: marshal merged tags: %w", err)
		}

		_, err = s.db.ExecContext(ctx, `
			UPDATE cas_objects SET tags = ?, updated_at = ? WHERE digest = ?
		`, mergedJSON, time.Now().UTC().Format(time.RFC3339Nano), digest)
		if err != nil {
			return Object{}, fmt.Errorf("cas: update tags: %w", err)
		}
		existingTags = merged
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)

	return Object{
		Metadata: Metadata{
			Digest:    digest,
			Size:      size,
			Kind:      kind,
			Tags:      existingTags,
			CreatedAt: createdAt,
		},
		Pinned: pinned != 0,
	}, nil
}

// Get retrieves content by digest.
func (s *SQLiteStore) Get(ctx context.Context, digest string) (io.ReadCloser, Metadata, error) {
	if !digestPattern.MatchString(digest) {
		return nil, Metadata{}, fmt.Errorf("cas: invalid digest")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRowContext(ctx, `
		SELECT size, kind, tags, content, created_at
		FROM cas_objects WHERE digest = ?
	`, digest)

	var size int64
	var kind string
	var tagsJSON sql.NullString
	var content []byte
	var createdAtStr string

	if err := row.Scan(&size, &kind, &tagsJSON, &content, &createdAtStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, Metadata{}, ErrNotFound
		}
		return nil, Metadata{}, fmt.Errorf("cas: get: %w", err)
	}

	tags, err := decodeCASTags(tagsJSON)
	if err != nil {
		return nil, Metadata{}, err
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)

	meta := Metadata{
		Digest:    digest,
		Size:      size,
		Kind:      kind,
		Tags:      tags,
		CreatedAt: createdAt,
	}

	metrics.Global().RecordCASOperation()

	return io.NopCloser(bytes.NewReader(content)), meta, nil
}

// Head returns metadata for a digest.
func (s *SQLiteStore) Head(ctx context.Context, digest string) (Object, error) {
	if !digestPattern.MatchString(digest) {
		return Object{}, fmt.Errorf("cas: invalid digest")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRowContext(ctx, `
		SELECT size, kind, tags, pinned, created_at
		FROM cas_objects WHERE digest = ?
	`, digest)

	var size int64
	var kind string
	var tagsJSON sql.NullString
	var pinned int
	var createdAtStr string

	if err := row.Scan(&size, &kind, &tagsJSON, &pinned, &createdAtStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Object{}, ErrNotFound
		}
		return Object{}, fmt.Errorf("cas: head: %w", err)
	}

	tags, err := decodeCASTags(tagsJSON)
	if err != nil {
		return Object{}, err
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)

	return Object{
		Metadata: Metadata{
			Digest:    digest,
			Size:      size,
			Kind:      kind,
			Tags:      tags,
			CreatedAt: createdAt,
		},
		Pinned: pinned != 0,
	}, nil
}

// List returns all stored objects.
func (s *SQLiteStore) List(ctx context.Context) ([]Object, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT digest, size, kind, tags, pinned, created_at
		FROM cas_objects ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("cas: list: %w", err)
	}
	defer rows.Close()

	var objects []Object
	for rows.Next() {
		var digest string
		var size int64
		var kind string
		var tagsJSON sql.NullString
		var pinned int
		var createdAtStr string

		if err := rows.Scan(&digest, &size, &kind, &tagsJSON, &pinned, &createdAtStr); err != nil {
			continue
		}

		tags, err := decodeCASTags(tagsJSON)
		if err != nil {
			return nil, err
		}

		createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)

		objects = append(objects, Object{
			Metadata: Metadata{
				Digest:    digest,
				Size:      size,
				Kind:      kind,
				Tags:      tags,
				CreatedAt: createdAt,
			},
			Pinned: pinned != 0,
		})
	}

	return objects, rows.Err()
}

// Remove deletes an object by digest.
func (s *SQLiteStore) Remove(ctx context.Context, digest string) error {
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("cas: invalid digest")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if pinned
	var pinned int
	err := s.db.QueryRowContext(ctx, "SELECT pinned FROM cas_objects WHERE digest = ?", digest).Scan(&pinned)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("cas: check pinned: %w", err)
	}

	if pinned != 0 {
		return ErrPinned
	}

	result, err := s.db.ExecContext(ctx, "DELETE FROM cas_objects WHERE digest = ?", digest)
	if err != nil {
		return fmt.Errorf("cas: remove: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}

	metrics.Global().RecordCASOperation()
	return nil
}

// Pin marks an object as pinned.
func (s *SQLiteStore) Pin(ctx context.Context, digest string) error {
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("cas: invalid digest")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx, `
		UPDATE cas_objects SET pinned = 1, updated_at = ? WHERE digest = ?
	`, time.Now().UTC().Format(time.RFC3339Nano), digest)
	if err != nil {
		return fmt.Errorf("cas: pin: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}

	metrics.Global().RecordCASOperation()
	return nil
}

// Unpin removes the pinned status.
func (s *SQLiteStore) Unpin(ctx context.Context, digest string) error {
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("cas: invalid digest")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx, `
		UPDATE cas_objects SET pinned = 0, updated_at = ? WHERE digest = ?
	`, time.Now().UTC().Format(time.RFC3339Nano), digest)
	if err != nil {
		return fmt.Errorf("cas: unpin: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}

	metrics.Global().RecordCASOperation()
	return nil
}

// AddTags adds tags to an existing object.
func (s *SQLiteStore) AddTags(ctx context.Context, digest string, tags []string) error {
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("cas: invalid digest")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Get existing tags
	var tagsJSON sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT tags FROM cas_objects WHERE digest = ?", digest).Scan(&tagsJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("cas: get tags: %w", err)
	}

	existingTags, err := decodeCASTags(tagsJSON)
	if err != nil {
		return err
	}

	merged := mergeTags(existingTags, tags)
	sort.Strings(merged)

	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return fmt.Errorf("cas: marshal tags: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE cas_objects SET tags = ?, updated_at = ? WHERE digest = ?
	`, mergedJSON, time.Now().UTC().Format(time.RFC3339Nano), digest)
	if err != nil {
		return fmt.Errorf("cas: update tags: %w", err)
	}

	return nil
}

// GC performs garbage collection.
func (s *SQLiteStore) GC(ctx context.Context, opts GCOptions) (GCResult, error) {
	if err := ctx.Err(); err != nil {
		return GCResult{}, err
	}

	objects, err := s.List(ctx)
	if err != nil {
		return GCResult{}, err
	}

	cutoff := time.Now().Add(-opts.OlderThan)
	var result GCResult

	for _, obj := range objects {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		if opts.KeepPinned && obj.Pinned {
			result.ObjectsSkipped++
			continue
		}

		if opts.OlderThan > 0 && obj.CreatedAt.After(cutoff) {
			result.ObjectsSkipped++
			continue
		}

		if opts.DryRun {
			result.ObjectsDeleted++
			result.BytesFreed += obj.Size
		} else {
			if err := s.Remove(ctx, obj.Digest); err != nil {
				if errors.Is(err, ErrPinned) {
					result.ObjectsSkipped++
					continue
				}
				result.Errors++
				continue
			}
			result.ObjectsDeleted++
			result.BytesFreed += obj.Size
		}

		if opts.MaxDelete > 0 && result.ObjectsDeleted >= opts.MaxDelete {
			break
		}
	}

	return result, ctx.Err()
}
