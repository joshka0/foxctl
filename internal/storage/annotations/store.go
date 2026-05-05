package annotations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	"github.com/joshka0/foxctl/internal/storage/vector"
)

// TurnAnnotation aliases the shared storage type.
type TurnAnnotation = storage.TurnAnnotation

// ErrNotFound indicates an annotation was not found.
var ErrNotFound = fmt.Errorf("annotations: not found")

// Store handles turn annotation persistence.
type Store struct {
	db    *sql.DB
	path  string
	close func() error
}

// Connection pool defaults.
const (
	defaultMaxOpenConns    = 10
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 10 * time.Minute
	defaultConnMaxIdleTime = 15 * time.Minute
)

const selectColumns = `
SELECT id, session_id, turn_index, context_window_index,
	byte_offset, byte_length, line_num, timestamp,
	chunk_type, role, code_blocks, commands, errors,
	file_paths, symbols, tools_used, content_preview, content_hash,
	toc_label, toc_category, intent, annotation_model, annotation_version,
	embedding, embedding_model, embedding_text,
	has_error, is_compact_boundary, pre_compact_tokens,
	created_at, updated_at
FROM turn_annotations`

// Open opens (or creates) a standalone annotations database.
// If dbPath is empty, it defaults to ~/.foxctl/storage/annotations.db.
func Open(ctx context.Context, dbPath string) (store *Store, err error) {
	resolvedPath, err := resolveDBPath(dbPath)
	if err != nil {
		return nil, fmt.Errorf("annotations: resolve db path: %w", err)
	}

	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, resolvedPath, migrateSchema)
	if err != nil {
		return nil, fmt.Errorf("annotations: open db: %w", err)
	}
	defer func() {
		if err != nil {
			_ = closeFn()
		}
	}()

	db.SetMaxOpenConns(defaultMaxOpenConns)
	db.SetMaxIdleConns(defaultMaxIdleConns)
	db.SetConnMaxLifetime(defaultConnMaxLifetime)
	db.SetConnMaxIdleTime(defaultConnMaxIdleTime)

	store = &Store{db: db, path: resolvedPath, close: closeFn}
	return store, nil
}

