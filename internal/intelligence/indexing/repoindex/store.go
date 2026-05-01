package repoindex

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

const (
	schemaVersion            = 3
	openContextMinimumBudget = 10 * time.Second
)

// SchemaVersion returns the current repoindex schema version.
func SchemaVersion() int {
	return schemaVersion
}

// ErrNotFound indicates the requested node or edge was not found.
var ErrNotFound = errors.New("not found")

// Store manages repoindex data in SQLite.
type Store struct {
	db       *sql.DB
	path     string
	repoRoot string
	repoKey  string
	close    func() error
}

// Open opens or creates a SQLite-backed repo index store for the given repo root.
// The database file is placed under storageRoot/repoindex using a deterministic key
// derived from repoRoot; if a legacy database file is present it will be migrated
// (renamed) to the new path. Open ensures the schema is migrated and configures
// recommended PRAGMA options before returning a Store representing the opened
// repository index, or an error if opening, migration, or setup fails.
//
// Index:
// - Purpose: Open a repoindex store for a repo root with schema migration and pragmas
// - Flow: resolve repo root → compute key → migrate legacy path → open db → set pragmas
// - SideEffects: filesystem access; SQLite schema migrations; PRAGMA writes
// - FailureModes: path resolution errors, open/migrate errors, pragma errors
// - Related: repoKey, migrate, sqliteutil.OpenDBShared
// - Keywords: repoindex, repo_key, sqlite, schema_version, OpenDBShared, PRAGMA, repo_root
func Open(ctx context.Context, storageRoot, repoRoot string) (*Store, error) {
	if repoRoot == "" {
		return nil, fmt.Errorf("repoindex: repo root is required")
	}
	absoluteRoot, dbPath, legacyPath, key, err := resolveStorePaths(storageRoot, repoRoot)
	if err != nil {
		return nil, err
	}
	if dbPath != legacyPath {
		if _, err := os.Stat(dbPath); err != nil {
			if os.IsNotExist(err) {
				if _, err := os.Stat(legacyPath); err == nil {
					if err := os.Rename(legacyPath, dbPath); err != nil {
						dbPath = legacyPath
					}
				}
			} else {
				return nil, fmt.Errorf("repoindex: stat db path: %w", err)
			}
		}
	}

	openCtx, cancelOpen := openContext(ctx, openContextMinimumBudget)
	defer cancelOpen()

	db, closeFn, err := dbutil.OpenSQLiteDBShared(openCtx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("repoindex: open db: %w", err)
	}
	if _, err := db.ExecContext(openCtx, "PRAGMA synchronous=NORMAL;"); err != nil {
		_ = closeFn()
		return nil, fmt.Errorf("repoindex: set synchronous: %w", err)
	}
	if _, err := db.ExecContext(openCtx, "PRAGMA temp_store=MEMORY;"); err != nil {
		_ = closeFn()
		return nil, fmt.Errorf("repoindex: set temp_store: %w", err)
	}

	return &Store{db: db, path: dbPath, repoRoot: absoluteRoot, repoKey: key, close: closeFn}, nil
}

// StoreExists reports whether a repoindex store already exists for repoRoot.
// It checks both the current deterministic path and the legacy path without
// creating a new database.
func StoreExists(storageRoot, repoRoot string) (bool, error) {
	_, dbPath, legacyPath, _, err := resolveStorePaths(storageRoot, repoRoot)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(dbPath); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if legacyPath != dbPath {
		if _, err := os.Stat(legacyPath); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}

func resolveStorePaths(storageRoot, repoRoot string) (absoluteRoot, dbPath, legacyPath, key string, err error) {
	if repoRoot == "" {
		return "", "", "", "", fmt.Errorf("repoindex: repo root is required")
	}
	absoluteRoot, err = filepath.Abs(repoRoot)
	if err != nil {
		return "", "", "", "", fmt.Errorf("repoindex: resolve repo root: %w", err)
	}
	absoluteRoot = filepath.Clean(absoluteRoot)

	key = repoKey(absoluteRoot)
	dir := strings.TrimSpace(os.Getenv("FOXCTL_REPOINDEX_DB_DIR"))
	if dir == "" {
		dir = filepath.Join(storageRoot, "repoindex")
	}
	dbPath = filepath.Join(dir, key+".db")
	legacyPath = filepath.Join(dir, legacyRepoKey(absoluteRoot)+".db")
	return absoluteRoot, dbPath, legacyPath, key, nil
}

func openContext(ctx context.Context, minimumBudget time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), minimumBudget)
	}
	if minimumBudget <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := ctx.Err(); err != nil || time.Until(deadline) < minimumBudget {
			return context.WithTimeout(context.WithoutCancel(ctx), minimumBudget)
		}
		return ctx, func() {}
	}
	if err := ctx.Err(); err != nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), minimumBudget)
}

