package obsidianindex

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	obsidiantool "github.com/jkatigb/agentctl/internal/tools/obsidian"
	"github.com/oklog/ulid/v2"
)

type Store interface {
	Close() error
	Rebuild(ctx context.Context, vaultRoot string) (*BuildResult, error)
	SearchNotes(ctx context.Context, query string, limit int) ([]SearchHit, error)
	SearchNotesSemantic(ctx context.Context, query string, provider semantic.EmbeddingProvider, limit int) ([]SearchHit, error)
	EnsureSemanticEmbeddings(ctx context.Context, provider semantic.EmbeddingProvider) (int, error)
	RelatedNotes(ctx context.Context, notePath string, limit int) ([]RelatedHit, error)
	Stats(ctx context.Context) (Stats, error)
	Health(ctx context.Context) (HealthReport, error)
}

type Note struct {
	ID        string
	VaultRoot string
	Path      string
	Title     string
	Type      string
	Project   string
	Status    string
	Trust     string
	UpdatedAt time.Time
	Hash      string
}

type Heading struct {
	NoteID string
	Text   string
	Level  int
	Anchor string
	Line   int
}

type Link struct {
	NoteID  string
	Raw     string
	Target  string
	Alias   string
	Subpath string
	IsEmbed bool
	Line    int
}

type Alias struct {
	NoteID string
	Alias  string
}

type Tag struct {
	NoteID string
	Tag    string
}

type Chunk struct {
	NoteID  string
	Heading string
	Text    string
}

type SearchHit struct {
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Type      string   `json:"type,omitempty"`
	Project   string   `json:"project,omitempty"`
	Status    string   `json:"status,omitempty"`
	Trust     string   `json:"trust,omitempty"`
	Score     int      `json:"score"`
	Snippet   string   `json:"snippet,omitempty"`
	RepoPaths []string `json:"repo_paths,omitempty"`
	Symbols   []string `json:"symbols,omitempty"`
}

type RelatedHit struct {
	Path  string   `json:"path"`
	Title string   `json:"title"`
	Score int      `json:"score"`
	Why   []string `json:"why"`
}

type BuildResult struct {
	VaultRoot               string `json:"vault_root"`
	Notes                   int    `json:"notes"`
	Headings                int    `json:"headings"`
	Links                   int    `json:"links"`
	Aliases                 int    `json:"aliases"`
	Tags                    int    `json:"tags"`
	Chunks                  int    `json:"chunks"`
	RepoPaths               int    `json:"repo_paths"`
	Symbols                 int    `json:"symbols"`
	SemanticEmbeddings      int    `json:"semantic_embeddings"`
	ChunkSemanticEmbeddings int    `json:"chunk_semantic_embeddings"`
}

type Stats struct {
	Notes                   int `json:"notes"`
	Headings                int `json:"headings"`
	Links                   int `json:"links"`
	Aliases                 int `json:"aliases"`
	Tags                    int `json:"tags"`
	Chunks                  int `json:"chunks"`
	RepoPaths               int `json:"repo_paths"`
	Symbols                 int `json:"symbols"`
	SemanticEmbeddings      int `json:"semantic_embeddings"`
	ChunkSemanticEmbeddings int `json:"chunk_semantic_embeddings"`
}

type HealthReport struct {
	Orphans       []string `json:"orphans,omitempty"`
	DeadEnds      []string `json:"dead_ends,omitempty"`
	Unresolved    []string `json:"unresolved,omitempty"`
	OversizedMOCs []string `json:"oversized_mocs,omitempty"`
	StaleNotes    []string `json:"stale_notes,omitempty"`
}

type sqlStore struct {
	db      *sql.DB
	close   func() error
	dbPath  string
	vaultID string
}

func Open(ctx context.Context, storageRoot, vaultRoot string) (Store, error) {
	vaultRoot = filepath.Clean(strings.TrimSpace(vaultRoot))
	if vaultRoot == "" {
		return nil, fmt.Errorf("obsidianindex: vault root required")
	}
	hash := sha256.Sum256([]byte(vaultRoot))
	file := fmt.Sprintf("obsidianindex-%s.db", hex.EncodeToString(hash[:8]))
	db, closeFn, err := dbutil.OpenStoreDB(ctx, storageRoot, "OBSIDIANINDEX", file, migrate)
	if err != nil {
		return nil, fmt.Errorf("obsidianindex: open db: %w", err)
	}
	return &sqlStore{
		db:      db,
		close:   closeFn,
		dbPath:  filepath.Join(storageRoot, file),
		vaultID: hex.EncodeToString(hash[:8]),
	}, nil
}

