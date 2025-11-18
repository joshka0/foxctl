package dbdriver

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
)

// SearchMode defines the type of search to perform
type SearchMode string

const (
	// SearchModeBM25 uses lexical/keyword search
	SearchModeBM25 SearchMode = "bm25"
	// SearchModeVector uses semantic vector similarity
	SearchModeVector SearchMode = "vector"
	// SearchModeHybrid combines BM25 and vector search
	SearchModeHybrid SearchMode = "hybrid"
)

// BM25Params holds BM25 hyperparameters
type BM25Params struct {
	K1 float64 // Term frequency saturation (typical: 1.2-2.0)
	B  float64 // Length normalization (typical: 0.75)
}

// DefaultBM25Params returns typical BM25 parameters
func DefaultBM25Params() BM25Params {
	return BM25Params{
		K1: 1.5,
		B:  0.75,
	}
}

// TermFrequency represents term frequency in a document
type TermFrequency map[string]int

// DocumentStats holds statistics about a document
type DocumentStats struct {
	ID        string
	Length    int // Token count
	TermFreqs TermFrequency
	AvgLength float64 // Average document length in corpus
}

// CorpusStats holds statistics about the entire corpus
type CorpusStats struct {
	TotalDocs    int
	AvgDocLength float64
	DocFreqs     map[string]int // Number of docs containing each term
}

// SearchResult represents a single search result
type SearchResult struct {
	DocumentID  string
	Score       float64
	BM25Score   float64
	VectorScore float64
	Metadata    map[string]interface{}
}

// SearchResults is a collection of search results
type SearchResults []SearchResult

// Sort sorts results by score descending
func (r SearchResults) Sort() {
	sort.Slice(r, func(i, j int) bool {
		return r[i].Score > r[j].Score
	})
}

// TopK returns the top k results
func (r SearchResults) TopK(k int) SearchResults {
	if k < 0 {
		k = 0
	}
	if k > len(r) {
		k = len(r)
	}
	return r[:k]
}

// BM25Scorer computes BM25 scores for documents
type BM25Scorer struct {
	params      BM25Params
	corpusStats CorpusStats
}

// NewBM25Scorer creates a new BM25 scorer
func NewBM25Scorer(params BM25Params, corpusStats CorpusStats) *BM25Scorer {
	return &BM25Scorer{
		params:      params,
		corpusStats: corpusStats,
	}
}

// ComputeIDF computes the IDF (inverse document frequency) for a term
func (b *BM25Scorer) ComputeIDF(term string) float64 {
	N := float64(b.corpusStats.TotalDocs)
	nt := float64(b.corpusStats.DocFreqs[term])

	if nt == 0 {
		return 0.0
	}

	// IDF(t) = ln((N - nt + 0.5) / (nt + 0.5) + 1)
	return math.Log((N-nt+0.5)/(nt+0.5) + 1.0)
}

// Score computes BM25 score for a document given query terms
func (b *BM25Scorer) Score(queryTerms []string, docStats DocumentStats) float64 {
	score := 0.0
	k1 := b.params.K1
	bParam := b.params.B

	// Use corpus average if not provided in doc stats
	avgdl := b.corpusStats.AvgDocLength
	if docStats.AvgLength > 0 {
		avgdl = docStats.AvgLength
	}

	// Process each unique query term
	seen := make(map[string]bool)
	for _, term := range queryTerms {
		if seen[term] {
			continue
		}
		seen[term] = true

		// Get term frequency in document
		ftd := float64(docStats.TermFreqs[term])
		if ftd == 0 {
			continue
		}

		// Compute IDF
		idf := b.ComputeIDF(term)

		// Compute BM25 term contribution
		// BM25 = IDF(t) * (f(t,d) * (k1 + 1)) / (f(t,d) + k1 * (1 - b + b * |d|/avgdl))
		norm := 1.0 - bParam + bParam*(float64(docStats.Length)/avgdl)
		denom := ftd + k1*norm
		termScore := idf * (ftd * (k1 + 1.0)) / denom

		score += termScore
	}

	return score
}

// Tokenize splits text into search terms (simple whitespace tokenizer)
func Tokenize(text string) []string {
	// Convert to lowercase and split on whitespace
	text = strings.ToLower(text)
	terms := strings.Fields(text)

	// Remove common punctuation
	cleaned := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.Trim(term, ".,!?;:()[]{}\"'")
		if len(term) > 0 {
			cleaned = append(cleaned, term)
		}
	}

	return cleaned
}