// Close closes the underlying database.
func (s *Store) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// Path returns the on-disk database path.
func (s *Store) Path() string {
	return s.path
}

// RepoRoot returns the repo root associated with this store.
func (s *Store) RepoRoot() string {
	return s.repoRoot
}

// RepoKey returns the repo key associated with this store.
func (s *Store) RepoKey() string {
	return s.repoKey
}

// SetMeta stores index metadata.
func (s *Store) SetMeta(ctx context.Context, meta IndexMeta) error {
	if meta.IndexedAt.IsZero() {
		meta.IndexedAt = time.Now().UTC()
	}
	meta.SchemaVersion = schemaVersion

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback() //nolint:errcheck
	}()

	if err := setMetaValue(ctx, tx, "repo_root", meta.RepoRoot); err != nil {
		return err
	}
	if err := setMetaValue(ctx, tx, "head_sha", meta.HeadSHA); err != nil {
		return err
	}
	if err := setMetaValue(ctx, tx, "worktree_dirty", fmt.Sprintf("%t", meta.WorktreeDirty)); err != nil {
		return err
	}
	if err := setMetaValue(ctx, tx, "schema_version", fmt.Sprintf("%d", meta.SchemaVersion)); err != nil {
		return err
	}
	if err := setMetaValue(ctx, tx, "indexed_at_unix", fmt.Sprintf("%d", meta.IndexedAt.Unix())); err != nil {
		return err
	}
	if meta.Languages != nil {
		data, err := json.Marshal(meta.Languages)
		if err != nil {
			return err
		}
		if err := setMetaValue(ctx, tx, "languages_json", string(data)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetMeta returns index metadata stored in the database.
func (s *Store) GetMeta(ctx context.Context) (IndexMeta, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM index_meta`)
	if err != nil {
		return IndexMeta{}, err
	}
	defer rows.Close()

	meta := IndexMeta{SchemaVersion: schemaVersion}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return IndexMeta{}, err
		}
		switch key {
		case "repo_root":
			meta.RepoRoot = value
		case "head_sha":
			meta.HeadSHA = value
		case "worktree_dirty":
			meta.WorktreeDirty = value == "true" || value == "1"
		case "schema_version":
			if parsed, err := parseInt(value); err == nil {
				meta.SchemaVersion = parsed
			}
		case "indexed_at_unix":
			if parsed, err := parseInt64(value); err == nil {
				meta.IndexedAt = time.Unix(parsed, 0).UTC()
			}
		case "languages_json":
			if strings.TrimSpace(value) != "" {
				var languages []string
				if err := json.Unmarshal([]byte(value), &languages); err == nil {
					meta.Languages = languages
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return IndexMeta{}, err
	}
	return meta, nil
}

// ReplaceAll replaces the entire graph with the provided nodes and edges.
//
// Index:
// - Purpose: Replace the full repo graph atomically in SQLite
// - Flow: begin tx → delete existing nodes/edges → insert nodes → insert edges → commit
// - SideEffects: SQLite writes within a transaction
// - FailureModes: insert failures, transaction errors
// - Related: Store.SetMeta, Store.Stats
// - Keywords: repoindex, ReplaceAll, nodes, edges, transaction, sqlite
func (s *Store) ReplaceAll(ctx context.Context, nodes []Node, edges []Edge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback() //nolint:errcheck
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM edges`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM nodes`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM symbol_locator`); err != nil {
		return err
	}

	stmtNode, err := tx.PrepareContext(ctx, `
		INSERT INTO nodes (
			id, repo_key, kind, pkg, file, name, signature, span_start, span_end,
			exported, doc, summary, meta_json, hash, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmtNode.Close()

	now := time.Now().UTC()
	for _, node := range nodes {
		updated := node.UpdatedAt
		if updated.IsZero() {
			updated = now
		}
		if _, err := stmtNode.ExecContext(ctx,
			node.ID,
			s.repoKey,
			string(node.Kind),
			nullIfEmpty(node.Pkg),
			nullIfEmpty(node.File),
			nullIfEmpty(node.Name),
			nullIfEmpty(node.Signature),
			nullIfZero(node.SpanStart),
			nullIfZero(node.SpanEnd),
			boolToInt(node.Exported),
			nullIfEmpty(node.Doc),
			nullIfEmpty(node.Summary),
			nullIfEmpty(string(node.Meta)),
			nullIfEmpty(node.Hash),
			updated.Unix(),
		); err != nil {
			return err
		}
	}

	stmtEdge, err := tx.PrepareContext(ctx, `
		INSERT INTO edges (src, dst, type, weight, meta_json, repo_key)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(src, dst, type, repo_key) DO UPDATE SET
			weight=excluded.weight,
			meta_json=excluded.meta_json
	`)
	if err != nil {
		return err
	}
	defer stmtEdge.Close()

	for _, edge := range edges {
		if _, err := stmtEdge.ExecContext(ctx, edge.Src, edge.Dst, string(edge.Type), edge.Weight, nullIfEmpty(string(edge.Meta)), s.repoKey); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// SearchFTS searches nodes using full-text search.
func (s *Store) SearchFTS(ctx context.Context, query string, limit int) ([]Node, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT nodes.id, nodes.kind, nodes.pkg, nodes.file, nodes.name, nodes.signature,
		       nodes.span_start, nodes.span_end, nodes.exported, nodes.doc, nodes.summary,
		       nodes.meta_json, nodes.hash, nodes.updated_at
		FROM node_fts
		JOIN nodes ON nodes.rowid = node_fts.rowid
		WHERE node_fts MATCH ? AND nodes.repo_key = ?
		ORDER BY bm25(node_fts)
		LIMIT ?
	`, query, s.repoKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanNodes(rows)
}

// SearchFTSScored searches nodes using full-text search and returns BM25 scores.
// Lower BM25 is better; callers should normalize as needed.
func (s *Store) SearchFTSScored(ctx context.Context, query string, limit int) ([]ScoredNode, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT nodes.id, nodes.kind, nodes.pkg, nodes.file, nodes.name, nodes.signature,
		       nodes.span_start, nodes.span_end, nodes.exported, nodes.doc, nodes.summary,
		       nodes.meta_json, nodes.hash, nodes.updated_at,
		       bm25(node_fts) AS score
		FROM node_fts
		JOIN nodes ON nodes.rowid = node_fts.rowid
		WHERE node_fts MATCH ? AND nodes.repo_key = ?
		ORDER BY score
		LIMIT ?
	`, query, s.repoKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scored []ScoredNode
	for rows.Next() {
		var (
			node      Node
			kind      string
			pkg       sql.NullString
			file      sql.NullString
			name      sql.NullString
			signature sql.NullString
			spanStart sql.NullInt64
			spanEnd   sql.NullInt64
			exported  int
			doc       sql.NullString
			summary   sql.NullString
			meta      sql.NullString
			hash      sql.NullString
			updated   int64
			score     float64
		)
		if err := rows.Scan(
			&node.ID,
			&kind,
			&pkg,
			&file,
			&name,
			&signature,
			&spanStart,
			&spanEnd,
			&exported,
			&doc,
			&summary,
			&meta,
			&hash,
			&updated,
			&score,
		); err != nil {
			return nil, err
		}
		node.Kind = NodeKind(kind)
		if pkg.Valid {
			node.Pkg = pkg.String
		}
		if file.Valid {
			node.File = file.String
		}
		if name.Valid {
			node.Name = name.String
		}
		if signature.Valid {
			node.Signature = signature.String
		}
		if spanStart.Valid {
			node.SpanStart = int(spanStart.Int64)
		}
		if spanEnd.Valid {
			node.SpanEnd = int(spanEnd.Int64)
		}
		node.Exported = exported > 0
		if doc.Valid {
			node.Doc = doc.String
		}
		if summary.Valid {
			node.Summary = summary.String
		}
		if meta.Valid {
			node.Meta = []byte(meta.String)
		}
		if hash.Valid {
			node.Hash = hash.String
		}
		node.UpdatedAt = time.Unix(updated, 0).UTC()
		scored = append(scored, ScoredNode{Node: node, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return scored, nil
}

// GetNode returns a node by ID.
func (s *Store) GetNode(ctx context.Context, id string) (Node, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, kind, pkg, file, name, signature, span_start, span_end, exported, doc, summary, meta_json, hash, updated_at
		FROM nodes
		WHERE id = ? AND repo_key = ?
	`, id, s.repoKey)

	node, err := scanNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Node{}, ErrNotFound
		}
		return Node{}, err
	}
	return node, nil
}