func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS obsidian_notes (
	id TEXT PRIMARY KEY,
	vault_root TEXT NOT NULL,
	path TEXT NOT NULL UNIQUE,
	title TEXT NOT NULL,
	search_text TEXT NOT NULL DEFAULT '',
	type TEXT,
	project TEXT,
	status TEXT,
	trust TEXT,
	updated_at TEXT NOT NULL,
	hash TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_obsidian_notes_title ON obsidian_notes(title);
CREATE INDEX IF NOT EXISTS idx_obsidian_notes_type ON obsidian_notes(type);
CREATE INDEX IF NOT EXISTS idx_obsidian_notes_project ON obsidian_notes(project);

CREATE TABLE IF NOT EXISTS obsidian_headings (
	note_id TEXT NOT NULL,
	heading TEXT NOT NULL,
	level INTEGER NOT NULL,
	anchor TEXT NOT NULL,
	line INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_obsidian_headings_note ON obsidian_headings(note_id);

CREATE TABLE IF NOT EXISTS obsidian_links (
	note_id TEXT NOT NULL,
	raw TEXT NOT NULL,
	target TEXT NOT NULL,
	alias TEXT,
	subpath TEXT,
	is_embed INTEGER NOT NULL DEFAULT 0,
	line INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_obsidian_links_note ON obsidian_links(note_id);
CREATE INDEX IF NOT EXISTS idx_obsidian_links_target ON obsidian_links(target);

CREATE TABLE IF NOT EXISTS obsidian_aliases (
	note_id TEXT NOT NULL,
	alias TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_obsidian_aliases_note ON obsidian_aliases(note_id);
CREATE INDEX IF NOT EXISTS idx_obsidian_aliases_alias ON obsidian_aliases(alias);

CREATE TABLE IF NOT EXISTS obsidian_tags (
	note_id TEXT NOT NULL,
	tag TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_obsidian_tags_note ON obsidian_tags(note_id);
CREATE INDEX IF NOT EXISTS idx_obsidian_tags_tag ON obsidian_tags(tag);

CREATE TABLE IF NOT EXISTS obsidian_chunks (
	note_id TEXT NOT NULL,
	heading TEXT,
	text TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_obsidian_chunks_note ON obsidian_chunks(note_id);

CREATE TABLE IF NOT EXISTS obsidian_repo_paths (
	note_id TEXT NOT NULL,
	path TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_obsidian_repo_paths_note ON obsidian_repo_paths(note_id);
CREATE INDEX IF NOT EXISTS idx_obsidian_repo_paths_path ON obsidian_repo_paths(path);

CREATE TABLE IF NOT EXISTS obsidian_repo_symbols (
	note_id TEXT NOT NULL,
	symbol TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_obsidian_repo_symbols_note ON obsidian_repo_symbols(note_id);
CREATE INDEX IF NOT EXISTS idx_obsidian_repo_symbols_symbol ON obsidian_repo_symbols(symbol);

CREATE TABLE IF NOT EXISTS obsidian_note_embeddings (
	path TEXT NOT NULL,
	model TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	embedding_json TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY(path, model)
);
CREATE INDEX IF NOT EXISTS idx_obsidian_note_embeddings_path ON obsidian_note_embeddings(path);
CREATE INDEX IF NOT EXISTS idx_obsidian_note_embeddings_model ON obsidian_note_embeddings(model);

CREATE TABLE IF NOT EXISTS obsidian_chunk_embeddings (
	path TEXT NOT NULL,
	chunk_index INTEGER NOT NULL,
	heading TEXT,
	text TEXT NOT NULL,
	model TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	embedding_json TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY(path, chunk_index, model)
);
CREATE INDEX IF NOT EXISTS idx_obsidian_chunk_embeddings_path ON obsidian_chunk_embeddings(path);
CREATE INDEX IF NOT EXISTS idx_obsidian_chunk_embeddings_model ON obsidian_chunk_embeddings(model);

CREATE TABLE IF NOT EXISTS obsidian_embedding_metadata (
	model TEXT PRIMARY KEY,
	dimensions INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_obsidian_embedding_metadata_model ON obsidian_embedding_metadata(model);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("obsidianindex: migrate: %w", err)
	}
	return nil
}

// MigrateSchema exposes the Obsidian index DDL for db migrate.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db)
}

func (s *sqlStore) Rebuild(ctx context.Context, vaultRoot string) (*BuildResult, error) {
	vaultRoot = filepath.Clean(vaultRoot)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("obsidianindex: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, table := range []string{"obsidian_repo_symbols", "obsidian_repo_paths", "obsidian_chunks", "obsidian_tags", "obsidian_aliases", "obsidian_links", "obsidian_headings", "obsidian_notes"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return nil, fmt.Errorf("obsidianindex: clear %s: %w", table, err)
		}
	}

	result := &BuildResult{VaultRoot: vaultRoot}
	err = filepath.WalkDir(vaultRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".obsidian" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(vaultRoot, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(body)
		parsed := obsidiantool.ParseNoteLinks(rel, body)
		fm := parseFrontmatter(body)
		title := parsed.Title
		if strings.TrimSpace(fm.Values["title"]) != "" {
			title = strings.TrimSpace(fm.Values["title"])
		}
		tags := uniqueStrings(append(fm.Tags, extractInlineTags(string(body))...))
		repoPaths := uniqueStrings(fm.Paths)
		repoSymbols := uniqueStrings(fm.Symbols)
		noteID := ulid.Make().String()
		now := timeutil.NowUTC()
		if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_notes (id, vault_root, path, title, search_text, type, project, status, trust, updated_at, hash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, noteID, vaultRoot, filepath.ToSlash(rel), title, buildSearchText(parsed, tags), nullable(fm.Values["type"]), nullable(fm.Values["project"]), nullable(fm.Values["status"]), nullable(fm.Values["trust"]), timeutil.FormatRFC3339Nano(now), "sha256:"+hex.EncodeToString(hash[:])); err != nil {
			return fmt.Errorf("insert note %s: %w", rel, err)
		}
		result.Notes++
		for _, heading := range parsed.Headings {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_headings (note_id, heading, level, anchor, line)
VALUES (?, ?, ?, ?, ?)
`, noteID, heading.Text, heading.Level, heading.Anchor, heading.Line); err != nil {
				return err
			}
			result.Headings++
		}
		for _, alias := range parsed.Aliases {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_aliases (note_id, alias)
VALUES (?, ?)
`, noteID, alias); err != nil {
				return err
			}
			result.Aliases++
		}
		for _, tag := range tags {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_tags (note_id, tag)
VALUES (?, ?)
`, noteID, tag); err != nil {
				return err
			}
			result.Tags++
		}
		for _, link := range parsed.Outgoing {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_links (note_id, raw, target, alias, subpath, is_embed, line)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, noteID, link.Raw, strings.ToLower(strings.TrimSpace(link.Target)), nullable(link.Alias), nullable(link.Subpath), boolToInt(link.IsEmbed), link.Line); err != nil {
				return err
			}
			result.Links++
		}
		for _, chunk := range buildChunks(parsed, string(body)) {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_chunks (note_id, heading, text)
VALUES (?, ?, ?)
`, noteID, nullable(chunk.Heading), chunk.Text); err != nil {
				return err
			}
			result.Chunks++
		}
		for _, p := range repoPaths {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_repo_paths (note_id, path)
VALUES (?, ?)
`, noteID, p); err != nil {
				return err
			}
			result.RepoPaths++
		}
		for _, sym := range repoSymbols {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_repo_symbols (note_id, symbol)
VALUES (?, ?)
`, noteID, sym); err != nil {
				return err
			}
			result.Symbols++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM obsidian_note_embeddings WHERE path NOT IN (SELECT path FROM obsidian_notes)`); err != nil {
		return nil, fmt.Errorf("obsidianindex: prune embeddings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM obsidian_chunk_embeddings`); err != nil {
		return nil, fmt.Errorf("obsidianindex: clear chunk embeddings: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("obsidianindex: commit: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_note_embeddings`).Scan(&result.SemanticEmbeddings); err != nil {
		return nil, fmt.Errorf("obsidianindex: count embeddings: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_chunk_embeddings`).Scan(&result.ChunkSemanticEmbeddings); err != nil {
		return nil, fmt.Errorf("obsidianindex: count chunk embeddings: %w", err)
	}
	return result, nil
}

func (s *sqlStore) SearchNotes(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("obsidianindex: query required")
	}
	if limit <= 0 {
		limit = 20
	}
	q := "%" + strings.ToLower(query) + "%"
	args := []any{
		q, q, q, q, q, q, q, q,
		q,
		q, q, q, q, q, q, q, q,
		limit,
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT path, title, COALESCE(type,''), COALESCE(project,''), COALESCE(status,''), COALESCE(trust,''),
       (CASE WHEN lower(title) LIKE ? THEN 5 ELSE 0 END) +
       (CASE WHEN lower(path) LIKE ? THEN 4 ELSE 0 END) +
       (CASE WHEN lower(search_text) LIKE ? THEN 2 ELSE 0 END) +
       (CASE WHEN EXISTS (SELECT 1 FROM obsidian_chunks c WHERE c.note_id = obsidian_notes.id AND lower(c.text) LIKE ?) THEN 3 ELSE 0 END) +
       (CASE WHEN EXISTS (SELECT 1 FROM obsidian_repo_paths rp WHERE rp.note_id = obsidian_notes.id AND lower(rp.path) LIKE ?) THEN 4 ELSE 0 END) +
       (CASE WHEN EXISTS (SELECT 1 FROM obsidian_repo_symbols rs WHERE rs.note_id = obsidian_notes.id AND lower(rs.symbol) LIKE ?) THEN 4 ELSE 0 END) +
       (CASE WHEN EXISTS (SELECT 1 FROM obsidian_tags t JOIN obsidian_notes n3 ON t.note_id = n3.id WHERE n3.path = obsidian_notes.path AND lower(t.tag) LIKE ?) THEN 3 ELSE 0 END) +
       (CASE WHEN EXISTS (SELECT 1 FROM obsidian_aliases a JOIN obsidian_notes n2 ON a.note_id = n2.id WHERE n2.path = obsidian_notes.path AND lower(a.alias) LIKE ?) THEN 3 ELSE 0 END) AS score,
       COALESCE((SELECT c.text FROM obsidian_chunks c WHERE c.note_id = obsidian_notes.id AND lower(c.text) LIKE ? ORDER BY length(c.text) ASC LIMIT 1), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT rp.path) FROM obsidian_repo_paths rp WHERE rp.note_id = obsidian_notes.id), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT rs.symbol) FROM obsidian_repo_symbols rs WHERE rs.note_id = obsidian_notes.id), '')
FROM obsidian_notes
WHERE lower(title) LIKE ? OR lower(path) LIKE ? OR lower(search_text) LIKE ? OR EXISTS (
  SELECT 1 FROM obsidian_chunks c WHERE c.note_id = obsidian_notes.id AND lower(c.text) LIKE ?
) OR EXISTS (
  SELECT 1 FROM obsidian_repo_paths rp WHERE rp.note_id = obsidian_notes.id AND lower(rp.path) LIKE ?
) OR EXISTS (
  SELECT 1 FROM obsidian_repo_symbols rs WHERE rs.note_id = obsidian_notes.id AND lower(rs.symbol) LIKE ?
) OR EXISTS (
  SELECT 1 FROM obsidian_tags t JOIN obsidian_notes n3 ON t.note_id = n3.id WHERE n3.path = obsidian_notes.path AND lower(t.tag) LIKE ?
) OR EXISTS (
  SELECT 1 FROM obsidian_aliases a JOIN obsidian_notes n2 ON a.note_id = n2.id WHERE n2.path = obsidian_notes.path AND lower(a.alias) LIKE ?
)
ORDER BY score DESC, path ASC
LIMIT ?
`, args...)
	if err != nil {
		return nil, fmt.Errorf("obsidianindex: search notes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []SearchHit
	for rows.Next() {
		var hit SearchHit
		var repoPathsCSV, symbolsCSV string
		if err := rows.Scan(&hit.Path, &hit.Title, &hit.Type, &hit.Project, &hit.Status, &hit.Trust, &hit.Score, &hit.Snippet, &repoPathsCSV, &symbolsCSV); err != nil {
			return nil, fmt.Errorf("obsidianindex: scan search hit: %w", err)
		}
		hit.Snippet = compactSnippet(hit.Snippet)
		hit.RepoPaths = splitCSV(repoPathsCSV)
		hit.Symbols = splitCSV(symbolsCSV)
		out = append(out, hit)
	}
	return out, rows.Err()
}

func (s *sqlStore) RelatedNotes(ctx context.Context, notePath string, limit int) ([]RelatedHit, error) {
	notePath = filepath.ToSlash(strings.TrimSpace(notePath))
	if notePath == "" {
		return nil, fmt.Errorf("obsidianindex: note path required")
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
WITH seed AS (
  SELECT id, path, title FROM obsidian_notes WHERE path = ?
),
direct AS (
  SELECT n.path, n.title, 3 AS score, 'direct_link' AS why
  FROM obsidian_links l
  JOIN seed s ON s.id = l.note_id
  JOIN obsidian_notes n ON lower(n.title) = lower(l.target) OR lower(n.path) = lower(l.target) OR lower(n.path) = lower(l.target || '.md')
),
backlinks AS (
  SELECT n.path, n.title, 4 AS score, 'backlink' AS why
  FROM obsidian_links l
  JOIN obsidian_notes n ON n.id = l.note_id
  JOIN seed s ON lower(l.target) = lower(s.title) OR lower(l.target) = lower(s.path) OR lower(l.target) = lower(replace(s.path, '.md', ''))
),
related AS (
  SELECT * FROM direct
  UNION ALL
  SELECT * FROM backlinks
)
SELECT path, title, SUM(score) AS score, GROUP_CONCAT(DISTINCT why)
FROM related
GROUP BY path, title
ORDER BY score DESC, path ASC
LIMIT ?
`, notePath, limit)
	if err != nil {
		return nil, fmt.Errorf("obsidianindex: related notes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RelatedHit
	for rows.Next() {
		var hit RelatedHit
		var whyCSV string
		if err := rows.Scan(&hit.Path, &hit.Title, &hit.Score, &whyCSV); err != nil {
			return nil, fmt.Errorf("obsidianindex: scan related hit: %w", err)
		}
		if whyCSV != "" {
			hit.Why = strings.Split(whyCSV, ",")
			sort.Strings(hit.Why)
		}
		out = append(out, hit)
	}
	return out, rows.Err()
}

func (s *sqlStore) EnsureSemanticEmbeddings(ctx context.Context, provider semantic.EmbeddingProvider) (int, error) {
	if provider == nil {
		return 0, fmt.Errorf("obsidianindex: embedding provider required")
	}
	if err := s.validateEmbeddingMetadata(ctx, provider); err != nil {
		return 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT n.path, n.title, n.search_text, n.hash
FROM obsidian_notes n
LEFT JOIN obsidian_note_embeddings e
  ON e.path = n.path AND e.model = ?
WHERE e.path IS NULL OR e.content_hash != n.hash
ORDER BY n.path ASC
`, provider.Model())
	if err != nil {
		return 0, fmt.Errorf("obsidianindex: query semantic embedding candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type candidate struct {
		Path string
		Text string
		Hash string
	}
	var candidates []candidate
	for rows.Next() {
		var path, title, searchText, hash string
		if err := rows.Scan(&path, &title, &searchText, &hash); err != nil {
			return 0, fmt.Errorf("obsidianindex: scan semantic candidate: %w", err)
		}
		candidates = append(candidates, candidate{
			Path: path,
			Text: strings.TrimSpace(title + "\n\n" + searchText),
			Hash: hash,
		})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	texts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		texts = append(texts, candidate.Text)
	}
	embeddings, err := provider.EmbedBatch(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("obsidianindex: embed notes: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("obsidianindex: begin semantic tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := timeutil.NowUTC()
	if err := upsertObsidianEmbeddingMetadataTx(ctx, tx, provider.Model(), provider.Dimensions(), now); err != nil {
		return 0, err
	}
	for i, candidate := range candidates {
		body, err := json.Marshal(embeddings[i])
		if err != nil {
			return 0, fmt.Errorf("obsidianindex: marshal embedding: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_note_embeddings (path, model, content_hash, embedding_json, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(path, model) DO UPDATE SET
	content_hash = excluded.content_hash,
	embedding_json = excluded.embedding_json,
	updated_at = excluded.updated_at
`, candidate.Path, provider.Model(), candidate.Hash, string(body), timeutil.FormatRFC3339Nano(now)); err != nil {
			return 0, fmt.Errorf("obsidianindex: upsert embedding: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("obsidianindex: commit semantic tx: %w", err)
	}
	return len(candidates), nil
}

func (s *sqlStore) SearchNotesSemantic(ctx context.Context, query string, provider semantic.EmbeddingProvider, limit int) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("obsidianindex: query required")
	}
	if provider == nil {
		return nil, fmt.Errorf("obsidianindex: embedding provider required")
	}
	if limit <= 0 {
		limit = 20
	}
	if err := s.validateEmbeddingMetadata(ctx, provider); err != nil {
		return nil, err
	}
	if _, err := s.EnsureSemanticEmbeddings(ctx, provider); err != nil {
		return nil, err
	}
	if _, err := s.ensureChunkSemanticEmbeddings(ctx, provider); err != nil {
		return nil, err
	}
	var queryEmbedding []float32
	if qp, ok := provider.(semantic.QueryEmbeddingProvider); ok {
		queryEmbedding, _ = qp.EmbedQuery(ctx, query)
	}
	if len(queryEmbedding) == 0 {
		var err error
		queryEmbedding, err = provider.Embed(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("obsidianindex: embed query: %w", err)
		}
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT n.path, n.title, COALESCE(n.type,''), COALESCE(n.project,''), COALESCE(n.status,''), COALESCE(n.trust,''),
       COALESCE((SELECT c.text FROM obsidian_chunks c WHERE c.note_id = n.id ORDER BY length(c.text) ASC LIMIT 1), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT rp.path) FROM obsidian_repo_paths rp WHERE rp.note_id = n.id), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT rs.symbol) FROM obsidian_repo_symbols rs WHERE rs.note_id = n.id), ''),
       e.embedding_json
