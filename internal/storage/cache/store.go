package cache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	canonicaljson "github.com/gibson042/canonicaljson-go"
	"github.com/joshka0/foxctl/internal/adapters/artifacts"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/domain/skill"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/platform/metrics"
	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

// Mode controls cache behavior for runs.
type Mode string

const (
	// ModeAuto enables caching for skill execution results.
	ModeAuto Mode = "auto"
	// ModeOff disables all caching operations.
	ModeOff Mode = "off"
	// ModeOnly returns cached results only, never executing skills.
	ModeOnly Mode = "only"
)

// Ensure Store implements the storage.CacheStore interface.
var _ storage.CacheStore = (*Store)(nil)

// Entry aliases the shared cache entry type for backward compatibility.
type Entry = storage.CacheEntry

// Options controls store behavior.
type Options struct {
	AutoTTL time.Duration
	CASPath string
}

// Store persists auto-cache entries.
type Store struct {
	db              *sql.DB
	cas             *cas.Store
	artifactManager artifacts.Manager
	ttl             time.Duration
	path            string
	close           func() error
	mu              sync.Mutex
	evictDone       chan struct{} // signals async eviction completed
}

// Stats aliases the shared cache stats type.
type Stats = storage.CacheStats

// Open initializes and returns a cache Store rooted at the given filesystem path.
//
// It ensures the cache database and schema exist, applies a default AutoTTL when
// not provided, and configures optional content-addressable storage if a CASPath
// is set in opts. The returned Store starts background eviction of expired
// entries; callers should call Store.Close to release resources and wait for
// background work to finish. An error is returned if initialization fails.
func Open(ctx context.Context, root string, opts Options) (store *Store, err error) {
	if opts.AutoTTL <= 0 {
		opts.AutoTTL = 24 * time.Hour
	}
	dbPath := filepath.Join(root, "cache.db")
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "CACHE", "cache.db", migrate)
	if err != nil {
		return nil, fmt.Errorf("cache: open db: %w", err)
	}
	defer func() {
		if err != nil {
			_ = closeFn()
		}
	}()
	// Configure connection pool for optimal performance
	db.SetMaxOpenConns(10)                  // Allow up to 10 concurrent connections
	db.SetMaxIdleConns(5)                   // Keep 5 idle connections ready
	db.SetConnMaxLifetime(time.Hour)        // Recycle connections after 1 hour
	db.SetConnMaxIdleTime(15 * time.Minute) // Close idle connections after 15 min
	// dbutil.OpenStoreDB (sqliteutil underneath for sqlite) already ensures directory creation, WAL mode, and schema migrations.

	var casStore *cas.Store
	var artifactMgr artifacts.Manager
	if opts.CASPath != "" {
		if casStore, err = cas.NewStore(opts.CASPath); err != nil {
			return nil, err
		}
		artifactMgr = artifacts.NewManager(casStore)
	}
	store = &Store{
		db:              db,
		cas:             casStore,
		artifactManager: artifactMgr,
		ttl:             opts.AutoTTL,
		path:            dbPath,
		close:           closeFn,
		evictDone:       make(chan struct{}),
	}
	// Evict expired entries asynchronously to avoid blocking startup.
	// This moves cache maintenance off the critical path.
	go func() {
		defer close(store.evictDone)
		errs.Ignore(store.evictExpired(context.Background()), "cache: async eviction")
	}()
	return store, nil
}

// Close releases resources. It waits for any async eviction to complete before closing.
func (s *Store) Close() error {
	// Wait for async eviction to complete before closing DB
	if s != nil && s.evictDone != nil {
		select {
		case <-s.evictDone:
		case <-time.After(2 * time.Second):
		}
	}
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// Stats returns entry counts and configuration metadata for observability commands.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auto_cache`).Scan(&count); err != nil {
		return Stats{}, fmt.Errorf("cache: stats: %w", err)
	}
	return Stats{Entries: count, Path: s.path, TTL: s.ttl}, nil
}

