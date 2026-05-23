package searchindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/searchquery"
	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

const defaultSearchIndexLimit = 20

var errInvalidDocument = errors.New("searchindex: invalid document")

// Open creates or opens a SQL-backed search index and applies migrations.
func Open(ctx context.Context, root string) (Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "SEARCHINDEX", "searchindex.db", MigrateSchema)
	if err != nil {
		return nil, fmt.Errorf("searchindex: open: %w", err)
	}
	return &sqlStore{db: db, close: closeFn}, nil
}

// MigrateSchema applies the search index schema.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	m := sqlutil.NewMigrator(db)
	m.Add(1, "create search_documents", `
CREATE TABLE IF NOT EXISTS search_documents (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    scope TEXT NOT NULL,
    kind TEXT NOT NULL,
    group_key TEXT NOT NULL,
    path TEXT NOT NULL,
    symbol_id TEXT,
    symbol_name TEXT,
    title TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    search_text TEXT NOT NULL DEFAULT '',
    keywords_json TEXT NOT NULL DEFAULT '[]',
    anchor_json TEXT NOT NULL DEFAULT '{}',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    embedding_json TEXT,
    embedding_model TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_search_documents_workspace ON search_documents(workspace_id);
CREATE INDEX IF NOT EXISTS idx_search_documents_scope_kind ON search_documents(scope, kind);
CREATE INDEX IF NOT EXISTS idx_search_documents_group_key ON search_documents(workspace_id, group_key);
CREATE INDEX IF NOT EXISTS idx_search_documents_path ON search_documents(path);
CREATE INDEX IF NOT EXISTS idx_search_documents_symbol ON search_documents(workspace_id, kind, symbol_id);
CREATE INDEX IF NOT EXISTS idx_search_documents_updated_at ON search_documents(workspace_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS search_embedding_metadata (
    workspace_id TEXT PRIMARY KEY,
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_search_embedding_metadata_workspace ON search_embedding_metadata(workspace_id);
`)
	return m.Migrate(ctx)
}

type sqlStore struct {
	db    *sql.DB
	close func() error
}

var _ Store = (*sqlStore)(nil)