// GetNodes returns nodes by ID.
func (s *Store) GetNodes(ctx context.Context, ids []string) ([]Node, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	clause, args := buildInClause(ids)
	query := fmt.Sprintf(`
		SELECT id, kind, pkg, file, name, signature, span_start, span_end, exported, doc, summary, meta_json, hash, updated_at
		FROM nodes
		WHERE id IN (%s) AND repo_key = ?
	`, clause)
	args = append(args, s.repoKey)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanNodes(rows)
}

// ListNodesByKind returns repo nodes of a specific kind ordered by package, file, and name.
func (s *Store) ListNodesByKind(ctx context.Context, kind NodeKind, limit int) ([]Node, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, pkg, file, name, signature, span_start, span_end, exported, doc, summary, meta_json, hash, updated_at
		FROM nodes
		WHERE repo_key = ? AND kind = ?
		ORDER BY COALESCE(pkg, ''), COALESCE(file, ''), COALESCE(name, ''), id
		LIMIT ?
	`, s.repoKey, string(kind), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// GetOutgoingEdges returns outgoing edges for a node.
func (s *Store) GetOutgoingEdges(ctx context.Context, srcID string, types []EdgeType, limit int) ([]Edge, error) {
	return getEdges(ctx, s.db, s.repoKey, "src", srcID, types, limit)
}

// GetIncomingEdges returns incoming edges for a node.
func (s *Store) GetIncomingEdges(ctx context.Context, dstID string, types []EdgeType, limit int) ([]Edge, error) {
	return getEdges(ctx, s.db, s.repoKey, "dst", dstID, types, limit)
}

// Stats returns aggregate node and edge counts.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	stats := Stats{NodesByKind: make(map[NodeKind]int)}

	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE repo_key = ?`, s.repoKey).Scan(&stats.NodesTotal); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges WHERE repo_key = ?`, s.repoKey).Scan(&stats.EdgesTotal); err != nil {
		return Stats{}, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM nodes WHERE repo_key = ? GROUP BY kind`, s.repoKey)
	if err != nil {
		return Stats{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			return Stats{}, err
		}
		stats.NodesByKind[NodeKind(kind)] = count
	}
	if err := rows.Err(); err != nil {
		return Stats{}, err
	}

	return stats, nil
}

