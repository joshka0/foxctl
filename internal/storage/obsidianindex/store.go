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

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	obsidiantool "github.com/joshka0/foxctl/internal/tooling/tools/obsidian"
	"github.com/oklog/ulid/v2"
)

const (
	obsidianBusyRetryWindow = 15 * time.Second
	obsidianBusyRetryStep   = 50 * time.Millisecond
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
	Path              string              `json:"path"`
	Title             string              `json:"title"`
	Type              string              `json:"type,omitempty"`
	Project           string              `json:"project,omitempty"`
	Status            string              `json:"status,omitempty"`
	Trust             string              `json:"trust,omitempty"`
	Score             int                 `json:"score"`
	Snippet           string              `json:"snippet,omitempty"`
	PrimaryAnchorPath string              `json:"primary_anchor_path,omitempty"`
	RepoPaths         []string            `json:"repo_paths,omitempty"`
	AnchorPaths       []string            `json:"anchor_paths,omitempty"`
	AnchorRoles       map[string][]string `json:"anchor_roles,omitempty"`
	Symbols           []string            `json:"symbols,omitempty"`
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
	primary_anchor_path TEXT,
	anchor_roles_json TEXT NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS obsidian_anchor_paths (
	note_id TEXT NOT NULL,
	path TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_obsidian_anchor_paths_note ON obsidian_anchor_paths(note_id);
CREATE INDEX IF NOT EXISTS idx_obsidian_anchor_paths_path ON obsidian_anchor_paths(path);

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
	_, _ = db.ExecContext(ctx, `ALTER TABLE obsidian_notes ADD COLUMN primary_anchor_path TEXT`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE obsidian_notes ADD COLUMN anchor_roles_json TEXT NOT NULL DEFAULT ''`)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_obsidian_notes_primary_anchor_path ON obsidian_notes(primary_anchor_path)`)
	return nil
}

// MigrateSchema exposes the Obsidian index DDL for db migrate.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db)
}

