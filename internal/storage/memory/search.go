package memory

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

// SearchableStore extends Store with advanced search capabilities
type SearchableStore struct {
	*Store
	db             dbdriver.DB
	hybridSearcher *dbdriver.HybridSearcher
	corpusStats    dbdriver.CorpusStats
}

// SearchResult represents a memory search result
type SearchResult struct {
	Entry       storage.NamedEntry
	Score       float64
	BM25Score   float64
	VectorScore float64
	Rank        int
}

// EnableSearch enables advanced search on an existing memory store
func (s *Store) EnableSearch(db dbdriver.DB, workspace string) (*SearchableStore, error) {
	// Build corpus statistics for the workspace
	ctx := context.Background()

	// Build corpus stats directly using the provided DB connection
	corpusStats, err := buildCorpusStatsForWorkspace(ctx, db, workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to build corpus stats: %w", err)
	}

	// Create hybrid searcher if vector search is available
	var hybridSearcher *dbdriver.HybridSearcher
	if db.IsVectorSearchEnabled() {
		options := dbdriver.DefaultHybridSearchOptions()
		hybridSearcher, err = dbdriver.NewHybridSearcher(db, corpusStats, options)
		if err != nil {
			return nil, fmt.Errorf("failed to create hybrid searcher: %w", err)
		}
	}

	return &SearchableStore{
		Store:          s,
		db:             db,
		hybridSearcher: hybridSearcher,
		corpusStats:    corpusStats,
	}, nil
}

// Search performs a search using the specified mode
func (ss *SearchableStore) Search(
	ctx context.Context,
	query string,
	queryVector dbdriver.Vector,
	workspace string,
	mode dbdriver.SearchMode,
	limit int,
) ([]SearchResult, error) {
	switch mode {
	case dbdriver.SearchModeBM25:
		return ss.searchBM25(ctx, query, workspace, limit)
	case dbdriver.SearchModeVector:
		return ss.searchVector(ctx, queryVector, workspace, limit)
	case dbdriver.SearchModeHybrid:
		return ss.searchHybrid(ctx, query, queryVector, workspace, limit)
	default:
		return nil, fmt.Errorf("unsupported search mode: %s", mode)
	}
}