// Put upserts an auto-cache entry and pins any referenced artifacts.
func (s *Store) Put(ctx context.Context, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateEntryIdentity(entry); err != nil {
		return err
	}

	now := timeutil.NowUTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.LastAccessed = now
	if entry.ExpiresAt.IsZero() {
		entry.ExpiresAt = entry.CreatedAt.Add(s.ttl)
	}

	// Format digests with proper error handling
	digestJSON, err := sqlutil.FormatJSON(entry.Digests)
	if err != nil {
		return fmt.Errorf("cache: format digests: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
			INSERT INTO auto_cache (cache_key, skill_name, skill_version, workspace, result, digests, created_at, expires_at, last_accessed, hit_count)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 0)
			ON CONFLICT(cache_key) DO UPDATE SET
				result = excluded.result,
				digests = excluded.digests,
				workspace = excluded.workspace,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at,
			last_accessed = excluded.last_accessed
	`, entry.CacheKey, entry.SkillName, entry.SkillVersion, entry.Workspace, entry.Result, digestJSON,
		sqlutil.FormatTimestamp(entry.CreatedAt), sqlutil.FormatTimestamp(entry.ExpiresAt), sqlutil.FormatTimestamp(entry.LastAccessed))
	if err != nil {
		return fmt.Errorf("cache: put: %w", err)
	}
	s.pinDigests(ctx, entry.Digests)
	return nil
}

// Get retrieves a cache entry by key, returning ok=false for misses/expired entries.
func (s *Store) Get(ctx context.Context, key string) (Entry, bool, error) {
	if err := s.evictExpired(ctx); err != nil {
		return Entry{}, false, err
	}
	row := s.db.QueryRowContext(ctx, `
			SELECT cache_key, skill_name, skill_version, workspace, result, digests, created_at, expires_at, last_accessed, hit_count
			FROM auto_cache
			WHERE cache_key = $1`, key)
	var entry Entry
	var digests string
	var created, expires, last string
	if err := row.Scan(&entry.CacheKey, &entry.SkillName, &entry.SkillVersion, &entry.Workspace, &entry.Result, &digests, &created, &expires, &last, &entry.HitCount); err != nil {
		if dbutil.IsNoRows(err) {
			metrics.Global().RecordCacheMiss()
			return Entry{}, false, nil
		}
		return Entry{}, false, fmt.Errorf("cache: scan: %w", err)
	}
	metrics.Global().RecordCacheHit()

	if err := decodeEntryFields(&entry, digests, created, expires, last, "scan"); err != nil {
		return Entry{}, false, err
	}

	// refresh access metadata (best-effort)
	entry.LastAccessed = timeutil.NowUTC()
	if _, updateErr := s.db.ExecContext(ctx, `
			UPDATE auto_cache
			SET last_accessed = $1, hit_count = hit_count + 1
			WHERE cache_key = $2`,
		sqlutil.FormatTimestamp(entry.LastAccessed), key); updateErr != nil {
		errs.Ignore(updateErr, "cache: refresh access metadata")
	}
	return entry, true, nil
}

// Recent lists the newest cache entries for a workspace (or all if empty).
func (s *Store) Recent(ctx context.Context, workspace string, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 20
	}
	if err := s.evictExpired(ctx); err != nil {
		return nil, err
	}
	var rows *sql.Rows
	var err error
	if workspace == "" {
		rows, err = s.db.QueryContext(ctx, `
				SELECT cache_key, skill_name, skill_version, workspace, result, digests, created_at, expires_at, last_accessed, hit_count
				FROM auto_cache
				ORDER BY created_at DESC
				LIMIT $1`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
				SELECT cache_key, skill_name, skill_version, workspace, result, digests, created_at, expires_at, last_accessed, hit_count
				FROM auto_cache
				WHERE workspace = $1
				ORDER BY created_at DESC
				LIMIT $2`, workspace, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("cache: recent: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close cache recent rows")
	}()

	entries := make([]Entry, 0, limit)
	for rows.Next() {
		var entry Entry
		var digests string
		var created, expires, last string
		if err := rows.Scan(&entry.CacheKey, &entry.SkillName, &entry.SkillVersion, &entry.Workspace, &entry.Result, &digests, &created, &expires, &last, &entry.HitCount); err != nil {
			return nil, fmt.Errorf("cache: scan recent: %w", err)
		}

		if err := decodeEntryFields(&entry, digests, created, expires, last, "scan recent"); err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}
	return entries, nil
}