// Close releases resources.
func (s *Store) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// Save inserts or updates an annotation.
func (s *Store) Save(ctx context.Context, ann *TurnAnnotation) error {
	if ann == nil {
		return fmt.Errorf("annotations: save: nil annotation")
	}
	if strings.TrimSpace(ann.ID) == "" {
		return fmt.Errorf("annotations: save: missing id")
	}
	if strings.TrimSpace(ann.SessionID) == "" {
		return fmt.Errorf("annotations: save: missing session_id")
	}
	if strings.TrimSpace(ann.ChunkType) == "" {
		return fmt.Errorf("annotations: save: missing chunk_type")
	}
	if strings.TrimSpace(ann.Role) == "" {
		return fmt.Errorf("annotations: save: missing role")
	}
	if strings.TrimSpace(ann.ContentHash) == "" {
		return fmt.Errorf("annotations: save: missing content_hash")
	}

	now := timeutil.NowUTC()
	if ann.CreatedAt.IsZero() {
		ann.CreatedAt = now
	}
	ann.UpdatedAt = now

	codeBlocksJSON, err := sqlutil.FormatJSON(ann.CodeBlocks)
	if err != nil {
		return fmt.Errorf("annotations: format code_blocks: %w", err)
	}
	commandsJSON, err := sqlutil.FormatJSON(ann.Commands)
	if err != nil {
		return fmt.Errorf("annotations: format commands: %w", err)
	}
	errorsJSON, err := sqlutil.FormatJSON(ann.Errors)
	if err != nil {
		return fmt.Errorf("annotations: format errors: %w", err)
	}
	filePathsJSON, err := sqlutil.FormatJSON(ann.FilePaths)
	if err != nil {
		return fmt.Errorf("annotations: format file_paths: %w", err)
	}
	symbolsJSON, err := sqlutil.FormatJSON(ann.Symbols)
	if err != nil {
		return fmt.Errorf("annotations: format symbols: %w", err)
	}
	toolsUsedJSON, err := sqlutil.FormatJSON(ann.ToolsUsed)
	if err != nil {
		return fmt.Errorf("annotations: format tools_used: %w", err)
	}

	if len(ann.Embedding) > 0 {
		if emb := vector.DeserializeF32(ann.Embedding); len(emb) > 0 {
			ann.Embedding = vector.SerializeF32(emb)
		}
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO turn_annotations (
	id, session_id, turn_index, context_window_index,
	byte_offset, byte_length, line_num, timestamp,
	chunk_type, role, code_blocks, commands, errors,
	file_paths, symbols, tools_used, content_preview, content_hash,
	toc_label, toc_category, intent, annotation_model, annotation_version,
	embedding, embedding_model, embedding_text,
	has_error, is_compact_boundary, pre_compact_tokens,
	created_at, updated_at
) VALUES (
	$1, $2, $3, $4,
	$5, $6, $7, $8,
	$9, $10, $11, $12, $13,
	$14, $15, $16, $17, $18,
	$19, $20, $21, $22, $23,
	$24, $25, $26,
	$27, $28, $29,
	$30, $31
)
ON CONFLICT(session_id, turn_index) DO UPDATE SET
	context_window_index = excluded.context_window_index,
	byte_offset = excluded.byte_offset,
	byte_length = excluded.byte_length,
	line_num = excluded.line_num,
	timestamp = excluded.timestamp,
	chunk_type = excluded.chunk_type,
	role = excluded.role,
	code_blocks = excluded.code_blocks,
	commands = excluded.commands,
	errors = excluded.errors,
	file_paths = excluded.file_paths,
	symbols = excluded.symbols,
	tools_used = excluded.tools_used,
	content_preview = excluded.content_preview,
	content_hash = excluded.content_hash,
	toc_label = excluded.toc_label,
	toc_category = excluded.toc_category,
	intent = excluded.intent,
	annotation_model = excluded.annotation_model,
	annotation_version = excluded.annotation_version,
	embedding = COALESCE(excluded.embedding, turn_annotations.embedding),
	embedding_model = COALESCE(excluded.embedding_model, turn_annotations.embedding_model),
	embedding_text = COALESCE(excluded.embedding_text, turn_annotations.embedding_text),
	has_error = excluded.has_error,
	is_compact_boundary = excluded.is_compact_boundary,
	pre_compact_tokens = excluded.pre_compact_tokens,
	updated_at = excluded.updated_at`,
		ann.ID, ann.SessionID, ann.TurnIndex, ann.ContextWindowIndex,
		ann.ByteOffset, ann.ByteLength, ann.LineNum, sqlutil.FormatTimestamp(ann.Timestamp),
		ann.ChunkType, ann.Role, codeBlocksJSON, commandsJSON, errorsJSON,
		filePathsJSON, symbolsJSON, toolsUsedJSON, ann.ContentPreview, ann.ContentHash,
		ann.TOCLabel, ann.TOCCategory, ann.Intent, ann.AnnotationModel, ann.AnnotationVersion,
		ann.Embedding, ann.EmbeddingModel, ann.EmbeddingText,
		boolToInt(ann.HasError), boolToInt(ann.IsCompactBoundary), ann.PreCompactTokens,
		sqlutil.FormatTimestamp(ann.CreatedAt), sqlutil.FormatTimestamp(ann.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("annotations: save: %w", err)
	}
	return nil
}

// Get retrieves an annotation by ID.
func (s *Store) Get(ctx context.Context, id string) (*TurnAnnotation, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE id = $1`, id)
	return scanAnnotation(row)
}

// GetBySessionTurn retrieves an annotation by session and turn index.
func (s *Store) GetBySessionTurn(ctx context.Context, sessionID string, turnIndex int) (*TurnAnnotation, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE session_id = $1 AND turn_index = $2`, sessionID, turnIndex)
	return scanAnnotation(row)
}

// ListBySession lists annotations for a session ordered by turn index.
func (s *Store) ListBySession(ctx context.Context, sessionID string) ([]*TurnAnnotation, error) {
	rows, err := s.db.QueryContext(ctx, selectColumns+` WHERE session_id = $1 ORDER BY turn_index ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("annotations: list by session: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close annotations list rows")
	}()

	return scanAnnotations(rows)
}