func (s *sqlStore) Rebuild(ctx context.Context, vaultRoot string) (*BuildResult, error) {
	vaultRoot = filepath.Clean(vaultRoot)
	var tx *sql.Tx
	err := retryObsidianBusy(ctx, func() error {
		var beginErr error
		tx, beginErr = s.db.BeginTx(ctx, nil)
		if beginErr != nil {
			return fmt.Errorf("obsidianindex: begin tx: %w", beginErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	for _, table := range []string{"obsidian_repo_symbols", "obsidian_anchor_paths", "obsidian_repo_paths", "obsidian_chunks", "obsidian_tags", "obsidian_aliases", "obsidian_links", "obsidian_headings", "obsidian_notes"} {
		tableName := table
		if err := retryObsidianBusy(ctx, func() error {
			if _, execErr := tx.ExecContext(ctx, "DELETE FROM "+tableName); execErr != nil {
				return fmt.Errorf("obsidianindex: clear %s: %w", tableName, execErr)
			}
			return nil
		}); err != nil {
			return nil, err
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
		anchorPaths := uniqueStrings(fm.AnchorPaths)
		primaryAnchorPath := strings.TrimSpace(fm.Values["primary_anchor_path"])
		if primaryAnchorPath == "" && len(anchorPaths) > 0 {
			primaryAnchorPath = anchorPaths[0]
		}
		anchorRoles := normalizeAnchorRoles(fm.AnchorRoles)
		anchorRolesJSON := ""
		if len(anchorRoles) > 0 {
			if body, err := json.Marshal(anchorRoles); err == nil {
				anchorRolesJSON = string(body)
			}
		}
		repoSymbols := uniqueStrings(fm.Symbols)
		noteID := ulid.Make().String()
		now := timeutil.NowUTC()
		if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_notes (id, vault_root, path, title, search_text, type, project, status, trust, primary_anchor_path, anchor_roles_json, updated_at, hash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, noteID, vaultRoot, filepath.ToSlash(rel), title, buildSearchText(parsed, tags), nullable(fm.Values["type"]), nullable(fm.Values["project"]), nullable(fm.Values["status"]), nullable(fm.Values["trust"]), nullable(filepath.ToSlash(primaryAnchorPath)), anchorRolesJSON, timeutil.FormatRFC3339Nano(now), "sha256:"+hex.EncodeToString(hash[:])); err != nil {
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
		for _, p := range anchorPaths {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO obsidian_anchor_paths (note_id, path)
VALUES (?, ?)
`, noteID, p); err != nil {
				return err
			}
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
	if err := retryObsidianBusy(ctx, func() error {
		if _, execErr := tx.ExecContext(ctx, `DELETE FROM obsidian_note_embeddings WHERE path NOT IN (SELECT path FROM obsidian_notes)`); execErr != nil {
			return fmt.Errorf("obsidianindex: prune embeddings: %w", execErr)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := retryObsidianBusy(ctx, func() error {
		if _, execErr := tx.ExecContext(ctx, `DELETE FROM obsidian_chunk_embeddings`); execErr != nil {
			return fmt.Errorf("obsidianindex: clear chunk embeddings: %w", execErr)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := retryObsidianBusy(ctx, func() error {
		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("obsidianindex: commit: %w", commitErr)
		}
		return nil
	}); err != nil {
		return nil, err
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
		q, q, q, q, q, q, q, q, q, q,
		q,
		q, q, q, q, q, q, q, q, q, q,
		limit,
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT path, title, COALESCE(type,''), COALESCE(project,''), COALESCE(status,''), COALESCE(trust,''), COALESCE(primary_anchor_path,''), COALESCE(anchor_roles_json,''),
       (CASE WHEN lower(title) LIKE ? THEN 5 ELSE 0 END) +
       (CASE WHEN lower(path) LIKE ? THEN 4 ELSE 0 END) +
       (CASE WHEN lower(search_text) LIKE ? THEN 2 ELSE 0 END) +
       (CASE WHEN EXISTS (SELECT 1 FROM obsidian_chunks c WHERE c.note_id = obsidian_notes.id AND lower(c.text) LIKE ?) THEN 3 ELSE 0 END) +
       (CASE WHEN EXISTS (SELECT 1 FROM obsidian_repo_paths rp WHERE rp.note_id = obsidian_notes.id AND lower(rp.path) LIKE ?) THEN 4 ELSE 0 END) +
       (CASE WHEN lower(COALESCE(primary_anchor_path,'')) LIKE ? THEN 6 ELSE 0 END) +
       (CASE WHEN EXISTS (SELECT 1 FROM obsidian_anchor_paths ap WHERE ap.note_id = obsidian_notes.id AND lower(ap.path) LIKE ?) THEN 5 ELSE 0 END) +
       (CASE WHEN EXISTS (SELECT 1 FROM obsidian_repo_symbols rs WHERE rs.note_id = obsidian_notes.id AND lower(rs.symbol) LIKE ?) THEN 4 ELSE 0 END) +
       (CASE WHEN EXISTS (SELECT 1 FROM obsidian_tags t JOIN obsidian_notes n3 ON t.note_id = n3.id WHERE n3.path = obsidian_notes.path AND lower(t.tag) LIKE ?) THEN 3 ELSE 0 END) +
       (CASE WHEN EXISTS (SELECT 1 FROM obsidian_aliases a JOIN obsidian_notes n2 ON a.note_id = n2.id WHERE n2.path = obsidian_notes.path AND lower(a.alias) LIKE ?) THEN 3 ELSE 0 END) AS score,
       COALESCE((SELECT c.text FROM obsidian_chunks c WHERE c.note_id = obsidian_notes.id AND lower(c.text) LIKE ? ORDER BY length(c.text) ASC LIMIT 1), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT rp.path) FROM obsidian_repo_paths rp WHERE rp.note_id = obsidian_notes.id), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT ap.path) FROM obsidian_anchor_paths ap WHERE ap.note_id = obsidian_notes.id), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT rs.symbol) FROM obsidian_repo_symbols rs WHERE rs.note_id = obsidian_notes.id), '')
FROM obsidian_notes
WHERE lower(title) LIKE ? OR lower(path) LIKE ? OR lower(search_text) LIKE ? OR lower(COALESCE(primary_anchor_path,'')) LIKE ? OR EXISTS (
  SELECT 1 FROM obsidian_chunks c WHERE c.note_id = obsidian_notes.id AND lower(c.text) LIKE ?
) OR EXISTS (
  SELECT 1 FROM obsidian_repo_paths rp WHERE rp.note_id = obsidian_notes.id AND lower(rp.path) LIKE ?
) OR EXISTS (
  SELECT 1 FROM obsidian_anchor_paths ap WHERE ap.note_id = obsidian_notes.id AND lower(ap.path) LIKE ?
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
		var repoPathsCSV, anchorPathsCSV, anchorRolesJSON, symbolsCSV string
		if err := rows.Scan(&hit.Path, &hit.Title, &hit.Type, &hit.Project, &hit.Status, &hit.Trust, &hit.PrimaryAnchorPath, &anchorRolesJSON, &hit.Score, &hit.Snippet, &repoPathsCSV, &anchorPathsCSV, &symbolsCSV); err != nil {
			return nil, fmt.Errorf("obsidianindex: scan search hit: %w", err)
		}
		hit.Snippet = compactSnippet(hit.Snippet)
		hit.RepoPaths = splitCSV(repoPathsCSV)
		hit.AnchorPaths = splitCSV(anchorPathsCSV)
		hit.AnchorRoles = decodeAnchorRolesJSON(anchorRolesJSON)
		hit.Symbols = splitCSV(symbolsCSV)
		out = append(out, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tokenHits, err := s.searchNotesByTerms(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	if len(tokenHits) > 0 {
		out = mergeSearchHits(out, tokenHits)
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Score != out[j].Score {
				return out[i].Score > out[j].Score
			}
			return out[i].Path < out[j].Path
		})
		if len(out) > limit {
			out = out[:limit]
		}
	}
	return out, nil
}

func (s *sqlStore) searchNotesByTerms(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	terms := noteSearchTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT path, title, COALESCE(type,''), COALESCE(project,''), COALESCE(status,''), COALESCE(trust,''), COALESCE(primary_anchor_path,''), COALESCE(anchor_roles_json,''),
       COALESCE(search_text, ''),
       COALESCE((SELECT c.text FROM obsidian_chunks c WHERE c.note_id = obsidian_notes.id ORDER BY length(c.text) ASC LIMIT 1), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT rp.path) FROM obsidian_repo_paths rp WHERE rp.note_id = obsidian_notes.id), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT ap.path) FROM obsidian_anchor_paths ap WHERE ap.note_id = obsidian_notes.id), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT rs.symbol) FROM obsidian_repo_symbols rs WHERE rs.note_id = obsidian_notes.id), '')
FROM obsidian_notes
ORDER BY path ASC
`)
	if err != nil {
		return nil, fmt.Errorf("obsidianindex: token note query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var hits []SearchHit
	for rows.Next() {
		var hit SearchHit
		var searchText, repoPathsCSV, anchorPathsCSV, anchorRolesJSON, symbolsCSV string
		if err := rows.Scan(&hit.Path, &hit.Title, &hit.Type, &hit.Project, &hit.Status, &hit.Trust, &hit.PrimaryAnchorPath, &anchorRolesJSON, &searchText, &hit.Snippet, &repoPathsCSV, &anchorPathsCSV, &symbolsCSV); err != nil {
			return nil, fmt.Errorf("obsidianindex: scan token note hit: %w", err)
		}
		hit.RepoPaths = splitCSV(repoPathsCSV)
		hit.AnchorPaths = splitCSV(anchorPathsCSV)
		hit.AnchorRoles = decodeAnchorRolesJSON(anchorRolesJSON)
		hit.Symbols = splitCSV(symbolsCSV)
		hit.Snippet = compactSnippet(hit.Snippet)
		hit.Score = scoreTokenizedNoteHit(hit, searchText, terms)
		if hit.Score <= 0 {
			continue
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, err
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

func scoreTokenizedNoteHit(hit SearchHit, searchText string, terms []string) int {
	title := strings.ToLower(strings.TrimSpace(hit.Title))
	path := strings.ToLower(strings.TrimSpace(hit.Path))
	primaryAnchorPath := strings.ToLower(strings.TrimSpace(hit.PrimaryAnchorPath))
	searchText = strings.ToLower(strings.TrimSpace(searchText))
	repoPaths := splitCSV(strings.ToLower(strings.Join(hit.RepoPaths, ",")))
	symbols := splitCSV(strings.ToLower(strings.Join(hit.Symbols, ",")))
	score := 0
	matches := 0
	for _, term := range terms {
		termMatched := false
		if strings.Contains(title, term) {
			score += 5
			termMatched = true
		}
		if strings.Contains(path, term) {
			score += 4
			termMatched = true
		}
		if primaryAnchorPath != "" && strings.Contains(primaryAnchorPath, term) {
			score += 6
			termMatched = true
		}
		if strings.Contains(searchText, term) {
			score += 2
			termMatched = true
		}
		for _, value := range repoPaths {
			if strings.Contains(value, term) {
				score += 4
				termMatched = true
				break
			}
		}
		for _, value := range symbols {
			if strings.Contains(value, term) {
				score += 4
				termMatched = true
				break
			}
		}
		if termMatched {
			matches++
		}
	}
	if matches >= 2 {
		score += matches * 2
	}
	return score
}

func noteSearchTerms(query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	for _, field := range strings.FieldsFunc(strings.ToLower(strings.TrimSpace(query)), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		field = strings.TrimSpace(field)
		if len(field) < 3 {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out
}

func mergeSearchHits(primary, secondary []SearchHit) []SearchHit {
	byPath := make(map[string]SearchHit, len(primary)+len(secondary))
	for _, hit := range primary {
		byPath[hit.Path] = hit
	}
	for _, hit := range secondary {
		existing, ok := byPath[hit.Path]
		if !ok {
			byPath[hit.Path] = hit
			continue
		}
		if hit.Score > existing.Score {
			existing.Score = hit.Score
		}
		if existing.Snippet == "" {
			existing.Snippet = hit.Snippet
		}
		if existing.Title == "" {
			existing.Title = hit.Title
		}
		if existing.Type == "" {
			existing.Type = hit.Type
		}
		if existing.Project == "" {
			existing.Project = hit.Project
		}
		if existing.Status == "" {
			existing.Status = hit.Status
		}
		if existing.Trust == "" {
			existing.Trust = hit.Trust
		}
		if existing.PrimaryAnchorPath == "" {
			existing.PrimaryAnchorPath = hit.PrimaryAnchorPath
		}
		if len(existing.AnchorRoles) == 0 {
			existing.AnchorRoles = hit.AnchorRoles
		}
		if len(existing.RepoPaths) == 0 {
			existing.RepoPaths = hit.RepoPaths
		}
		if len(existing.AnchorPaths) == 0 {
			existing.AnchorPaths = hit.AnchorPaths
		}
		if len(existing.Symbols) == 0 {
			existing.Symbols = hit.Symbols
		}
		byPath[hit.Path] = existing
	}
	out := make([]SearchHit, 0, len(byPath))
	for _, hit := range byPath {
		out = append(out, hit)
	}
	return out
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
	var tx *sql.Tx
	err = retryObsidianBusy(ctx, func() error {
		var beginErr error
		tx, beginErr = s.db.BeginTx(ctx, nil)
		if beginErr != nil {
			return fmt.Errorf("obsidianindex: begin semantic tx: %w", beginErr)
		}
		return nil
	})
	if err != nil {
		return 0, err
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
	if err := retryObsidianBusy(ctx, func() error {
		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("obsidianindex: commit semantic tx: %w", commitErr)
		}
		return nil
	}); err != nil {
		return 0, err
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
SELECT n.path, n.title, COALESCE(n.type,''), COALESCE(n.project,''), COALESCE(n.status,''), COALESCE(n.trust,''), COALESCE(n.primary_anchor_path,''), COALESCE(n.anchor_roles_json,''),
       COALESCE((SELECT c.text FROM obsidian_chunks c WHERE c.note_id = n.id ORDER BY length(c.text) ASC LIMIT 1), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT rp.path) FROM obsidian_repo_paths rp WHERE rp.note_id = n.id), ''),
       COALESCE((SELECT GROUP_CONCAT(DISTINCT ap.path) FROM obsidian_anchor_paths ap WHERE ap.note_id = n.id), ''),
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
		var repoPathsCSV, anchorPathsCSV, anchorRolesJSON, symbolsCSV, embeddingJSON string
		if err := rows.Scan(&hit.Path, &hit.Title, &hit.Type, &hit.Project, &hit.Status, &hit.Trust, &hit.PrimaryAnchorPath, &anchorRolesJSON, &hit.Snippet, &repoPathsCSV, &anchorPathsCSV, &symbolsCSV, &embeddingJSON); err != nil {
			return nil, fmt.Errorf("obsidianindex: scan semantic hit: %w", err)
		}
		var embedding []float32
		if err := json.Unmarshal([]byte(embeddingJSON), &embedding); err != nil {
			return nil, fmt.Errorf("obsidianindex: decode semantic embedding: %w", err)
		}
		hit.Score = int(cosineSimilarity(queryEmbedding, embedding) * 1000)
		hit.Snippet = compactSnippet(hit.Snippet)
		hit.RepoPaths = splitCSV(repoPathsCSV)
		hit.AnchorPaths = splitCSV(anchorPathsCSV)
		hit.AnchorRoles = decodeAnchorRolesJSON(anchorRolesJSON)
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
	Values      map[string]string
	Tags        []string
	Paths       []string
	AnchorPaths []string
	AnchorRoles map[string][]string
	Symbols     []string
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
	out := frontmatter{Values: map[string]string{}, AnchorRoles: map[string][]string{}}
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
	case "tags", "paths", "anchor_paths", "symbols", "impl_anchor_paths", "support_anchor_paths", "resource_anchor_paths":
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
	case "anchor_paths":
		out.AnchorPaths = append(out.AnchorPaths, filepath.ToSlash(item))
	case "impl_anchor_paths":
		appendAnchorRole(out, "impl", filepath.ToSlash(item))
	case "support_anchor_paths":
		appendAnchorRole(out, "support", filepath.ToSlash(item))
	case "resource_anchor_paths":
		appendAnchorRole(out, "resource", filepath.ToSlash(item))
	case "symbols":
		out.Symbols = append(out.Symbols, item)
	}
}

func appendAnchorRole(out *frontmatter, role, item string) {
	if out.AnchorRoles == nil {
		out.AnchorRoles = map[string][]string{}
	}
	role = strings.TrimSpace(strings.ToLower(role))
	item = filepath.ToSlash(strings.TrimSpace(item))
	if role == "" || item == "" {
		return
	}
	out.AnchorRoles[role] = append(out.AnchorRoles[role], item)
}

func normalizeAnchorRoles(raw map[string][]string) map[string][]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string][]string, len(raw))
	for role, paths := range raw {
		role = strings.TrimSpace(strings.ToLower(role))
		if role == "" {
			continue
		}
		normalized := normalizeUniqueFrontmatterPaths(paths)
		if len(normalized) == 0 {
			continue
		}
		out[role] = normalized
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeUniqueFrontmatterPaths(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, pathValue := range paths {
		pathValue = filepath.ToSlash(strings.TrimSpace(pathValue))
		if pathValue == "" {
			continue
		}
		if _, ok := seen[pathValue]; ok {
			continue
		}
		seen[pathValue] = struct{}{}
		out = append(out, pathValue)
	}
	return out
}

func decodeAnchorRolesJSON(raw string) map[string][]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var decoded map[string][]string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	return normalizeAnchorRoles(decoded)
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
	var totalChunks int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_chunks`).Scan(&totalChunks); err != nil {
		return 0, fmt.Errorf("obsidianindex: count chunks: %w", err)
	}
	if totalChunks == 0 {
		return 0, nil
	}
	var existingCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM obsidian_chunk_embeddings WHERE model = ?`, provider.Model()).Scan(&existingCount); err != nil {
		return 0, fmt.Errorf("obsidianindex: count chunk embeddings: %w", err)
	}
	if existingCount == totalChunks {
		return 0, nil
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
	var tx *sql.Tx
	err = retryObsidianBusy(ctx, func() error {
		var beginErr error
		tx, beginErr = s.db.BeginTx(ctx, nil)
		if beginErr != nil {
			return fmt.Errorf("obsidianindex: begin chunk semantic tx: %w", beginErr)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := retryObsidianBusy(ctx, func() error {
		if _, execErr := tx.ExecContext(ctx, `DELETE FROM obsidian_chunk_embeddings WHERE model = ?`, provider.Model()); execErr != nil {
			return fmt.Errorf("obsidianindex: clear chunk embeddings: %w", execErr)
		}
		return nil
	}); err != nil {
		return 0, err
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
	if err := retryObsidianBusy(ctx, func() error {
		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("obsidianindex: commit chunk semantic tx: %w", commitErr)
		}
		return nil
	}); err != nil {
		return 0, err
	}
	return len(candidates), nil
}

func retryObsidianBusy(ctx context.Context, fn func() error) error {
	deadline := time.Now().Add(obsidianBusyRetryWindow)
	var lastErr error
	for {
		err := fn()
		if err == nil || !isObsidianBusyErr(err) {
			return err
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		timer := time.NewTimer(obsidianBusyRetryStep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func isObsidianBusyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "sqlite_busy")
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
		return fmt.Errorf("obsidianindex: embedding dimension mismatch for model %q: stored=%d, provider=%d; run `foxctl obsidian index build --vault-path <vault-path>` to rebuild vault semantic embeddings", provider.Model(), dims, provider.Dimensions())
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