// searchBM25 performs BM25 lexical search
func (ss *SearchableStore) searchBM25(
	ctx context.Context,
	query string,
	workspace string,
	limit int,
) ([]SearchResult, error) {
	// Get all documents in workspace
	listQuery := `
		SELECT id, name, type, workspace, summary, result, digests,
		       created_at, updated_at, last_accessed, access_count
		FROM named_memory
		WHERE workspace = ?
	`

	rows, err := ss.db.QueryContext(ctx, listQuery, workspace)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	// Score each document with BM25
	queryTerms := dbdriver.Tokenize(query)
	bm25Scorer := dbdriver.NewBM25Scorer(dbdriver.DefaultBM25Params(), ss.corpusStats)

	results := make([]SearchResult, 0)

	for rows.Next() {
		var entry storage.NamedEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.Name,
			&entry.Type,
			&entry.Workspace,
			&entry.Summary,
			&entry.Result,
			&entry.Digests,
			&entry.CreatedAt,
			&entry.UpdatedAt,
			&entry.LastAccess,
			&entry.AccessCount,
		); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		// Compute searchable text
		searchableText := entry.Name + " " + entry.Summary

		// Compute BM25 score
		termFreqs := dbdriver.ComputeTermFrequency(searchableText)
		docStats := dbdriver.DocumentStats{
			ID:        entry.ID,
			Length:    len(dbdriver.Tokenize(searchableText)),
			TermFreqs: termFreqs,
		}
		score := bm25Scorer.Score(queryTerms, docStats)

		if score > 0 {
			results = append(results, SearchResult{
				Entry:     entry,
				Score:     score,
				BM25Score: score,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration failed: %w", err)
	}

	// Sort by score and limit
	return sortAndLimit(results, limit), nil
}

// searchVector performs vector similarity search
func (ss *SearchableStore) searchVector(
	ctx context.Context,
	queryVector dbdriver.Vector,
	workspace string,
	limit int,
) ([]SearchResult, error) {
	if !ss.db.IsVectorSearchEnabled() {
		return nil, fmt.Errorf("vector search is not enabled")
	}

	vectorHelper, err := dbdriver.NewVectorHelper(ss.db)
	if err != nil {
		return nil, err
	}

	// Validate query vector
	if err := vectorHelper.ValidateVector(queryVector); err != nil {
		return nil, fmt.Errorf("invalid query vector: %w", err)
	}

	// Search using vector index
	searchQuery := fmt.Sprintf(`
		SELECT
			t.id, t.name, t.type, t.workspace, t.summary,
			t.result, t.digests, t.created_at, t.updated_at,
			t.last_accessed, t.access_count,
			%s as similarity
		FROM vector_top_k('idx_memory_vector', '%s', ?) vt
		JOIN named_memory t ON t.rowid = vt.id
		WHERE t.workspace = ?
		ORDER BY similarity DESC
	`, vectorHelper.CosineSimilarity("t.embedding", queryVector), queryVector.String())

	rows, err := ss.db.QueryContext(ctx, searchQuery, limit, workspace)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	results := make([]SearchResult, 0)

	for rows.Next() {
		var entry storage.NamedEntry
		var similarity float64

		if err := rows.Scan(
			&entry.ID,
			&entry.Name,
			&entry.Type,
			&entry.Workspace,
			&entry.Summary,
			&entry.Result,
			&entry.Digests,
			&entry.CreatedAt,
			&entry.UpdatedAt,
			&entry.LastAccess,
			&entry.AccessCount,
			&similarity,
		); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		results = append(results, SearchResult{
			Entry:       entry,
			Score:       similarity,
			VectorScore: similarity,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration failed: %w", err)
	}

	return results, nil
}

// searchHybrid performs hybrid search combining BM25 and vector
func (ss *SearchableStore) searchHybrid(
	ctx context.Context,
	query string,
	queryVector dbdriver.Vector,
	workspace string,
	limit int,
) ([]SearchResult, error) {
	if ss.hybridSearcher == nil {
		return nil, fmt.Errorf("hybrid search is not available (requires vector search)")
	}

	// Use hybrid searcher
	rawResults, err := ss.hybridSearcher.Search(
		ctx,
		query,
		queryVector,
		"named_memory",
		"name",                           // Text column expression
		"embedding",                            // Vector column
		"idx_memory_vector",                    // Index name
		limit,
		"workspace = ?", // Additional filter
		workspace,
	)
	if err != nil {
		return nil, err
	}

	// Convert to memory search results
	results := make([]SearchResult, 0, len(rawResults))

	for _, result := range rawResults {
		// Fetch full entry
		entry, err := ss.Get(ctx, result.DocumentID, workspace)
		if err != nil {
			if err == ErrNotFound {
				continue
			}
			return nil, fmt.Errorf("failed to fetch entry %s: %w", result.DocumentID, err)
		}

		results = append(results, SearchResult{
			Entry:       entry,
			Score:       result.Score,
			BM25Score:   result.BM25Score,
			VectorScore: result.VectorScore,
		})
	}

	return results, nil
}

// SearchBM25 is a convenience method for BM25 search
func (ss *SearchableStore) SearchBM25(
	ctx context.Context,
	query string,
	workspace string,
	limit int,
) ([]SearchResult, error) {
	return ss.Search(ctx, query, nil, workspace, dbdriver.SearchModeBM25, limit)
}

// SearchVector is a convenience method for vector search
func (ss *SearchableStore) SearchVector(
	ctx context.Context,
	queryVector dbdriver.Vector,
	workspace string,
	limit int,
) ([]SearchResult, error) {
	return ss.Search(ctx, "", queryVector, workspace, dbdriver.SearchModeVector, limit)
}

// SearchHybrid is a convenience method for hybrid search
func (ss *SearchableStore) SearchHybrid(
	ctx context.Context,
	query string,
	queryVector dbdriver.Vector,
	workspace string,
	limit int,
) ([]SearchResult, error) {
	return ss.Search(ctx, query, queryVector, workspace, dbdriver.SearchModeHybrid, limit)
}

// sortAndLimit sorts results by score and returns top k
func sortAndLimit(results []SearchResult, limit int) []SearchResult {
	// Sort by score descending
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Add ranks
	for i := range results {
		results[i].Rank = i + 1
	}

	// Limit results
	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}

	return results
}

// Get retrieves a memory by ID (helper method)
func (ss *SearchableStore) Get(ctx context.Context, id, workspace string) (storage.NamedEntry, error) {
	query := `
		SELECT id, name, type, workspace, summary, result, digests,
		       created_at, updated_at, last_accessed, access_count
		FROM named_memory
		WHERE id = ? AND workspace = ?
	`

	var entry storage.NamedEntry
	err := ss.db.QueryRowContext(ctx, query, id, workspace).Scan(
		&entry.ID,
		&entry.Name,
		&entry.Type,
		&entry.Workspace,
		&entry.Summary,
		&entry.Result,
		&entry.Digests,
		&entry.CreatedAt,
		&entry.UpdatedAt,
		&entry.LastAccess,
		&entry.AccessCount,
	)

	if err == sql.ErrNoRows {
		return entry, ErrNotFound
	}

	if err != nil {
		return entry, fmt.Errorf("failed to get entry: %w", err)
	}

	return entry, nil
}

// RefreshCorpusStats rebuilds corpus statistics (call after bulk updates)
func (ss *SearchableStore) RefreshCorpusStats(ctx context.Context, workspace string) error {
	stats, err := buildCorpusStatsForWorkspace(ctx, ss.db, workspace)
	if err != nil {
		return err
	}

	ss.corpusStats = stats

	// Recreate hybrid searcher if available
	if ss.db.IsVectorSearchEnabled() {
		options := dbdriver.DefaultHybridSearchOptions()
		hybridSearcher, err := dbdriver.NewHybridSearcher(ss.db, stats, options)
		if err != nil {
			return fmt.Errorf("failed to recreate hybrid searcher: %w", err)
		}
		ss.hybridSearcher = hybridSearcher
	}

	return nil
}

// buildCorpusStatsForWorkspace builds corpus stats for a specific workspace
func buildCorpusStatsForWorkspace(
	ctx context.Context,
	db dbdriver.DB,
	workspace string,
) (dbdriver.CorpusStats, error) {
	stats := dbdriver.CorpusStats{
		DocFreqs: make(map[string]int),
	}

	// Count documents in workspace
	var totalDocs int
	countQuery := "SELECT COUNT(*) FROM named_memory WHERE workspace = ?"
	if err := db.QueryRowContext(ctx, countQuery, workspace).Scan(&totalDocs); err != nil {
		return stats, fmt.Errorf("count query failed: %w", err)
	}
	stats.TotalDocs = totalDocs

	if totalDocs == 0 {
		return stats, nil
	}

	// Get all documents
	query := "SELECT name, COALESCE(summary, '') as summary FROM named_memory WHERE workspace = ?"
	rows, err := db.QueryContext(ctx, query, workspace)
	if err != nil {
		return stats, fmt.Errorf("text query failed: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	totalTokens := 0
	docFreqs := make(map[string]map[string]bool) // term -> set of doc IDs

	docID := 0
	for rows.Next() {
		var name, summary string
		if err := rows.Scan(&name, &summary); err != nil {
			return stats, fmt.Errorf("scan failed: %w", err)
		}

		searchableText := name + " " + summary
		terms := dbdriver.Tokenize(searchableText)
		totalTokens += len(terms)

		// Track unique terms
		uniqueTerms := make(map[string]bool)
		for _, term := range terms {
			uniqueTerms[term] = true
		}

		for term := range uniqueTerms {
			if docFreqs[term] == nil {
				docFreqs[term] = make(map[string]bool)
			}
			docFreqs[term][fmt.Sprintf("%d", docID)] = true
		}

		docID++
	}

	if err := rows.Err(); err != nil {
		return stats, fmt.Errorf("row iteration failed: %w", err)
	}

	// Finalize stats
	for term, docs := range docFreqs {
		stats.DocFreqs[term] = len(docs)
	}

	stats.AvgDocLength = float64(totalTokens) / float64(totalDocs)

	return stats, nil
}