// Close releases database resources.
func (s *sqlStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// Upsert stores a single document.
func (s *sqlStore) Upsert(ctx context.Context, doc Document) error {
	_, err := s.upsertMany(ctx, []Document{doc})
	return err
}

// upsertMany stores documents in order.
func (s *sqlStore) upsertMany(ctx context.Context, docs []Document) (int, error) {
	if len(docs) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("searchindex: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	count := 0
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO search_documents (
    id, workspace_id, scope, kind, group_key, path,
    symbol_id, symbol_name, title, summary, search_text,
    keywords_json, anchor_json, metadata_json,
    embedding_json, embedding_model, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14,
    $15, $16, $17, $18
)
ON CONFLICT(id) DO UPDATE SET
    workspace_id = excluded.workspace_id,
    scope = excluded.scope,
    kind = excluded.kind,
    group_key = excluded.group_key,
    path = excluded.path,
    symbol_id = excluded.symbol_id,
    symbol_name = excluded.symbol_name,
    title = excluded.title,
    summary = excluded.summary,
    search_text = excluded.search_text,
    keywords_json = excluded.keywords_json,
    anchor_json = excluded.anchor_json,
    metadata_json = excluded.metadata_json,
    embedding_json = excluded.embedding_json,
    embedding_model = excluded.embedding_model,
    updated_at = excluded.updated_at
`)
	if err != nil {
		return 0, fmt.Errorf("searchindex: prepare upsert: %w", err)
	}
	defer func() {
		_ = stmt.Close()
	}()

	for _, doc := range docs {
		prepared := normalizeDocument(doc)
		if prepared.ID == "" || prepared.WorkspaceID == "" {
			return count, errInvalidDocument
		}
		if len(prepared.Embedding) > 0 {
			if prepared.EmbeddingModel == "" {
				return count, fmt.Errorf("searchindex: embedding model required when embedding is present")
			}
			if err := validateOrPersistEmbeddingMetadataTx(ctx, tx, prepared.WorkspaceID, prepared.EmbeddingModel, len(prepared.Embedding)); err != nil {
				return count, err
			}
		}

		if prepared.SearchText == "" {
			prepared.SearchText = encodeSearchText(prepared.Title, prepared.Summary, prepared.Path)
		}

		keywordsJSON, err := encodeJSON(prepared.Keywords)
		if err != nil {
			return count, fmt.Errorf("searchindex: encode keywords: %w", err)
		}
		anchorJSON, err := encodeJSON(prepared.Anchor)
		if err != nil {
			return count, fmt.Errorf("searchindex: encode anchor: %w", err)
		}
		metadataJSON, err := encodeJSON(prepared.Metadata)
		if err != nil {
			return count, fmt.Errorf("searchindex: encode metadata: %w", err)
		}
		embeddingJSON, err := encodeJSON(prepared.Embedding)
		if err != nil {
			return count, fmt.Errorf("searchindex: encode embedding: %w", err)
		}

		now := timeutil.FormatRFC3339Nano(timeutil.NowUTC())
		_, err = stmt.ExecContext(
			ctx,
			prepared.ID,
			prepared.WorkspaceID,
			prepared.Scope,
			prepared.Kind,
			prepared.GroupKey,
			prepared.Path,
			emptyString(prepared.SymbolID),
			emptyString(prepared.SymbolName),
			emptyString(prepared.Title),
			emptyString(prepared.Summary),
			prepared.SearchText,
			keywordsJSON,
			anchorJSON,
			metadataJSON,
			embeddingJSON,
			emptyString(prepared.EmbeddingModel),
			now,
			now,
		)
		if err != nil {
			return count, fmt.Errorf("searchindex: upsert %q: %w", prepared.ID, err)
		}
		count++
	}

	err = tx.Commit()
	if err != nil {
		return 0, fmt.Errorf("searchindex: commit upsert: %w", err)
	}
	return count, nil
}

// Delete removes a document by ID.
func (s *sqlStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM search_documents WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("searchindex: delete: %w", err)
	}
	return nil
}

// DeleteWorkspace removes all documents for a workspace.
func (s *sqlStore) DeleteWorkspace(ctx context.Context, workspaceID string) error {
	workspaceID = workspace.CanonicalID(workspaceID)
	_, err := s.db.ExecContext(ctx, `DELETE FROM search_documents WHERE workspace_id = $1`, workspaceID)
	if err != nil {
		return fmt.Errorf("searchindex: delete workspace: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM search_embedding_metadata WHERE workspace_id = $1`, workspaceID); err != nil {
		return fmt.Errorf("searchindex: delete workspace metadata: %w", err)
	}
	return nil
}

// CountWorkspace returns the number of persisted documents for a workspace.
func (s *sqlStore) CountWorkspace(ctx context.Context, workspaceID string) (int, error) {
	workspaceID = workspace.CanonicalID(workspaceID)
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_documents WHERE workspace_id = $1`, workspaceID).Scan(&count); err != nil {
		return 0, fmt.Errorf("searchindex: count workspace: %w", err)
	}
	return count, nil
}

// WorkspaceStats returns persisted retrieval corpus stats for a workspace.
func (s *sqlStore) WorkspaceStats(ctx context.Context, workspaceID string) (WorkspaceStats, error) {
	workspaceID = workspace.CanonicalID(workspaceID)
	stats := WorkspaceStats{WorkspaceID: workspaceID}
	if err := s.db.QueryRowContext(ctx, `
SELECT
	COUNT(*),
	COALESCE(SUM(CASE WHEN embedding_json IS NOT NULL AND embedding_json != '' AND embedding_json != 'null' THEN 1 ELSE 0 END), 0)
FROM search_documents
WHERE workspace_id = $1
`, workspaceID).Scan(&stats.DocumentCount, &stats.EmbeddedCount); err != nil {
		return WorkspaceStats{}, fmt.Errorf("searchindex: workspace stats: %w", err)
	}
	meta, err := s.GetEmbeddingMetadata(ctx, workspaceID)
	if err != nil {
		return WorkspaceStats{}, err
	}
	stats.EmbeddingMetadata = meta
	return stats, nil
}

// GetEmbeddingMetadata returns the persisted embedding contract for a workspace.
func (s *sqlStore) GetEmbeddingMetadata(ctx context.Context, workspaceID string) (*EmbeddingMetadata, error) {
	workspaceID = workspace.CanonicalID(workspaceID)
	var meta EmbeddingMetadata
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `
SELECT workspace_id, model, dimensions, created_at, updated_at
FROM search_embedding_metadata
WHERE workspace_id = $1
`, workspaceID).Scan(&meta.WorkspaceID, &meta.Model, &meta.Dimensions, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("searchindex: get embedding metadata: %w", err)
	}
	return &meta, nil
}

// ValidateEmbeddingMetadata checks model and dimensions for a workspace.
func (s *sqlStore) ValidateEmbeddingMetadata(ctx context.Context, workspaceID, model string, dimensions int) error {
	workspaceID = workspace.CanonicalID(workspaceID)
	model = stringsTrimSpace(model)
	meta, err := s.GetEmbeddingMetadata(ctx, workspaceID)
	if err != nil {
		return err
	}
	if meta == nil {
		return nil
	}
	if dimensions > 0 && meta.Dimensions != dimensions {
		return fmt.Errorf("searchindex: embedding dimension mismatch: workspace %q expects model %q with %d dimensions, got %d; run `foxctl index init --workspace <workspace-path> --scope symbols` to rebuild symbol/search embeddings", workspaceID, meta.Model, meta.Dimensions, dimensions)
	}
	if model != "" && meta.Model != "" && meta.Model != model {
		return fmt.Errorf("searchindex: embedding model mismatch: workspace %q expects model %q with %d dimensions, got model %q; run `foxctl index init --workspace <workspace-path> --scope symbols` to rebuild symbol/search embeddings", workspaceID, meta.Model, meta.Dimensions, model)
	}
	return nil
}

// LexicalRecall performs in-process lexical recall scoring.
func (s *sqlStore) LexicalRecall(ctx context.Context, workspaceID, query string, opts RecallOptions) ([]SearchHit, error) {
	workspaceID = workspace.CanonicalID(workspaceID)
	terms := splitSearchTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultSearchIndexLimit
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, scope, kind, group_key, path, symbol_id, symbol_name, title, summary,
       search_text, keywords_json, anchor_json, metadata_json, embedding_json, embedding_model, updated_at
FROM search_documents
WHERE workspace_id = $1`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("searchindex: lexical query: %w", err)
	}
	defer rows.Close()

	results := make([]scoredHit, 0)
	for rows.Next() {
		doc, updatedAt, err := scanDocRow(rows)
		if err != nil {
			return nil, err
		}

		score := lexicalScore(doc.SearchText, terms)
		if score <= 0 {
			continue
		}
		if opts.MinScore > 0 && score < opts.MinScore {
			continue
		}
		results = append(results, scoredHit{Doc: doc, Score: score, UpdatedAt: updatedAt})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("searchindex: lexical rows: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].UpdatedAt.After(results[j].UpdatedAt)
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return toSearchHits(results), nil
}

// ExactRecall performs in-process exact-match oriented recall scoring.
func (s *sqlStore) ExactRecall(ctx context.Context, workspaceID, query string, opts ExactRecallOptions) ([]SearchHit, error) {
	workspaceID = workspace.CanonicalID(workspaceID)
	plan := searchquery.ParseQuery(query)
	if len(plan.Identifiers) == 0 && len(plan.PathHints) == 0 && strings.TrimSpace(plan.Raw) == "" {
		return nil, nil
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultSearchIndexLimit
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, scope, kind, group_key, path, symbol_id, symbol_name, title, summary,
       search_text, keywords_json, anchor_json, metadata_json, embedding_json, embedding_model, updated_at
FROM search_documents
WHERE workspace_id = $1`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("searchindex: exact query: %w", err)
	}
	defer rows.Close()

	results := make([]scoredHit, 0)
	for rows.Next() {
		doc, updatedAt, err := scanDocRow(rows)
		if err != nil {
			return nil, err
		}
		score := exactScore(doc, plan)
		if score <= 0 {
			continue
		}
		results = append(results, scoredHit{Doc: doc, Score: score, UpdatedAt: updatedAt})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("searchindex: exact rows: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].UpdatedAt.After(results[j].UpdatedAt)
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return toSearchHits(results), nil
}

// VectorRecall performs in-process cosine similarity over stored embeddings.
func (s *sqlStore) VectorRecall(ctx context.Context, workspaceID string, embedding []float32, opts VectorRecallOptions) ([]SearchHit, error) {
	workspaceID = workspace.CanonicalID(workspaceID)
	if len(embedding) == 0 {
		return nil, errors.New("searchindex: vector recall requires a non-empty embedding")
	}
	if err := s.ValidateEmbeddingMetadata(ctx, workspaceID, opts.EmbeddingModel, len(embedding)); err != nil {
		return nil, err
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultSearchIndexLimit
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, scope, kind, group_key, path, symbol_id, symbol_name, title, summary,
       search_text, keywords_json, anchor_json, metadata_json, embedding_json, embedding_model, updated_at
FROM search_documents
WHERE workspace_id = $1 AND embedding_json IS NOT NULL AND embedding_json != ''`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("searchindex: vector query: %w", err)
	}
	defer rows.Close()

	results := make([]scoredHit, 0)
	for rows.Next() {
		doc, updatedAt, err := scanDocRow(rows)
		if err != nil {
			return nil, err
		}
		if opts.EmbeddingModel != "" && doc.EmbeddingModel != "" && doc.EmbeddingModel != opts.EmbeddingModel {
			continue
		}

		score := cosineSimilarity(embedding, doc.Embedding)
		if score <= 0 {
			continue
		}
		if opts.MinScore > 0 && score < opts.MinScore {
			continue
		}
		results = append(results, scoredHit{Doc: doc, Score: score, UpdatedAt: updatedAt})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("searchindex: vector rows: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].UpdatedAt.After(results[j].UpdatedAt)
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return toSearchHits(results), nil
}

// GetEmbeddingsByIDs returns exact embeddings for the given document IDs.
// IDs without a stored embedding are silently omitted.
func (s *sqlStore) GetEmbeddingsByIDs(ctx context.Context, ids []string) (map[string][]float32, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build parameterised placeholders: ($1, $2, ...)
	args := make([]any, len(ids))
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(
		"SELECT id, embedding_json FROM search_documents WHERE id IN (%s) AND embedding_json IS NOT NULL AND embedding_json != ''",
		strings.Join(placeholders, ", "),
	)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("searchindex: get embeddings by ids: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]float32, len(ids))
	for rows.Next() {
		var id, embeddingJSON string
		if err := rows.Scan(&id, &embeddingJSON); err != nil {
			return nil, fmt.Errorf("searchindex: scan embedding: %w", err)
		}
		var emb []float32
		if err := json.Unmarshal([]byte(embeddingJSON), &emb); err != nil {
			continue // skip malformed
		}
		result[id] = emb
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("searchindex: get embeddings rows: %w", err)
	}
	return result, nil
}

type scoredHit struct {
	Doc       Document
	Score     float64
	UpdatedAt time.Time
}

func toSearchHits(hits []scoredHit) []SearchHit {
	out := make([]SearchHit, 0, len(hits))
	for _, hit := range hits {
		out = append(out, SearchHit{Doc: hit.Doc, Score: hit.Score})
	}
	return out
}

func scanDocRow(s interface{ Scan(args ...any) error }) (Document, time.Time, error) {
	var (
		doc            Document
		path           sql.NullString
		symbolID       sql.NullString
		symbolName     sql.NullString
		embeddingModel sql.NullString
		keywordsJSON   string
		anchorJSON     string
		metadataJSON   string
		embeddingJSON  string
		updatedAtRaw   string
	)

	err := s.Scan(
		&doc.ID,
		&doc.WorkspaceID,
		&doc.Scope,
		&doc.Kind,
		&doc.GroupKey,
		&path,
		&symbolID,
		&symbolName,
		&doc.Title,
		&doc.Summary,
		&doc.SearchText,
		&keywordsJSON,
		&anchorJSON,
		&metadataJSON,
		&embeddingJSON,
		&embeddingModel,
		&updatedAtRaw,
	)
	if err != nil {
		return Document{}, time.Time{}, fmt.Errorf("searchindex: scan doc: %w", err)
	}

	if path.Valid {
		doc.Path = path.String
	}
	if symbolID.Valid {
		doc.SymbolID = symbolID.String
	}
	if symbolName.Valid {
		doc.SymbolName = symbolName.String
	}
	if embeddingModel.Valid {
		doc.EmbeddingModel = embeddingModel.String
	}

	if keywordsJSON != "" {
		if err := json.Unmarshal([]byte(keywordsJSON), &doc.Keywords); err != nil {
			return Document{}, time.Time{}, fmt.Errorf("searchindex: unmarshal keywords: %w", err)
		}
	}
	if anchorJSON != "" {
		if err := json.Unmarshal([]byte(anchorJSON), &doc.Anchor); err != nil {
			return Document{}, time.Time{}, fmt.Errorf("searchindex: unmarshal anchor: %w", err)
		}
	}
	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &doc.Metadata); err != nil {
			return Document{}, time.Time{}, fmt.Errorf("searchindex: unmarshal metadata: %w", err)
		}
	}
	if embeddingJSON != "" {
		if err := json.Unmarshal([]byte(embeddingJSON), &doc.Embedding); err != nil {
			return Document{}, time.Time{}, fmt.Errorf("searchindex: unmarshal embedding: %w", err)
		}
	}

	updatedAt, err := timeutil.ParseRFC3339Nano(updatedAtRaw)
	if err != nil {
		return Document{}, time.Time{}, fmt.Errorf("searchindex: parse updated_at %q: %w", updatedAtRaw, err)
	}

	doc.WorkspaceID = workspace.CanonicalID(doc.WorkspaceID)
	doc.Keywords = normalizeKeywords(doc.Keywords)
	return doc, updatedAt, nil
}

func normalizeDocument(doc Document) Document {
	doc.WorkspaceID = workspace.CanonicalID(doc.WorkspaceID)
	if doc.Scope == "" {
		doc.Scope = ScopeCode
	}
	if doc.Kind == "" {
		doc.Kind = KindText
	}
	doc.Title = stringsTrimSpace(doc.Title)
	doc.Path = stringsTrimSpace(doc.Path)
	doc.GroupKey = stringsTrimSpace(doc.GroupKey)
	doc.SearchText = stringsTrimSpace(doc.SearchText)
	doc.Summary = stringsTrimSpace(doc.Summary)
	doc.SymbolID = stringsTrimSpace(doc.SymbolID)
	doc.SymbolName = stringsTrimSpace(doc.SymbolName)
	doc.EmbeddingModel = stringsTrimSpace(doc.EmbeddingModel)
	doc.Keywords = normalizeKeywords(doc.Keywords)

	return doc
}

func validateOrPersistEmbeddingMetadataTx(ctx context.Context, tx *sql.Tx, workspaceID, model string, dimensions int) error {
	workspaceID = workspace.CanonicalID(workspaceID)
	model = stringsTrimSpace(model)
	if workspaceID == "" || model == "" || dimensions <= 0 {
		return nil
	}
	var existingModel string
	var existingDims int
	err := tx.QueryRowContext(ctx, `
SELECT model, dimensions FROM search_embedding_metadata WHERE workspace_id = $1
`, workspaceID).Scan(&existingModel, &existingDims)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		now := timeutil.FormatRFC3339Nano(timeutil.NowUTC())
		_, err = tx.ExecContext(ctx, `
INSERT INTO search_embedding_metadata (workspace_id, model, dimensions, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
`, workspaceID, model, dimensions, now, now)
		if err != nil {
			return fmt.Errorf("searchindex: insert embedding metadata: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("searchindex: read embedding metadata: %w", err)
	}
	if existingDims != dimensions {
		return fmt.Errorf("searchindex: embedding dimension mismatch: workspace %q expects model %q with %d dimensions, got %d; run `foxctl index init --workspace <workspace-path> --scope symbols` to rebuild symbol/search embeddings", workspaceID, existingModel, existingDims, dimensions)
	}
	if existingModel != "" && existingModel != model {
		return fmt.Errorf("searchindex: embedding model mismatch: workspace %q expects model %q with %d dimensions, got model %q; run `foxctl index init --workspace <workspace-path> --scope symbols` to rebuild symbol/search embeddings", workspaceID, existingModel, existingDims, model)
	}
	return nil
}

func lexicalScore(haystack string, terms []string) float64 {
	haystack = strings.ToLower(strings.TrimSpace(haystack))
	score := 0.0
	for _, term := range terms {
		t := strings.ToLower(strings.TrimSpace(term))
		if t == "" {
			continue
		}
		count := float64(strings.Count(haystack, t))
		score += count
		if strings.Contains(haystack, t) {
			score += 0.5
		}
	}
	return score
}

func exactScore(doc Document, plan searchquery.QueryPlan) float64 {
	score := 0.0
	pathLower := strings.ToLower(strings.TrimSpace(doc.Path))
	baseLower := strings.ToLower(filepath.Base(strings.TrimSpace(doc.Path)))
	titleLower := strings.ToLower(strings.TrimSpace(doc.Title))
	symbolLower := strings.ToLower(strings.TrimSpace(doc.SymbolName))
	rawLower := strings.ToLower(strings.TrimSpace(plan.Raw))
	structuralPlan := isStructuralExactPlan(plan)

	for _, id := range plan.Identifiers {
		v := strings.ToLower(strings.TrimSpace(id.Value))
		if v == "" {
			continue
		}
		if structuralPlan && isGenericStructuralIdentifier(v) {
			continue
		}
		switch {
		case symbolLower != "" && symbolLower == v:
			score = maxFloat64(score, 1.0)
		case titleLower != "" && titleLower == v:
			score = maxFloat64(score, 0.95)
		case pathLower != "" && (pathLower == v || baseLower == v):
			score = maxFloat64(score, 0.9)
		case symbolLower != "" && strings.HasSuffix(symbolLower, v):
			score = maxFloat64(score, 0.85)
		}
	}

	for _, hint := range plan.PathHints {
		v := strings.ToLower(strings.TrimSpace(hint.Path))
		if v == "" {
			continue
		}
		switch {
		case pathLower == v || baseLower == v:
			score = maxFloat64(score, 0.95)
		case strings.Contains(pathLower, v):
			score = maxFloat64(score, 0.8)
		}
	}

	if rawLower != "" {
		switch {
		case symbolLower != "" && symbolLower == rawLower:
			score = maxFloat64(score, 1.0)
		case titleLower != "" && titleLower == rawLower:
			score = maxFloat64(score, 0.95)
		case pathLower != "" && (pathLower == rawLower || baseLower == rawLower):
			score = maxFloat64(score, 0.9)
		}
	}
	return score
}

func isStructuralExactPlan(plan searchquery.QueryPlan) bool {
	q := strings.ToLower(strings.TrimSpace(plan.Raw))
	if q == "" {
		return false
	}
	for _, token := range structuralExactTokens {
		if strings.Contains(q, token) {
			return true
		}
	}
	return false
}

var structuralExactTokens = []string{
	"call", "calls", "caller", "callee", "flow", "graph", "dag", "expand",
	"relationship", "chain", "refers", "imports", "depends", "used by", "grep",
	"repo index", "repoindex",
}

func isGenericStructuralIdentifier(v string) bool {
	switch v {
	case "dag", "graph", "flow", "call", "calls", "grep", "edge", "edges", "repo", "index", "expand":
		return true
	default:
		return false
	}
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func cosineSimilarity(query, doc []float32) float64 {
	if len(query) == 0 || len(doc) == 0 || len(query) != len(doc) {
		return 0
	}

	dot := float64(0)
	normQ := float64(0)
	normD := float64(0)
	for i := range query {
		a := float64(query[i])
		b := float64(doc[i])
		dot += a * b
		normQ += a * a
		normD += b * b
	}
	if normQ == 0 || normD == 0 {
		return 0
	}
	return dot / (math.Sqrt(normQ) * math.Sqrt(normD))
}

func stringsTrimSpace(value string) string {
	return strings.TrimSpace(value)
}

func emptyString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