// ListFilesInScope returns indexed file node paths within the requested relative scope path.
func (s *Store) ListFilesInScope(ctx context.Context, scopePath string, isDir bool) ([]string, error) {
	scopePath = filepath.ToSlash(strings.TrimSpace(scopePath))
	var (
		query string
		args  []any
		rows  *sql.Rows
		err   error
	)
	switch {
	case scopePath == "" || scopePath == ".":
		query = `SELECT file FROM nodes WHERE repo_key = ? AND kind = ? AND file IS NOT NULL ORDER BY file ASC`
		args = []any{s.repoKey, string(NodeFile)}
	case isDir:
		query = `SELECT file FROM nodes WHERE repo_key = ? AND kind = ? AND (file = ? OR file LIKE ?) ORDER BY file ASC`
		args = []any{s.repoKey, string(NodeFile), scopePath, scopePath + "/%"}
	default:
		query = `SELECT file FROM nodes WHERE repo_key = ? AND kind = ? AND file = ? ORDER BY file ASC`
		args = []any{s.repoKey, string(NodeFile), scopePath}
	}
	rows, err = s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]string, 0)
	for rows.Next() {
		var file string
		if err := rows.Scan(&file); err != nil {
			return nil, err
		}
		file = filepath.ToSlash(strings.TrimSpace(file))
		if file != "" {
			files = append(files, file)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS index_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`); err != nil {
		return err
	}
	currentVersion, err := loadSchemaVersion(ctx, db)
	if err != nil {
		return err
	}
	if currentVersion != 0 && currentVersion != schemaVersion {
		log.Printf("repoindex: schema version change %d -> %d; resetting index", currentVersion, schemaVersion)
		if err := resetSchema(ctx, db); err != nil {
			return err
		}
	}

	schema := `
	PRAGMA foreign_keys = ON;

	CREATE TABLE IF NOT EXISTS index_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS nodes (
		id TEXT PRIMARY KEY,
		repo_key TEXT NOT NULL,
		kind TEXT NOT NULL,
		pkg TEXT,
		file TEXT,
		name TEXT,
		signature TEXT,
		span_start INTEGER,
		span_end INTEGER,
		exported INTEGER NOT NULL DEFAULT 0,
		doc TEXT,
		summary TEXT,
		meta_json TEXT,
		hash TEXT,
		updated_at INTEGER NOT NULL,
		CHECK (substr(id, 1, length(repo_key)+2) = repo_key || '::')
	);

	CREATE INDEX IF NOT EXISTS idx_nodes_kind ON nodes(kind);
	CREATE INDEX IF NOT EXISTS idx_nodes_pkg ON nodes(pkg);
	CREATE INDEX IF NOT EXISTS idx_nodes_file ON nodes(file);
	CREATE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name);
	CREATE INDEX IF NOT EXISTS idx_nodes_repo_key ON nodes(repo_key);

	CREATE VIRTUAL TABLE IF NOT EXISTS node_fts USING fts5(
		id UNINDEXED,
		name,
		signature,
		doc,
		summary,
		content='nodes',
		content_rowid='rowid'
	);

	CREATE TRIGGER IF NOT EXISTS nodes_ai AFTER INSERT ON nodes BEGIN
		INSERT INTO node_fts(rowid, id, name, signature, doc, summary)
		VALUES (new.rowid, new.id, new.name, new.signature, new.doc, new.summary);
	END;

	CREATE TRIGGER IF NOT EXISTS nodes_ad AFTER DELETE ON nodes BEGIN
		INSERT INTO node_fts(node_fts, rowid, id, name, signature, doc, summary)
		VALUES('delete', old.rowid, old.id, old.name, old.signature, old.doc, old.summary);
	END;

	CREATE TRIGGER IF NOT EXISTS nodes_au AFTER UPDATE ON nodes BEGIN
		INSERT INTO node_fts(node_fts, rowid, id, name, signature, doc, summary)
		VALUES('delete', old.rowid, old.id, old.name, old.signature, old.doc, old.summary);
		INSERT INTO node_fts(rowid, id, name, signature, doc, summary)
		VALUES (new.rowid, new.id, new.name, new.signature, new.doc, new.summary);
	END;

	CREATE TABLE IF NOT EXISTS edges (
		src TEXT NOT NULL,
		dst TEXT NOT NULL,
		type TEXT NOT NULL,
		weight REAL NOT NULL,
		meta_json TEXT,
		repo_key TEXT NOT NULL,
		PRIMARY KEY (src, dst, type, repo_key),
		CHECK (substr(src, 1, length(repo_key)+2) = repo_key || '::'),
		CHECK (substr(dst, 1, length(repo_key)+2) = repo_key || '::'),
		FOREIGN KEY (src) REFERENCES nodes(id) ON DELETE CASCADE,
		FOREIGN KEY (dst) REFERENCES nodes(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_edges_src_type ON edges(src, type, repo_key);
	CREATE INDEX IF NOT EXISTS idx_edges_dst_type ON edges(dst, type, repo_key);
	CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(type, repo_key);

	CREATE TABLE IF NOT EXISTS pkg_state (
		pkg TEXT PRIMARY KEY,
		files_hash TEXT NOT NULL,
		indexed_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS symbol_locator (
		symbol_key TEXT NOT NULL,
		pkg TEXT NOT NULL,
		file_path TEXT NOT NULL,
		name TEXT NOT NULL,
		kind TEXT,
		exported INTEGER NOT NULL DEFAULT 0,
		span_start INTEGER,
		span_end INTEGER,
		body_hash TEXT,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (symbol_key, pkg)
	);
	CREATE INDEX IF NOT EXISTS idx_locator_file ON symbol_locator(file_path);
	`

	if _, err := db.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO index_meta (key, value) VALUES ('schema_version', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value
	`, fmt.Sprintf("%d", schemaVersion)); err != nil {
		return err
	}
	return nil
}

func loadSchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	row := db.QueryRowContext(ctx, `SELECT value FROM index_meta WHERE key = 'schema_version'`)
	var value string
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	parsed, err := parseInt(value)
	if err != nil {
		return 0, nil
	}
	return parsed, nil
}

func resetSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		DROP TABLE IF EXISTS symbol_locator;
		DROP TRIGGER IF EXISTS nodes_ai;
		DROP TRIGGER IF EXISTS nodes_ad;
		DROP TRIGGER IF EXISTS nodes_au;
		DROP TABLE IF EXISTS node_fts;
		DROP TABLE IF EXISTS edges;
		DROP TABLE IF EXISTS nodes;
		DROP TABLE IF EXISTS pkg_state;
	`)
	return err
}

// UpsertLocator inserts or updates a symbol locator entry.
func (s *Store) UpsertLocator(ctx context.Context, loc LocatorEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO symbol_locator (symbol_key, pkg, file_path, name, kind, exported, span_start, span_end, body_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol_key, pkg) DO UPDATE SET
			file_path=excluded.file_path,
			name=excluded.name,
			kind=excluded.kind,
			exported=excluded.exported,
			span_start=excluded.span_start,
			span_end=excluded.span_end,
			body_hash=excluded.body_hash,
			updated_at=excluded.updated_at
	`, loc.SymbolKey, loc.Pkg, loc.FilePath, loc.Name, loc.Kind,
		boolToInt(loc.Exported), nullIfZero(loc.SpanStart), nullIfZero(loc.SpanEnd),
		nullIfEmpty(loc.BodyHash), loc.UpdatedAt)
	return err
}

