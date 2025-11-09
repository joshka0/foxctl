package cache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	canonicaljson "github.com/gibson042/canonicaljson-go"
	"github.com/jkatigb/agentctl/internal/artifacts"
	"github.com/jkatigb/agentctl/internal/cas"
	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/skill"
	_ "modernc.org/sqlite" // sqlite driver
)

// Mode controls cache behavior for runs.
type Mode string

const (
	ModeAuto Mode = "auto"
	ModeOff  Mode = "off"
	ModeOnly Mode = "only"
)

// Entry captures auto-cache metadata.
type Entry struct {
	CacheKey     string
	SkillName    string
	SkillVersion string
	Workspace    string
	Result       []byte
	Digests      []string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastAccessed time.Time
	HitCount     int
}

// Options controls store behavior.
type Options struct {
	AutoTTL time.Duration
	CASPath string
}

// Store persists auto-cache entries.
type Store struct {
	db   *sql.DB
	cas  *cas.Store
	ttl  time.Duration
	path string
	mu   sync.Mutex
}

// Open initializes the cache store at the provided path.
func Open(ctx context.Context, root string, opts Options) (*Store, error) {
	if opts.AutoTTL <= 0 {
		opts.AutoTTL = 24 * time.Hour
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("cache: ensure root: %w", err)
	}
	dbPath := filepath.Join(root, "cache.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("cache: open db: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cache: pragma: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	var casStore *cas.Store
	if opts.CASPath != "" {
		if casStore, err = cas.NewStore(opts.CASPath); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	store := &Store{
		db:   db,
		cas:  casStore,
		ttl:  opts.AutoTTL,
		path: dbPath,
	}
	if err := store.evictExpired(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

// Close releases resources.
func (s *Store) Close() error {
	return s.db.Close()
}

// Put upserts an auto-cache entry and pins any referenced artifacts.
func (s *Store) Put(ctx context.Context, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.LastAccessed = now
	if entry.ExpiresAt.IsZero() {
		entry.ExpiresAt = entry.CreatedAt.Add(s.ttl)
	}
	digestJSON, _ := json.Marshal(entry.Digests)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auto_cache (cache_key, skill_name, skill_version, workspace, result, digests, created_at, expires_at, last_accessed, hit_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(cache_key) DO UPDATE SET
			result = excluded.result,
			digests = excluded.digests,
			workspace = excluded.workspace,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at,
			last_accessed = excluded.last_accessed
	`, entry.CacheKey, entry.SkillName, entry.SkillVersion, entry.Workspace, entry.Result, string(digestJSON),
		entry.CreatedAt.Format(time.RFC3339Nano), entry.ExpiresAt.Format(time.RFC3339Nano), entry.LastAccessed.Format(time.RFC3339Nano))
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
		WHERE cache_key = ?`, key)
	var entry Entry
	var digests string
	var created, expires, last string
	if err := row.Scan(&entry.CacheKey, &entry.SkillName, &entry.SkillVersion, &entry.Workspace, &entry.Result, &digests, &created, &expires, &last, &entry.HitCount); err != nil {
		if errorsIsNoRows(err) {
			return Entry{}, false, nil
		}
		return Entry{}, false, fmt.Errorf("cache: scan: %w", err)
	}
	_ = json.Unmarshal([]byte(digests), &entry.Digests)
	entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	entry.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
	entry.LastAccessed, _ = time.Parse(time.RFC3339Nano, last)

	// refresh access metadata
	_, _ = s.db.ExecContext(ctx, `
		UPDATE auto_cache
		SET last_accessed = ?, hit_count = hit_count + 1
		WHERE cache_key = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), key)
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
			LIMIT ?`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT cache_key, skill_name, skill_version, workspace, result, digests, created_at, expires_at, last_accessed, hit_count
			FROM auto_cache
			WHERE workspace = ?
			ORDER BY created_at DESC
			LIMIT ?`, workspace, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("cache: recent: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var entry Entry
		var digests string
		var created, expires, last string
		if err := rows.Scan(&entry.CacheKey, &entry.SkillName, &entry.SkillVersion, &entry.Workspace, &entry.Result, &digests, &created, &expires, &last, &entry.HitCount); err != nil {
			return nil, fmt.Errorf("cache: scan recent: %w", err)
		}
		_ = json.Unmarshal([]byte(digests), &entry.Digests)
		entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		entry.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
		entry.LastAccessed, _ = time.Parse(time.RFC3339Nano, last)
		entries = append(entries, entry)
	}
	return entries, nil
}

// AnnotateHit mutates the envelope metadata for cache hits.
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
	digests := append([]string{}, extraDigests...)
	sort.Strings(digests)

	h := sha256.New()
	h.Write([]byte(manifest.Metadata.Name))
	h.Write([]byte{0})
	h.Write([]byte(manifest.Metadata.Version))
	h.Write([]byte{0})
	h.Write(args)
	for _, d := range digests {
		h.Write([]byte{0})
		h.Write([]byte(d))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `SELECT cache_key, digests FROM auto_cache WHERE expires_at <= ?`, now)
	if err != nil {
		return fmt.Errorf("cache: select expired: %w", err)
	}
	defer rows.Close()
	type doomed struct {
		key     string
		digests []string
	}
	var expired []doomed
	for rows.Next() {
		var d doomed
		var digests string
		if err := rows.Scan(&d.key, &digests); err != nil {
			return fmt.Errorf("cache: scan expired: %w", err)
		}
		_ = json.Unmarshal([]byte(digests), &d.digests)
		expired = append(expired, d)
	}
	for _, d := range expired {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM auto_cache WHERE cache_key = ?`, d.key); err != nil {
			return fmt.Errorf("cache: delete expired: %w", err)
		}
		s.unpinDigests(ctx, d.digests)
	}
	return nil
}

func (s *Store) pinDigests(ctx context.Context, digests []string) {
	if s.cas == nil {
		return
	}
	for _, d := range digests {
		_ = s.cas.Pin(ctx, d)
	}
}

func (s *Store) unpinDigests(ctx context.Context, digests []string) {
	if s.cas == nil {
		return
	}
	for _, d := range digests {
		_ = s.cas.Unpin(ctx, d)
	}
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
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("cache: migrate: %w", err)
	}
	return nil
}

func errorsIsNoRows(err error) bool {
	return err == sql.ErrNoRows
}

// CollectDigests is a helper that extracts digests from an envelope.
func CollectDigests(result []byte) []string {
	return artifacts.Digests(result)
}