// FindByContentHash finds the most recent annotation matching a content hash.
func (s *Store) FindByContentHash(ctx context.Context, hash string) (*TurnAnnotation, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE content_hash = $1 ORDER BY created_at DESC LIMIT 1`, hash)
	return scanAnnotation(row)
}

// DeleteBySession removes all annotations for a session.
func (s *Store) DeleteBySession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM turn_annotations WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("annotations: delete by session: %w", err)
	}
	return nil
}

// ScoredAnnotation pairs an annotation with its similarity score.
type ScoredAnnotation struct {
	*TurnAnnotation
	Similarity float64
}

// AnnotationSearchOptions controls filtering for annotation-level vector search.
type AnnotationSearchOptions struct {
	Limit       int
	TOCCategory string
	HasErrors   bool
	SessionIDs  []string
}

// FileTrackingSummary summarizes file activity within a session.
type FileTrackingSummary struct {
	SessionID  string    `json:"session_id"`
	TurnCount  int       `json:"turn_count"`
	Categories string    `json:"categories"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
}

// CategoryCount tracks annotation counts per category.
type CategoryCount struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// SearchSimilar returns top-N annotations by cosine similarity.
func (s *Store) SearchSimilar(ctx context.Context, embedding []float32, limit int) ([]ScoredAnnotation, error) {
	return s.SearchSimilarFiltered(ctx, embedding, AnnotationSearchOptions{Limit: limit})
}

// SearchSimilarFiltered returns top-N annotations by cosine similarity with optional prefilters.
func (s *Store) SearchSimilarFiltered(ctx context.Context, embedding []float32, opts AnnotationSearchOptions) ([]ScoredAnnotation, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	if len(embedding) == 0 {
		return nil, nil
	}

	query := selectColumns + ` WHERE embedding IS NOT NULL AND LENGTH(embedding) > 0`
	args := make([]any, 0, 4)
	argIdx := 0

	if cat := strings.TrimSpace(opts.TOCCategory); cat != "" {
		argIdx++
		query += fmt.Sprintf(` AND LOWER(toc_category) = LOWER($%d)`, argIdx)
		args = append(args, cat)
	}
	if opts.HasErrors {
		query += ` AND COALESCE(NULLIF(TRIM(errors), ''), '[]') NOT IN ('[]', 'null', '[""]')`
	}
	if cond, condArgs := buildSessionScopeCondition(opts.SessionIDs, &argIdx); cond != "" {
		query += " AND " + cond
		args = append(args, condArgs...)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("annotations: search similar: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close annotations search rows")
	}()

	anns, err := scanAnnotations(rows)
	if err != nil {
		return nil, err
	}

	scored := make([]ScoredAnnotation, 0, len(anns))
	for _, ann := range anns {
		if len(ann.Embedding) == 0 {
			continue
		}
		candidateEmbedding := vector.DeserializeF32(ann.Embedding)
		if len(candidateEmbedding) == 0 || len(candidateEmbedding) != len(embedding) {
			continue
		}
		scored = append(scored, ScoredAnnotation{TurnAnnotation: ann, Similarity: vector.Cosine(embedding, candidateEmbedding)})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Similarity > scored[j].Similarity
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	return scored, nil
}

// ListBySessionTurnRange lists annotations in (startTurnExclusive, endTurnInclusive], ordered by turn index ascending.
func (s *Store) ListBySessionTurnRange(ctx context.Context, sessionID string, startTurnExclusive, endTurnInclusive int, category string, limit int) ([]*TurnAnnotation, error) {
	if limit <= 0 {
		limit = 20
	}

	query := selectColumns + ` WHERE session_id = $1 AND turn_index > $2 AND turn_index <= $3`
	args := []any{sessionID, startTurnExclusive, endTurnInclusive}
	argIdx := 3
	if cat := strings.TrimSpace(category); cat != "" {
		argIdx++
		query += fmt.Sprintf(` AND LOWER(toc_category) = LOWER($%d)`, argIdx)
		args = append(args, cat)
	}
	argIdx++
	query += fmt.Sprintf(` ORDER BY turn_index ASC LIMIT $%d`, argIdx)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("annotations: list by session turn range: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close annotations turn range rows")
	}()

	return scanAnnotations(rows)
}

