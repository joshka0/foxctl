package retrieval

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// searchSemanticIndex searches the semantic index using vector similarity.
// Falls back to BM25 if vector search is not available.
func (g *Generator) searchSemanticIndex(ctx context.Context, workspaceID, question string, limit int) ([]Candidate, error) {
	if g.embedProvider == nil {
		g.logger.Debug().Msg("no embedding provider, skipping semantic search")
		return []Candidate{}, nil
	}

	// Check if we have a SearchableStore for advanced search
	if g.searchableStore == nil {
		g.logger.Debug().Msg("no searchable store, trying BM25 fallback")
		return g.semanticBM25Fallback(ctx, workspaceID, question, limit)
	}

	searchable := g.searchableStore

	// Get embedding for the question
	vec, err := g.embedProvider.Embed(ctx, question)
	if err != nil {
		g.logger.Warn().Err(err).Msg("embedding failed, falling back to BM25")
		return g.semanticBM25Fallback(ctx, workspaceID, question, limit)
	}

	// Convert to dbdriver.Vector
	queryVec := dbdriver.Vector(vec)

	// Try hybrid search (BM25 + Vector)
	results, err := searchable.SearchHybrid(ctx, question, queryVec, workspaceID, limit*2)
	if err != nil {
		g.logger.Debug().Err(err).Msg("hybrid search failed, trying BM25")
		// Fall back to BM25 only
		results, err = searchable.SearchBM25(ctx, question, workspaceID, limit*2)
		if err != nil {
			return nil, err
		}
	}

	// Convert to candidates
	return g.memoryResultsToCandidates(results, limit)
}

// semanticBM25Fallback uses BM25 search when vector search is unavailable.
func (g *Generator) semanticBM25Fallback(ctx context.Context, workspaceID, question string, limit int) ([]Candidate, error) {
	// Use regular store search (simpler BM25-like)
	results, err := g.store.Search(ctx, workspaceID, question, limit*2)
	if err != nil {
		return nil, err
	}

	// Convert to candidates, filtering for file entries
	candidates := make([]Candidate, 0, len(results))
	seen := make(map[string]bool)

	for _, r := range results {
		// Extract file path from entry name
		filePath := extractFilePath(r.Entry.Name)
		if filePath == "" {
			continue
		}

		// Deduplicate by file path
		if seen[filePath] {
			continue
		}
		seen[filePath] = true

		// Normalize score (store.Search returns raw scores)
		score := r.Score
		if score > 1.0 {
			score = 1.0 // Cap at 1.0 for consistency
		}

		candidates = append(candidates, Candidate{
			Path:     filePath,
			Score:    score,
			RawScore: r.Score,
			Source:   SourceSemantic,
		})

		if len(candidates) >= limit {
			break
		}
	}

	return candidates, nil
}

// memoryResultsToCandidates converts memory search results to candidates.
func (g *Generator) memoryResultsToCandidates(results []memory.SearchResult, limit int) ([]Candidate, error) {
	candidates := make([]Candidate, 0, len(results))
	seen := make(map[string]bool)

	for _, r := range results {
		// Extract file path from entry name
		filePath := extractFilePath(r.Entry.Name)
		if filePath == "" {
			continue
		}

		// Deduplicate by file path
		if seen[filePath] {
			continue
		}
		seen[filePath] = true

		// Score is already normalized in hybrid search
		score := r.Score
		if score > 1.0 {
			score = 1.0
		}

		candidates = append(candidates, Candidate{
			Path:     filePath,
			Score:    score,
			RawScore: r.Score,
			Source:   SourceSemantic,
		})

		if len(candidates) >= limit {
			break
		}
	}

	return candidates, nil
}

// extractFilePath extracts a file path from a named memory entry name.
// Entry names follow patterns like:
//   - "symbol://<workspace>/<file_path>:<symbol_name>"
//   - "file://<workspace>/<file_path>"
//   - "semantic://<workspace>/<file_path>"
func extractFilePath(name string) string {
	// Handle symbol:// format
	if strings.HasPrefix(name, "symbol://") {
		// Format: symbol://<workspace>/<file_path>:<symbol_name>
		rest := strings.TrimPrefix(name, "symbol://")
		// Skip workspace
		if idx := strings.Index(rest, "/"); idx != -1 {
			rest = rest[idx+1:]
		}
		// Remove symbol name
		if idx := strings.LastIndex(rest, ":"); idx != -1 {
			rest = rest[:idx]
		}
		return rest
	}

	// Handle file:// or semantic:// format
	for _, prefix := range []string{"file://", "semantic://", "embed://"} {
		if strings.HasPrefix(name, prefix) {
			rest := strings.TrimPrefix(name, prefix)
			// Skip workspace
			if idx := strings.Index(rest, "/"); idx != -1 {
				rest = rest[idx+1:]
			}
			return rest
		}
	}

	// Check if it looks like a file path
	if strings.Contains(name, "/") && isCodeFile(name) {
		// Might be a raw path
		return name
	}

	return ""
}

// isCodeFile returns true if the path looks like a code file.
func isCodeFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".rs", ".java", ".c", ".cpp", ".h", ".hpp",
		".rb", ".php", ".swift", ".kt", ".scala", ".cs", ".gd":
		return true
	}
	return false
}