FROM obsidian_notes n
JOIN obsidian_note_embeddings e ON e.path = n.path AND e.model = ?
ORDER BY n.path ASC
`, provider.Model())
	if err != nil {
		return nil, fmt.Errorf("obsidianindex: semantic note query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	hitByPath := map[string]SearchHit{}
	for rows.Next() {
		var hit SearchHit
		var repoPathsCSV, symbolsCSV, embeddingJSON string
		if err := rows.Scan(&hit.Path, &hit.Title, &hit.Type, &hit.Project, &hit.Status, &hit.Trust, &hit.Snippet, &repoPathsCSV, &symbolsCSV, &embeddingJSON); err != nil {
			return nil, fmt.Errorf("obsidianindex: scan semantic hit: %w", err)
		}
		var embedding []float32
		if err := json.Unmarshal([]byte(embeddingJSON), &embedding); err != nil {
			return nil, fmt.Errorf("obsidianindex: decode semantic embedding: %w", err)
		}
		hit.Score = int(cosineSimilarity(queryEmbedding, embedding) * 1000)
		hit.Snippet = compactSnippet(hit.Snippet)
		hit.RepoPaths = splitCSV(repoPathsCSV)
		hit.Symbols = splitCSV(symbolsCSV)
		hitByPath[hit.Path] = hit
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	chunkRows, err := s.db.QueryContext(ctx, `
SELECT path, chunk_index, heading, text, embedding_json
FROM obsidian_chunk_embeddings
WHERE model = ?
ORDER BY path ASC, chunk_index ASC
`, provider.Model())
	if err != nil {
		return nil, fmt.Errorf("obsidianindex: semantic chunk query: %w", err)
	}
	defer func() { _ = chunkRows.Close() }()
	for chunkRows.Next() {
		var path string
		var chunkIndex int
		var heading sql.NullString
		var text, embeddingJSON string
		if err := chunkRows.Scan(&path, &chunkIndex, &heading, &text, &embeddingJSON); err != nil {
			return nil, fmt.Errorf("obsidianindex: scan semantic chunk hit: %w", err)
		}
		var embedding []float32
		if err := json.Unmarshal([]byte(embeddingJSON), &embedding); err != nil {
			return nil, fmt.Errorf("obsidianindex: decode semantic chunk embedding: %w", err)
		}
		score := int(cosineSimilarity(queryEmbedding, embedding) * 1000)
		hit := hitByPath[path]
		if score > hit.Score {
			hit.Score = score
			hit.Snippet = compactSnippet(text)
			hitByPath[path] = hit
		}
	}
	if err := chunkRows.Err(); err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, len(hitByPath))
	for _, hit := range hitByPath {
		hits = append(hits, hit)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Path < hits[j].Path
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (s *sqlStore) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_notes`).Scan(&stats.Notes); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_headings`).Scan(&stats.Headings); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_links`).Scan(&stats.Links); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_aliases`).Scan(&stats.Aliases); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_tags`).Scan(&stats.Tags); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_chunks`).Scan(&stats.Chunks); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_repo_paths`).Scan(&stats.RepoPaths); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_repo_symbols`).Scan(&stats.Symbols); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_note_embeddings`).Scan(&stats.SemanticEmbeddings); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_chunk_embeddings`).Scan(&stats.ChunkSemanticEmbeddings); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

func (s *sqlStore) Health(ctx context.Context) (HealthReport, error) {
	report := HealthReport{}

	orphansRows, err := s.db.QueryContext(ctx, `