// ListByFilePath lists annotations that reference filePath exactly in file_paths JSON array.
func (s *Store) ListByFilePath(ctx context.Context, filePath string, sessionIDs []string, limit int) ([]*TurnAnnotation, error) {
	if limit <= 0 {
		limit = 20
	}
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return nil, nil
	}

	query := selectColumns + ` WHERE EXISTS (SELECT 1 FROM json_each(turn_annotations.file_paths) WHERE value = $1)`
	args := []any{filePath}
	argIdx := 1
	if cond, condArgs := buildSessionScopeCondition(sessionIDs, &argIdx); cond != "" {
		query += " AND " + cond
		args = append(args, condArgs...)
	}
	argIdx++
	query += fmt.Sprintf(` ORDER BY COALESCE(NULLIF(timestamp, ''), created_at) DESC LIMIT $%d`, argIdx)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("annotations: list by file path: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close annotations file path rows")
	}()

	return scanAnnotations(rows)
}

// SummarizeByFilePath returns per-session file activity summaries for filePath exact matches.
func (s *Store) SummarizeByFilePath(ctx context.Context, filePath string, sessionIDs []string) ([]FileTrackingSummary, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return nil, nil
	}

	query := `
SELECT
	session_id,
	COUNT(*) AS turn_count,
	COALESCE(GROUP_CONCAT(DISTINCT COALESCE(NULLIF(toc_category, ''), 'context')), '') AS categories,
	MIN(COALESCE(NULLIF(timestamp, ''), created_at)) AS first_seen,
	MAX(COALESCE(NULLIF(timestamp, ''), created_at)) AS last_seen
FROM turn_annotations
WHERE EXISTS (SELECT 1 FROM json_each(turn_annotations.file_paths) WHERE value = $1)
`
	args := []any{filePath}
	argIdx := 1
	if cond, condArgs := buildSessionScopeCondition(sessionIDs, &argIdx); cond != "" {
		query += " AND " + cond
		args = append(args, condArgs...)
	}
	query += " GROUP BY session_id ORDER BY last_seen DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("annotations: summarize by file path: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close annotations file summary rows")
	}()

	out := make([]FileTrackingSummary, 0)
	for rows.Next() {
		var summary FileTrackingSummary
		var firstSeen, lastSeen sql.NullString
		if err := rows.Scan(&summary.SessionID, &summary.TurnCount, &summary.Categories, &firstSeen, &lastSeen); err != nil {
			return nil, fmt.Errorf("annotations: summarize by file path scan: %w", err)
		}
		if firstSeen.Valid {
			ts, err := sqlutil.ScanTimestamp(firstSeen.String)
			if err == nil {
				summary.FirstSeen = ts
			}
		}
		if lastSeen.Valid {
			ts, err := sqlutil.ScanTimestamp(lastSeen.String)
			if err == nil {
				summary.LastSeen = ts
			}
		}
		out = append(out, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("annotations: summarize by file path rows: %w", err)
	}
	return out, nil
}

// CountByCategory returns annotation counts grouped by category.
func (s *Store) CountByCategory(ctx context.Context, sessionIDs []string) ([]CategoryCount, error) {
	query := `
SELECT COALESCE(NULLIF(toc_category, ''), 'context') AS category, COUNT(*) AS count
FROM turn_annotations
`
	args := make([]any, 0, 4)
	argIdx := 0
	if cond, condArgs := buildSessionScopeCondition(sessionIDs, &argIdx); cond != "" {
		query += " WHERE " + cond
		args = append(args, condArgs...)
	}
	query += " GROUP BY COALESCE(NULLIF(toc_category, ''), 'context') ORDER BY count DESC, category ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("annotations: count by category: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close annotations category rows")
	}()

	out := make([]CategoryCount, 0)
	for rows.Next() {
		var cc CategoryCount
		if err := rows.Scan(&cc.Category, &cc.Count); err != nil {
			return nil, fmt.Errorf("annotations: count by category scan: %w", err)
		}
		out = append(out, cc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("annotations: count by category rows: %w", err)
	}
	return out, nil
}