// ComputeTermFrequency computes term frequency map from text
func ComputeTermFrequency(text string) TermFrequency {
	terms := Tokenize(text)
	freqs := make(TermFrequency)

	for _, term := range terms {
		freqs[term]++
	}

	return freqs
}

// MinMaxScaler rescales values to [0, 1] range
type MinMaxScaler struct {
	min float64
	max float64
}

// NewMinMaxScaler creates a scaler from a set of values
func NewMinMaxScaler(values []float64) *MinMaxScaler {
	if len(values) == 0 {
		return &MinMaxScaler{0, 1}
	}

	minVal := values[0]
	maxVal := values[0]

	for _, v := range values {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	return &MinMaxScaler{min: minVal, max: maxVal}
}

// Scale rescales a value to [0, 1]
func (s *MinMaxScaler) Scale(x float64) float64 {
	if s.max == s.min {
		return 0.5
	}
	return (x - s.min) / (s.max - s.min)
}

// HybridSearcher combines BM25 and vector search
type HybridSearcher struct {
	db           DB
	vectorHelper *VectorHelper
	bm25Scorer   *BM25Scorer
	alpha        float64 // Weight for BM25 (0-1), 1-alpha for vector
}

// HybridSearchOptions configures hybrid search
type HybridSearchOptions struct {
	Alpha      float64    // BM25 weight (default: 0.5)
	BM25Params BM25Params // BM25 parameters
	Limit      int        // Max results
}

// DefaultHybridSearchOptions returns default hybrid search options
func DefaultHybridSearchOptions() HybridSearchOptions {
	return HybridSearchOptions{
		Alpha:      0.5,
		BM25Params: DefaultBM25Params(),
		Limit:      10,
	}
}

// NewHybridSearcher creates a hybrid searcher
func NewHybridSearcher(db DB, corpusStats CorpusStats, options HybridSearchOptions) (*HybridSearcher, error) {
	if !db.IsVectorSearchEnabled() {
		return nil, fmt.Errorf("vector search must be enabled for hybrid search")
	}

	vectorHelper, err := NewVectorHelper(db)
	if err != nil {
		return nil, err
	}

	bm25Scorer := NewBM25Scorer(options.BM25Params, corpusStats)

	return &HybridSearcher{
		db:           db,
		vectorHelper: vectorHelper,
		bm25Scorer:   bm25Scorer,
		alpha:        options.Alpha,
	}, nil
}

// Search performs hybrid search combining BM25 and vector similarity
func (h *HybridSearcher) Search(
	ctx context.Context,
	queryText string,
	queryVector Vector,
	tableName string,
	textColumn string,
	vectorColumn string,
	indexName string,
	limit int,
	additionalWhere string,
	args ...any,
) (SearchResults, error) {
	// Phase 1: Get candidate documents using vector search
	// This gives us a reasonable subset to score with BM25
	candidateQuery := fmt.Sprintf(`
		SELECT
			t.id as id,
			t.%s as text,
			%s as vector_sim
		FROM %s vt
		JOIN %s t ON t.rowid = vt.id
	`, textColumn,
		h.vectorHelper.CosineSimilarity("t."+vectorColumn, queryVector),
		h.vectorHelper.VectorTopK(indexName, queryVector, limit*2), // Get 2x candidates
		tableName)

	if additionalWhere != "" {
		candidateQuery += " WHERE " + additionalWhere
	}

	rows, err := h.db.QueryContext(ctx, candidateQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("candidate query failed: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	// Phase 2: Compute BM25 scores for candidates
	queryTerms := Tokenize(queryText)
	results := make(SearchResults, 0)
	bm25Scores := make([]float64, 0)

	for rows.Next() {
		var docID string
		var text string
		var vectorSim float64

		if err := rows.Scan(&docID, &text, &vectorSim); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		// Compute BM25 score
		termFreqs := ComputeTermFrequency(text)
		docStats := DocumentStats{
			ID:        docID,
			Length:    len(Tokenize(text)),
			TermFreqs: termFreqs,
		}
		bm25Score := h.bm25Scorer.Score(queryTerms, docStats)

		results = append(results, SearchResult{
			DocumentID:  docID,
			BM25Score:   bm25Score,
			VectorScore: vectorSim,
		})

		bm25Scores = append(bm25Scores, bm25Score)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration failed: %w", err)
	}

	// Phase 3: Normalize and combine scores
	bm25Scaler := NewMinMaxScaler(bm25Scores)

	for i := range results {
		// Scale BM25 to [0, 1]
		bm25Scaled := bm25Scaler.Scale(results[i].BM25Score)

		// Scale cosine similarity from [-1, 1] to [0, 1]
		vectorScaled := (results[i].VectorScore + 1.0) / 2.0

		// Combine with alpha weighting
		results[i].Score = h.alpha*bm25Scaled + (1.0-h.alpha)*vectorScaled
	}

	// Phase 4: Sort and return top-k
	results.Sort()
	return results.TopK(limit), nil
}

// BuildCorpusStats computes corpus statistics from a table
func BuildCorpusStats(ctx context.Context, db DB, tableName, textColumn string) (CorpusStats, error) {
	stats := CorpusStats{
		DocFreqs: make(map[string]int),
	}

	// Count total documents
	var totalDocs int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	if err := db.QueryRowContext(ctx, countQuery).Scan(&totalDocs); err != nil {
		return stats, fmt.Errorf("count query failed: %w", err)
	}
	stats.TotalDocs = totalDocs

	if totalDocs == 0 {
		return stats, nil
	}

	// Compute average document length and document frequencies
	query := fmt.Sprintf("SELECT %s FROM %s", textColumn, tableName)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return stats, fmt.Errorf("text query failed: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	totalTokens := 0
	seenTerms := make(map[string]map[string]bool) // term -> set of doc IDs

	docID := 0
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return stats, fmt.Errorf("scan failed: %w", err)
		}

		terms := Tokenize(text)
		totalTokens += len(terms)

		// Track which documents contain which terms
		uniqueTerms := make(map[string]bool)
		for _, term := range terms {
			uniqueTerms[term] = true
		}

		for term := range uniqueTerms {
			if seenTerms[term] == nil {
				seenTerms[term] = make(map[string]bool)
			}
			seenTerms[term][fmt.Sprintf("%d", docID)] = true
		}

		docID++
	}

	if err := rows.Err(); err != nil {
		return stats, fmt.Errorf("row iteration failed: %w", err)
	}

	// Compute document frequencies
	for term, docs := range seenTerms {
		stats.DocFreqs[term] = len(docs)
	}

	// Compute average document length
	stats.AvgDocLength = float64(totalTokens) / float64(totalDocs)

	return stats, nil
}

// FullTextSearchHelper provides FTS5 integration for SQLite/Turso
type FullTextSearchHelper struct {
	db DB
}

// NewFullTextSearchHelper creates a helper for full-text search
func NewFullTextSearchHelper(db DB) *FullTextSearchHelper {
	return &FullTextSearchHelper{db: db}
}

// CreateFTS5Table creates an FTS5 virtual table
func (f *FullTextSearchHelper) CreateFTS5Table(
	ctx context.Context,
	tableName string,
	columns []string,
) error {
	query := fmt.Sprintf(
		"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(%s)",
		tableName,
		strings.Join(columns, ", "),
	)

	_, err := f.db.ExecContext(ctx, query)
	return err
}

// InsertIntoFTS5 inserts a document into an FTS5 table
func (f *FullTextSearchHelper) InsertIntoFTS5(
	ctx context.Context,
	tableName string,
	values map[string]string,
) error {
	columns := make([]string, 0, len(values))
	placeholders := make([]string, 0, len(values))
	args := make([]interface{}, 0, len(values))

	for col, val := range values {
		columns = append(columns, col)
		placeholders = append(placeholders, "?")
		args = append(args, val)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	_, err := f.db.ExecContext(ctx, query, args...)
	return err
}

// SearchFTS5 performs a full-text search using FTS5
func (f *FullTextSearchHelper) SearchFTS5(
	ctx context.Context,
	tableName string,
	query string,
	limit int,
) (*sql.Rows, error) {
	searchQuery := fmt.Sprintf(
		"SELECT *, rank FROM %s WHERE %s MATCH ? ORDER BY rank LIMIT ?",
		tableName, tableName,
	)

	return f.db.QueryContext(ctx, searchQuery, query, limit)
}