SELECT n.path
FROM obsidian_notes n
WHERE NOT EXISTS (
  SELECT 1 FROM obsidian_links l
  JOIN obsidian_notes src ON src.id = l.note_id
  WHERE lower(l.target) = lower(n.title)
     OR lower(l.target) = lower(n.path)
     OR lower(l.target) = lower(replace(n.path, '.md', ''))
)
ORDER BY n.path ASC
`)
	if err != nil {
		return report, fmt.Errorf("obsidianindex: query orphans: %w", err)
	}
	defer func() { _ = orphansRows.Close() }()
	for orphansRows.Next() {
		var path string
		if err := orphansRows.Scan(&path); err != nil {
			return report, err
		}
		report.Orphans = append(report.Orphans, path)
	}

	deadRows, err := s.db.QueryContext(ctx, `
SELECT n.path
FROM obsidian_notes n
WHERE NOT EXISTS (
  SELECT 1 FROM obsidian_links l WHERE l.note_id = n.id
)
ORDER BY n.path ASC
`)
	if err != nil {
		return report, fmt.Errorf("obsidianindex: query dead ends: %w", err)
	}
	defer func() { _ = deadRows.Close() }()
	for deadRows.Next() {
		var path string
		if err := deadRows.Scan(&path); err != nil {
			return report, err
		}
		report.DeadEnds = append(report.DeadEnds, path)
	}

	unresolvedRows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT src.path, l.target
FROM obsidian_links l
JOIN obsidian_notes src ON src.id = l.note_id
LEFT JOIN obsidian_notes dst
  ON lower(l.target) = lower(dst.title)
  OR lower(l.target) = lower(dst.path)
  OR lower(l.target) = lower(replace(dst.path, '.md', ''))
WHERE dst.id IS NULL
ORDER BY src.path ASC, l.target ASC
`)
	if err != nil {
		return report, fmt.Errorf("obsidianindex: query unresolved: %w", err)
	}
	defer func() { _ = unresolvedRows.Close() }()
	for unresolvedRows.Next() {
		var path, target string
		if err := unresolvedRows.Scan(&path, &target); err != nil {
			return report, err
		}
		report.Unresolved = append(report.Unresolved, path+" -> "+target)
	}

	mocRows, err := s.db.QueryContext(ctx, `
SELECT n.path
FROM obsidian_notes n
JOIN obsidian_links l ON l.note_id = n.id
WHERE lower(COALESCE(n.type, '')) = 'map'
GROUP BY n.path
HAVING COUNT(*) > 20
ORDER BY n.path ASC
`)
	if err != nil {
		return report, fmt.Errorf("obsidianindex: query oversized mocs: %w", err)
	}
	defer func() { _ = mocRows.Close() }()
	for mocRows.Next() {
		var path string
		if err := mocRows.Scan(&path); err != nil {
			return report, err
		}
		report.OversizedMOCs = append(report.OversizedMOCs, path)
	}

	staleCutoff := timeutil.NowUTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	staleRows, err := s.db.QueryContext(ctx, `
SELECT path
FROM obsidian_notes
WHERE updated_at < ?
  AND lower(COALESCE(status, '')) != 'draft'
ORDER BY path ASC
`, staleCutoff)
	if err != nil {
		return report, fmt.Errorf("obsidianindex: query stale notes: %w", err)
	}
	defer func() { _ = staleRows.Close() }()
	for staleRows.Next() {
		var path string
		if err := staleRows.Scan(&path); err != nil {
			return report, err
		}
		report.StaleNotes = append(report.StaleNotes, path)
	}

	return report, nil
}