// ListWithoutEmbedding returns annotations for a session that lack embeddings.
func (s *Store) ListWithoutEmbedding(ctx context.Context, sessionID string, limit int) ([]*TurnAnnotation, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, selectColumns+
		` WHERE session_id = $1 AND (embedding IS NULL OR LENGTH(embedding) = 0) ORDER BY turn_index ASC LIMIT $2`,
		sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("annotations: list without embedding: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close annotations list rows")
	}()
	return scanAnnotations(rows)
}

// SetEmbedding updates the embedding for an existing annotation.
func (s *Store) SetEmbedding(ctx context.Context, sessionID string, turnIndex int, embedding []float32, model string, embeddingText string) error {
	blob := vector.SerializeF32(embedding)
	now := timeutil.NowUTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx,
		`UPDATE turn_annotations SET embedding = $1, embedding_model = $2, embedding_text = $3, updated_at = $4 WHERE session_id = $5 AND turn_index = $6`,
		blob, model, embeddingText, now, sessionID, turnIndex)
	if err != nil {
		return fmt.Errorf("annotations: set embedding: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("annotations: no annotation found for session=%s turn=%d", sessionID, turnIndex)
	}
	return nil
}

// Count returns total annotation count.
func (s *Store) Count(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM turn_annotations`).Scan(&count); err != nil {
		return 0, fmt.Errorf("annotations: count: %w", err)
	}
	return count, nil
}

// MigrateSchema runs the annotation DDL migrations.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrateSchema(ctx, db)
}

func migrateSchema(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS turn_annotations (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    turn_index INTEGER NOT NULL,
    context_window_index INTEGER DEFAULT 0,
    byte_offset INTEGER NOT NULL,
    byte_length INTEGER NOT NULL,
    line_num INTEGER NOT NULL,
    timestamp TEXT,
    chunk_type TEXT NOT NULL,
    role TEXT NOT NULL,
    code_blocks TEXT,
    commands TEXT,
    errors TEXT,
    file_paths TEXT,
    symbols TEXT,
    tools_used TEXT,
    content_preview TEXT,
    content_hash TEXT NOT NULL,
    toc_label TEXT,
    toc_category TEXT,
    intent TEXT,
    annotation_model TEXT,
    annotation_version TEXT,
    embedding BLOB,
    embedding_model TEXT,
    embedding_text TEXT,
    has_error INTEGER DEFAULT 0,
    is_compact_boundary INTEGER DEFAULT 0,
    pre_compact_tokens INTEGER DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(session_id, turn_index)
);

CREATE INDEX IF NOT EXISTS idx_annotations_session ON turn_annotations(session_id);
CREATE INDEX IF NOT EXISTS idx_annotations_session_turn ON turn_annotations(session_id, turn_index);
CREATE INDEX IF NOT EXISTS idx_annotations_type ON turn_annotations(chunk_type);
CREATE INDEX IF NOT EXISTS idx_annotations_error ON turn_annotations(session_id) WHERE has_error = 1;
CREATE INDEX IF NOT EXISTS idx_annotations_hash ON turn_annotations(content_hash);
CREATE INDEX IF NOT EXISTS idx_annotations_category ON turn_annotations(toc_category);
CREATE INDEX IF NOT EXISTS idx_annotations_window ON turn_annotations(session_id, context_window_index);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("annotations: migrate: %w", err)
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanAnnotation(row scannable) (*TurnAnnotation, error) {
	var ann TurnAnnotation
	var timestamp, createdAt, updatedAt sql.NullString
	var codeBlocks, commands, errorsJSON sql.NullString
	var filePaths, symbols, toolsUsed sql.NullString
	var contentPreview, tocLabel, tocCategory sql.NullString
	var intent, annotationModel, annotationVersion sql.NullString
	var embeddingModel, embeddingText sql.NullString
	var hasError, isCompactBoundary int
	var contextWindowIndex, preCompactTokens sql.NullInt64

	err := row.Scan(
		&ann.ID, &ann.SessionID, &ann.TurnIndex, &contextWindowIndex,
		&ann.ByteOffset, &ann.ByteLength, &ann.LineNum, &timestamp,
		&ann.ChunkType, &ann.Role, &codeBlocks, &commands, &errorsJSON,
		&filePaths, &symbols, &toolsUsed, &contentPreview, &ann.ContentHash,
		&tocLabel, &tocCategory, &intent, &annotationModel, &annotationVersion,
		&ann.Embedding, &embeddingModel, &embeddingText,
		&hasError, &isCompactBoundary, &preCompactTokens,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("annotations: scan: %w", err)
	}

	ann.HasError = hasError == 1
	ann.IsCompactBoundary = isCompactBoundary == 1
	if contextWindowIndex.Valid {
		ann.ContextWindowIndex = int(contextWindowIndex.Int64)
	}
	if preCompactTokens.Valid {
		ann.PreCompactTokens = int(preCompactTokens.Int64)
	}
	if timestamp.Valid {
		ts, err := sqlutil.ScanTimestamp(timestamp.String)
		errs.Ignore(err, "parse annotations timestamp")
		ann.Timestamp = ts
	}
	if createdAt.Valid {
		ts, err := sqlutil.ScanTimestamp(createdAt.String)
		errs.Ignore(err, "parse annotations created_at")
		ann.CreatedAt = ts
	}
	if updatedAt.Valid {
		ts, err := sqlutil.ScanTimestamp(updatedAt.String)
		errs.Ignore(err, "parse annotations updated_at")
		ann.UpdatedAt = ts
	}
	if contentPreview.Valid {
		ann.ContentPreview = contentPreview.String
	}
	if tocLabel.Valid {
		ann.TOCLabel = tocLabel.String
	}
	if tocCategory.Valid {
		ann.TOCCategory = tocCategory.String
	}
	if intent.Valid {
		ann.Intent = intent.String
	}
	if annotationModel.Valid {
		ann.AnnotationModel = annotationModel.String
	}
	if annotationVersion.Valid {
		ann.AnnotationVersion = annotationVersion.String
	}
	if embeddingModel.Valid {
		ann.EmbeddingModel = embeddingModel.String
	}
	if embeddingText.Valid {
		ann.EmbeddingText = embeddingText.String
	}
	if codeBlocks.Valid {
		errs.Ignore(sqlutil.ScanJSON(codeBlocks.String, &ann.CodeBlocks), "parse code_blocks JSON")
	}
	if commands.Valid {
		errs.Ignore(sqlutil.ScanJSON(commands.String, &ann.Commands), "parse commands JSON")
	}
	if errorsJSON.Valid {
		errs.Ignore(sqlutil.ScanJSON(errorsJSON.String, &ann.Errors), "parse errors JSON")
	}
	if filePaths.Valid {
		errs.Ignore(sqlutil.ScanJSON(filePaths.String, &ann.FilePaths), "parse file_paths JSON")
	}
	if symbols.Valid {
		errs.Ignore(sqlutil.ScanJSON(symbols.String, &ann.Symbols), "parse symbols JSON")
	}
	if toolsUsed.Valid {
		errs.Ignore(sqlutil.ScanJSON(toolsUsed.String, &ann.ToolsUsed), "parse tools_used JSON")
	}

	return &ann, nil
}

func scanAnnotations(rows *sql.Rows) ([]*TurnAnnotation, error) {
	var anns []*TurnAnnotation
	for rows.Next() {
		ann, err := scanAnnotation(rows)
		if err != nil {
			return nil, err
		}
		anns = append(anns, ann)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("annotations: rows: %w", err)
	}
	return anns, nil
}

func buildSessionScopeCondition(sessionIDs []string, argIdx *int) (string, []any) {
	cleaned := make([]string, 0, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, id := range sessionIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	if len(cleaned) == 0 {
		return "", nil
	}

	placeholders := make([]string, 0, len(cleaned))
	args := make([]any, 0, len(cleaned))
	for _, id := range cleaned {
		*argIdx = *argIdx + 1
		placeholders = append(placeholders, fmt.Sprintf("$%d", *argIdx))
		args = append(args, id)
	}
	return "session_id IN (" + strings.Join(placeholders, ", ") + ")", args
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func resolveDBPath(dbPath string) (string, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get user home: %w", err)
		}
		return filepath.Join(home, ".foxctl", "storage", "annotations.db"), nil
	}
	if dbPath == "~" || strings.HasPrefix(dbPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get user home: %w", err)
		}
		if dbPath == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(dbPath, "~/")), nil
	}
	return dbPath, nil
}