// Delete removes a cache entry by key and unpins its artifacts.
func (s *Store) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRowContext(ctx, `SELECT digests FROM auto_cache WHERE cache_key = $1`, key)
	var digestsJSON string
	if err := row.Scan(&digestsJSON); err != nil {
		if dbutil.IsNoRows(err) {
			return nil
		}
		return fmt.Errorf("cache: get digests for delete: %w", err)
	}

	var digests []string
	if err := sqlutil.ScanJSON(digestsJSON, &digests); err != nil {
		return fmt.Errorf("cache: scan digests for delete: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM auto_cache WHERE cache_key = $1`, key); err != nil {
		return fmt.Errorf("cache: delete: %w", err)
	}
	s.unpinDigests(ctx, digests)
	return nil
}

// AnnotateHit mutates the envelope metadata for cache hits.
// Deprecated: Use AnnotateHitBytes which uses the protocol package.
func AnnotateHit(result []byte, cacheKey, workspace, skillVersion string) ([]byte, error) {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err != nil {
		return nil, fmt.Errorf("cache: parse envelope: %w", err)
	}
	env.Meta.Source = "cache"
	env.Meta.CacheKey = cacheKey
	if workspace != "" {
		env.Meta.Workspace = workspace
	}
	if skillVersion != "" {
		env.Meta.SkillVer = skillVersion
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("cache: encode envelope: %w", err)
	}
	return data, nil
}

// AnnotateHitBytes is a wrapper around protocol.AnnotateCacheHitBytes.
// It exists to avoid import cycles and maintain backward compatibility.
func AnnotateHitBytes(result []byte, cacheKey, workspace, skillVersion string) ([]byte, error) {
	// For now, just use the old implementation to avoid import cycles
	// The protocol package provides the canonical implementation
	return AnnotateHit(result, cacheKey, workspace, skillVersion)
}

// AnnotateMemory mutates the envelope metadata for named memory retrievals.
func AnnotateMemory(result []byte, mem envelope.MemoryRef) ([]byte, error) {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err != nil {
		return nil, fmt.Errorf("memory: parse envelope: %w", err)
	}
	env.Meta.Source = "memory"
	env.Meta.Memory = &mem
	if mem.Workspace != "" {
		env.Meta.Workspace = mem.Workspace
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("memory: encode envelope: %w", err)
	}
	return data, nil
}