type frontmatter struct {
	Values  map[string]string
	Tags    []string
	Paths   []string
	Symbols []string
}

func parseFrontmatter(body []byte) frontmatter {
	text := string(body)
	if !strings.HasPrefix(text, "---\n") {
		return frontmatter{}
	}
	parts := strings.SplitN(text, "\n---\n", 2)
	if len(parts) != 2 {
		return frontmatter{}
	}
	out := frontmatter{Values: map[string]string{}}
	currentList := ""
	for _, line := range strings.Split(parts[0], "\n") {
		rawLine := strings.TrimRight(line, "\r")
		line = strings.TrimSpace(rawLine)
		if line == "" || line == "---" {
			continue
		}
		if currentList != "" && strings.HasPrefix(line, "-") {
			item := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "-")), `"'`)
			appendFrontmatterList(&out, currentList, item)
			continue
		}
		if currentList != "" && !strings.HasPrefix(line, "-") {
			currentList = ""
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		value := strings.TrimSpace(v)
		out.Values[key] = strings.Trim(value, `"'`)
		if !isFrontmatterListKey(key) {
			continue
		}
		if value == "" {
			currentList = key
			continue
		}
		currentList = ""
		for _, item := range parseFrontmatterInlineList(value) {
			appendFrontmatterList(&out, key, item)
		}
	}
	return out
}