// LookupLocator returns a locator entry by symbol key and package.
func (s *Store) LookupLocator(ctx context.Context, symbolKey, pkg string) (*LocatorEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT symbol_key, pkg, file_path, name, kind, exported, span_start, span_end, body_hash, updated_at
		FROM symbol_locator
		WHERE symbol_key = ? AND pkg = ?
	`, symbolKey, pkg)

	var loc LocatorEntry
	var kind sql.NullString
	var exported int
	var spanStart, spanEnd sql.NullInt64
	var bodyHash sql.NullString
	if err := row.Scan(
		&loc.SymbolKey, &loc.Pkg, &loc.FilePath, &loc.Name,
		&kind, &exported, &spanStart, &spanEnd, &bodyHash, &loc.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if kind.Valid {
		loc.Kind = kind.String
	}
	loc.Exported = exported > 0
	if spanStart.Valid {
		loc.SpanStart = int(spanStart.Int64)
	}
	if spanEnd.Valid {
		loc.SpanEnd = int(spanEnd.Int64)
	}
	if bodyHash.Valid {
		loc.BodyHash = bodyHash.String
	}
	return &loc, nil
}

// LookupLocatorsByFile returns all locator entries for a given file path.
func (s *Store) LookupLocatorsByFile(ctx context.Context, filePath string) ([]LocatorEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol_key, pkg, file_path, name, kind, exported, span_start, span_end, body_hash, updated_at
		FROM symbol_locator
		WHERE file_path = ?
	`, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locators []LocatorEntry
	for rows.Next() {
		var loc LocatorEntry
		var kind sql.NullString
		var exported int
		var spanStart, spanEnd sql.NullInt64
		var bodyHash sql.NullString
		if err := rows.Scan(
			&loc.SymbolKey, &loc.Pkg, &loc.FilePath, &loc.Name,
			&kind, &exported, &spanStart, &spanEnd, &bodyHash, &loc.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if kind.Valid {
			loc.Kind = kind.String
		}
		loc.Exported = exported > 0
		if spanStart.Valid {
			loc.SpanStart = int(spanStart.Int64)
		}
		if spanEnd.Valid {
			loc.SpanEnd = int(spanEnd.Int64)
		}
		if bodyHash.Valid {
			loc.BodyHash = bodyHash.String
		}
		locators = append(locators, loc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return locators, nil
}

func repoKey(root string) string {
	base := repoBaseName(root)
	hash := sha256.Sum256([]byte(root))
	suffix := hex.EncodeToString(hash[:8])
	return base + "-repoindex-" + suffix
}

func legacyRepoKey(root string) string {
	base := repoBaseName(root)
	hash := sha256.Sum256([]byte(root))
	suffix := hex.EncodeToString(hash[:8])
	return base + "-" + suffix
}

func repoBaseName(root string) string {
	base := filepath.Base(root)
	base = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, base)
	if base == "" {
		base = "repo"
	}
	return base
}

func setMetaValue(ctx context.Context, tx *sql.Tx, key, value string) error {
	if key == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO index_meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value
	`, key, value)
	return err
}

func parseInt(value string) (int, error) {
	parsed, err := parseInt64(value)
	if err != nil {
		return 0, err
	}
	return int(parsed), nil
}

func parseInt64(value string) (int64, error) {
	var parsed int64
	_, err := fmt.Sscanf(value, "%d", &parsed)
	return parsed, err
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullIfZero(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func buildInClause(values []string) (string, []any) {
	placeholders := make([]string, len(values))
	args := make([]any, len(values))
	for i, value := range values {
		placeholders[i] = "?"
		args[i] = value
	}
	return strings.Join(placeholders, ","), args
}

func scanNodes(rows *sql.Rows) ([]Node, error) {
	var nodes []Node
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nodes, nil
}

func scanNode(scanner interface{ Scan(...any) error }) (Node, error) {
	var (
		node      Node
		kind      string
		pkg       sql.NullString
		file      sql.NullString
		name      sql.NullString
		signature sql.NullString
		spanStart sql.NullInt64
		spanEnd   sql.NullInt64
		exported  int
		doc       sql.NullString
		summary   sql.NullString
		meta      sql.NullString
		hash      sql.NullString
		updated   int64
	)
	if err := scanner.Scan(
		&node.ID,
		&kind,
		&pkg,
		&file,
		&name,
		&signature,
		&spanStart,
		&spanEnd,
		&exported,
		&doc,
		&summary,
		&meta,
		&hash,
		&updated,
	); err != nil {
		return Node{}, err
	}
	if pkg.Valid {
		node.Pkg = pkg.String
	}
	if file.Valid {
		node.File = file.String
	}
	if name.Valid {
		node.Name = name.String
	}
	if signature.Valid {
		node.Signature = signature.String
	}
	if spanStart.Valid {
		node.SpanStart = int(spanStart.Int64)
	}
	if spanEnd.Valid {
		node.SpanEnd = int(spanEnd.Int64)
	}
	node.Exported = exported == 1
	if doc.Valid {
		node.Doc = doc.String
	}
	if summary.Valid {
		node.Summary = summary.String
	}
	if meta.Valid {
		node.Meta = []byte(meta.String)
	}
	if hash.Valid {
		node.Hash = hash.String
	}
	node.Kind = NodeKind(kind)
	node.UpdatedAt = time.Unix(updated, 0).UTC()
	return node, nil
}

func getEdges(ctx context.Context, db *sql.DB, repoKey, field, id string, types []EdgeType, limit int) ([]Edge, error) {
	if limit <= 0 {
		limit = 100
	}
	var (
		query string
		args  []any
	)
	if len(types) == 0 {
		query = fmt.Sprintf(`SELECT src, dst, type, weight, meta_json
			FROM edges
			WHERE %s = ? AND repo_key = ?
			ORDER BY weight DESC, type ASC, dst ASC, src ASC
			LIMIT ?`, field)
		args = []any{id, repoKey, limit}
	} else {
		placeholders := make([]string, len(types))
		args = make([]any, 0, len(types)+3)
		args = append(args, id)
		for i, edgeType := range types {
			placeholders[i] = "?"
			args = append(args, string(edgeType))
		}
		args = append(args, repoKey, limit)
		query = fmt.Sprintf(`SELECT src, dst, type, weight, meta_json
			FROM edges
			WHERE %s = ? AND type IN (%s) AND repo_key = ?
			ORDER BY weight DESC, type ASC, dst ASC, src ASC
			LIMIT ?`, field, strings.Join(placeholders, ","))
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var edge Edge
		var edgeType string
		var meta sql.NullString
		if err := rows.Scan(&edge.Src, &edge.Dst, &edgeType, &edge.Weight, &meta); err != nil {
			return nil, err
		}
		edge.Type = EdgeType(edgeType)
		if meta.Valid {
			edge.Meta = []byte(meta.String)
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return edges, nil
}