// BuildKey computes the deterministic cache key per spec.
func BuildKey(manifest skill.Manifest, input []byte, extraDigests []string) (string, error) {
	args, err := canonicalArgs(input)
	if err != nil {
		return "", err
	}
	digests := normalizeCacheKeyDigests(extraDigests)
	sort.Strings(digests)

	h := sha256.New()
	if _, err := h.Write([]byte(manifest.Metadata.Name)); err != nil {
		return "", fmt.Errorf("cache: hash skill name: %w", err)
	}
	if _, err := h.Write([]byte{0}); err != nil {
		return "", fmt.Errorf("cache: hash separator: %w", err)
	}
	if _, err := h.Write([]byte(manifest.Metadata.Version)); err != nil {
		return "", fmt.Errorf("cache: hash version: %w", err)
	}
	if _, err := h.Write([]byte{0}); err != nil {
		return "", fmt.Errorf("cache: hash separator: %w", err)
	}
	if _, err := h.Write(args); err != nil {
		return "", fmt.Errorf("cache: hash args: %w", err)
	}
	for _, d := range digests {
		if _, err := h.Write([]byte{0}); err != nil {
			return "", fmt.Errorf("cache: hash digest separator: %w", err)
		}
		if _, err := h.Write([]byte(d)); err != nil {
			return "", fmt.Errorf("cache: hash digest: %w", err)
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func normalizeCacheKeyDigests(digests []string) []string {
	seen := make(map[string]struct{}, len(digests))
	out := make([]string, 0, len(digests))
	for _, digest := range digests {
		digest = strings.TrimSpace(digest)
		if digest == "" {
			continue
		}
		if _, ok := seen[digest]; ok {
			continue
		}
		seen[digest] = struct{}{}
		out = append(out, digest)
	}
	return out
}

func canonicalArgs(input []byte) ([]byte, error) {
	if len(strings.TrimSpace(string(input))) == 0 {
		return canonicaljson.Marshal(map[string]any{})
	}
	var v any
	if err := json.Unmarshal(input, &v); err != nil {
		// fallback to treating input as raw string
		return canonicaljson.Marshal(string(input))
	}
	return canonicaljson.Marshal(v)
}

func (s *Store) evictExpired(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := timeutil.FormatNowUTC()
	rows, err := s.db.QueryContext(ctx, `SELECT cache_key, digests, expires_at FROM auto_cache WHERE expires_at <= $1`, now)
	if err != nil {
		return fmt.Errorf("cache: select expired: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close cache eviction rows")
	}()
	type doomed struct {
		key     string
		digests []string
	}
	var expired []doomed
	for rows.Next() {
		var d doomed
		var digests string
		var expires string
		if err := rows.Scan(&d.key, &digests, &expires); err != nil {
			return fmt.Errorf("cache: scan expired: %w", err)
		}
		if _, err := scanRequiredTimestamp(expires, "scan expired expires_at"); err != nil {
			return err
		}
		if err := sqlutil.ScanJSON(digests, &d.digests); err != nil {
			return fmt.Errorf("cache: scan expired digests: %w", err)
		}
		expired = append(expired, d)
	}
	for _, d := range expired {
		// Check for context cancellation
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM auto_cache WHERE cache_key = $1`, d.key); err != nil {
			return fmt.Errorf("cache: delete expired: %w", err)
		}
		s.unpinDigests(ctx, d.digests)
	}
	return nil
}

func validateEntryIdentity(entry Entry) error {
	if strings.TrimSpace(entry.CacheKey) == "" {
		return fmt.Errorf("cache: missing cache_key")
	}
	if strings.TrimSpace(entry.SkillName) == "" {
		return fmt.Errorf("cache: missing skill_name")
	}
	if strings.TrimSpace(entry.SkillVersion) == "" {
		return fmt.Errorf("cache: missing skill_version")
	}
	return nil
}

func decodeEntryFields(entry *Entry, digests, created, expires, last, context string) error {
	if entry.HitCount < 0 {
		return fmt.Errorf("cache: %s hit_count: must be non-negative", context)
	}
	if err := sqlutil.ScanJSON(digests, &entry.Digests); err != nil {
		return fmt.Errorf("cache: %s digests: %w", context, err)
	}

	var err error
	entry.CreatedAt, err = scanRequiredTimestamp(created, context+" created_at")
	if err != nil {
		return err
	}
	entry.ExpiresAt, err = scanRequiredTimestamp(expires, context+" expires_at")
	if err != nil {
		return err
	}
	entry.LastAccessed, err = scanRequiredTimestamp(last, context+" last_accessed")
	if err != nil {
		return err
	}
	return nil
}

func scanRequiredTimestamp(raw, field string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, fmt.Errorf("cache: %s: missing timestamp", field)
	}
	ts, err := sqlutil.ScanTimestamp(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("cache: %s: %w", field, err)
	}
	return ts, nil
}

func (s *Store) pinDigests(ctx context.Context, digests []string) {
	if s.artifactManager == nil {
		return
	}
	// Check for context cancellation
	if ctx.Err() != nil {
		return
	}
	if err := s.artifactManager.Pin(ctx, digests...); err != nil {
		errs.Ignore(err, "cache: pin digests")
	}
}

func (s *Store) unpinDigests(ctx context.Context, digests []string) {
	if s.artifactManager == nil {
		return
	}
	// Check for context cancellation
	if ctx.Err() != nil {
		return
	}
	if err := s.artifactManager.Unpin(ctx, digests...); err != nil {
		errs.Ignore(err, "cache: unpin digests")
	}
}

// MigrateSchema runs the cache store DDL migrations against the given database.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db)
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS auto_cache (
	cache_key TEXT PRIMARY KEY,
	skill_name TEXT NOT NULL,
	skill_version TEXT NOT NULL,
	workspace TEXT NOT NULL,
	result BLOB NOT NULL,
	digests TEXT NOT NULL,
	created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL,
	last_accessed TEXT NOT NULL,
	hit_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_auto_cache_workspace ON auto_cache(workspace);
CREATE INDEX IF NOT EXISTS idx_cache_expires ON auto_cache(expires_at);
CREATE INDEX IF NOT EXISTS idx_cache_ws_created ON auto_cache(workspace, created_at DESC);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("cache: migrate: %w", err)
	}
	return nil
}

// CollectDigests is a helper that extracts digests from an envelope.
func CollectDigests(result []byte) []string {
	return artifacts.Digests(result)
}