func isFrontmatterListKey(key string) bool {
	switch key {
	case "tags", "paths", "symbols":
		return true
	default:
		return false
	}
}

func parseFrontmatterInlineList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || value == "[]" {
		return nil
	}
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.Trim(strings.TrimSpace(part), `"'`)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func appendFrontmatterList(out *frontmatter, key, item string) {
	item = strings.TrimSpace(item)
	if item == "" {
		return
	}
	switch key {
	case "tags":
		out.Tags = append(out.Tags, normalizeTag(item))
	case "paths":
		out.Paths = append(out.Paths, filepath.ToSlash(item))
	case "symbols":
		out.Symbols = append(out.Symbols, item)
	}
}

func nullable(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func buildSearchText(parsed obsidiantool.LinkParseResult, tags []string) string {
	parts := []string{parsed.Title}
	parts = append(parts, parsed.Aliases...)
	parts = append(parts, tags...)
	for _, heading := range parsed.Headings {
		parts = append(parts, heading.Text)
	}
	for _, link := range parsed.Outgoing {
		parts = append(parts, link.Target, link.Alias)
	}
	return strings.Join(uniqueStrings(parts), " ")
}

func compactSnippet(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if len(text) > 180 {
		return text[:177] + "..."
	}
	return text
}

type chunk struct {
	Heading string
	Text    string
}

func buildChunks(parsed obsidiantool.LinkParseResult, body string) []chunk {
	body = stripFrontmatter(body)
	if len(parsed.Headings) == 0 {
		trimmed := strings.TrimSpace(body)
		if trimmed == "" {
			return nil
		}
		return []chunk{{Text: trimmed}}
	}
	lines := strings.Split(body, "\n")
	type headingLine struct {
		text string
		line int
	}
	headings := make([]headingLine, 0, len(parsed.Headings))
	for _, h := range parsed.Headings {
		headings = append(headings, headingLine{text: h.Text, line: h.Line})
	}
	var chunks []chunk
	for i, h := range headings {
		start := h.line
		end := len(lines)
		if i+1 < len(headings) {
			end = headings[i+1].line - 1
		}
		if start < 0 {
			start = 0
		}
		if end > len(lines) {
			end = len(lines)
		}
		text := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if text == "" {
			continue
		}
		chunks = append(chunks, chunk{Heading: h.text, Text: text})
	}
	return chunks
}

func stripFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	parts := strings.SplitN(body, "\n---\n", 2)
	if len(parts) != 2 {
		return body
	}
	return parts[1]
}

func extractInlineTags(body string) []string {
	body = stripFrontmatter(body)
	fields := strings.Fields(body)
	var tags []string
	for _, field := range fields {
		if !strings.HasPrefix(field, "#") || len(field) < 2 {
			continue
		}
		tag := normalizeTag(strings.TrimPrefix(field, "#"))
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return uniqueStrings(tags)
}

func normalizeTag(tag string) string {
	tag = strings.TrimSpace(strings.TrimPrefix(tag, "#"))
	return strings.ToLower(tag)
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return uniqueStrings(strings.Split(value, ","))
}

func (s *sqlStore) ensureChunkSemanticEmbeddings(ctx context.Context, provider semantic.EmbeddingProvider) (int, error) {
	if provider == nil {
		return 0, fmt.Errorf("obsidianindex: embedding provider required")
	}
	if err := s.validateEmbeddingMetadata(ctx, provider); err != nil {
		return 0, err
	}
	type chunkCandidate struct {
		Path       string
		ChunkIndex int
		Heading    string
		Text       string
		Hash       string
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT n.path, c.heading, c.text
FROM obsidian_chunks c
JOIN obsidian_notes n ON n.id = c.note_id
ORDER BY n.path ASC, c.rowid ASC
`)
	if err != nil {
		return 0, fmt.Errorf("obsidianindex: query chunk candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var candidates []chunkCandidate
	currentPath := ""
	currentIndex := -1
	for rows.Next() {
		var path string
		var heading sql.NullString
		var text string
		if err := rows.Scan(&path, &heading, &text); err != nil {
			return 0, fmt.Errorf("obsidianindex: scan chunk candidate: %w", err)
		}
		if path != currentPath {
			currentPath = path
			currentIndex = 0
		} else {
			currentIndex++
		}
		hash := sha256.Sum256([]byte(text))
		candidates = append(candidates, chunkCandidate{
			Path:       path,
			ChunkIndex: currentIndex,
			Heading:    heading.String,
			Text:       text,
			Hash:       "sha256:" + hex.EncodeToString(hash[:]),
		})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("obsidianindex: begin chunk semantic tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM obsidian_chunk_embeddings WHERE model = ?`, provider.Model()); err != nil {
		return 0, fmt.Errorf("obsidianindex: clear chunk embeddings: %w", err)
	}
	texts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		texts = append(texts, candidate.Text)
	}
	embeddings, err := provider.EmbedBatch(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("obsidianindex: embed chunks: %w", err)
	}
	now := timeutil.NowUTC()
	if err := upsertObsidianEmbeddingMetadataTx(ctx, tx, provider.Model(), provider.Dimensions(), now); err != nil {
		return 0, err
	}
	for i, candidate := range candidates {
		body, err := json.Marshal(embeddings[i])
		if err != nil {
			return 0, fmt.Errorf("obsidianindex: marshal chunk embedding: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_chunk_embeddings (path, chunk_index, heading, text, model, content_hash, embedding_json, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, candidate.Path, candidate.ChunkIndex, nullable(candidate.Heading), candidate.Text, provider.Model(), candidate.Hash, string(body), timeutil.FormatRFC3339Nano(now)); err != nil {
			return 0, fmt.Errorf("obsidianindex: insert chunk embedding: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("obsidianindex: commit chunk semantic tx: %w", err)
	}
	return len(candidates), nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		normA += af * af
		normB += bf * bf
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func (s *sqlStore) validateEmbeddingMetadata(ctx context.Context, provider semantic.EmbeddingProvider) error {
	if provider == nil {
		return nil
	}
	var dims int
	err := s.db.QueryRowContext(ctx, `
SELECT dimensions
FROM obsidian_embedding_metadata
WHERE model = ?
`, provider.Model()).Scan(&dims)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("obsidianindex: embedding metadata lookup: %w", err)
	}
	if dims != provider.Dimensions() {
		return fmt.Errorf("obsidianindex: embedding dimension mismatch for model %q: stored=%d, provider=%d; run `agentctl obsidian index build --vault-path <vault-path>` to rebuild vault semantic embeddings", provider.Model(), dims, provider.Dimensions())
	}
	return nil
}

func upsertObsidianEmbeddingMetadataTx(ctx context.Context, tx *sql.Tx, model string, dimensions int, now time.Time) error {
	if strings.TrimSpace(model) == "" || dimensions <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_embedding_metadata (model, dimensions, created_at, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(model) DO UPDATE SET
	updated_at = excluded.updated_at
`, model, dimensions, timeutil.FormatRFC3339Nano(now), timeutil.FormatRFC3339Nano(now))
	if err != nil {
		return fmt.Errorf("obsidianindex: upsert embedding metadata: %w", err)
	}
	return nil
}
