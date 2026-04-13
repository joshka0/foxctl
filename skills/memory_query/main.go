// Package main implements the memory/query skill for filtered memory access.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/mathutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

const (
	DefaultLimit         = 10
	DefaultMinSimilarity = 0.3
	DefaultTimeout       = 5 * time.Second
)

// Input defines the skill input parameters for memory query with filtering and search options.
type Input struct {
	Query          string  `json:"query,omitempty"`
	File           string  `json:"file,omitempty"`
	Types          string  `json:"types,omitempty"`
	Workspace      string  `json:"workspace,omitempty"`
	SessionID      string  `json:"session_id,omitempty"`
	Limit          int     `json:"limit,omitempty"`
	Offset         int     `json:"offset,omitempty"`
	MinSimilarity  float64 `json:"min_similarity,omitempty"`
	IncludeContent bool    `json:"include_content,omitempty"`
}

// Output defines the skill output with memory results, pagination, and query statistics.
type Output struct {
	Memories   []MemoryResult `json:"memories"`
	Pagination Pagination     `json:"pagination"`
	Stats      QueryStats     `json:"stats"`
}

// Pagination provides pagination metadata for memory query results with total count and navigation.
type Pagination struct {
	Total   int  `json:"total"`
	Offset  int  `json:"offset"`
	Limit   int  `json:"limit"`
	HasMore bool `json:"has_more"`
}

// MemoryResult represents a single memory entry in results with scoring and metadata.
type MemoryResult struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Summary   string  `json:"summary"`
	File      string  `json:"file,omitempty"`
	Score     float64 `json:"score"`
	CreatedAt string  `json:"created_at,omitempty"`
	UpdatedAt string  `json:"updated_at,omitempty"`
	SessionID string  `json:"session_id,omitempty"`
	Content   any     `json:"content,omitempty"`
}

// QueryStats provides query statistics with performance metrics and filter information.
type QueryStats struct {
	TotalFound      int    `json:"total_found"`
	Filtered        int    `json:"filtered"`
	SearchMethod    string `json:"search_method"`
	LatencyMS       int    `json:"latency_ms"`
	TypesFilter     string `json:"types_filter,omitempty"`
	FileFilter      string `json:"file_filter,omitempty"`
	SessionIDFilter string `json:"session_id_filter,omitempty"`
	Hint            string `json:"hint,omitempty"`
}

// main is the skill entry point for memory/query with filtered memory access capabilities.
func main() {
	skillmain.Main("memory/query", skillmain.Chain(run,
		skillmain.WithTimeout[Input](DefaultTimeout),
		skillmain.WithRecover[Input](),
	))
}

// run orchestrates memory query operations with validation, normalization, and result formatting.
//
// Index:
// - Purpose: Query memory entries with filtering, search, and pagination capabilities
// - Flow: validate input → normalize parameters → execute query → format results → emit output
// - SideEffects: opens memory store connection; performs vector search; reads memory entries
// - FailureModes: invalid input, memory store access failures, search errors, timeout issues
// - Observability: emits query results, pagination metadata, search statistics, and performance metrics
// - Related: normalizeInput, query, searchWithEmbeddings, isFileAssociated, extractFileFromEntry
// - Keywords: memory/query, memory_search, vector_search, filtering, pagination
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Validate: at least one search criteria must be provided
	if in.Query == "" && in.File == "" && in.Types == "" {
		return skillerr.Arg("at least one of query, file, or types must be provided", skillerr.WithHint("Provide query text, a file filter, or types."))
	}

	// Apply defaults
	normalizeInput(&in, rc)

	out, err := query(ctx, rc, &in)
	if err != nil {
		return err
	}

	return skillout.Emit(rc, "memory/query", out)
}

// normalizeInput applies default values and validation limits to input parameters with bounds checking.
func normalizeInput(in *Input, rc *skillmain.RunContext) {
	in.Limit = mathutil.DefaultPositiveInt(in.Limit, DefaultLimit)
	if in.Limit > 100 {
		in.Limit = 100
	}
	if in.Offset < 0 {
		in.Offset = 0
	}
	in.MinSimilarity = mathutil.DefaultPositiveFloat(in.MinSimilarity, DefaultMinSimilarity)
	if in.Workspace == "" {
		in.Workspace = rc.Workspace
	}
}

// query executes memory search with filtering, vector search fallback, and result pagination.
func query(ctx context.Context, rc *skillmain.RunContext, in *Input) (*Output, error) {
	start := time.Now()
	out := &Output{
		Memories: []MemoryResult{},
		Pagination: Pagination{
			Offset: in.Offset,
			Limit:  in.Limit,
		},
		Stats: QueryStats{
			TypesFilter:     in.Types,
			FileFilter:      in.File,
			SessionIDFilter: in.SessionID,
		},
	}

	var typeFilters []string
	if in.Types != "" {
		for _, t := range strings.Split(in.Types, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				typeFilters = append(typeFilters, t)
			}
		}
	}

	workspacePath := in.Workspace
	if absPath, err := filepath.Abs(workspacePath); err == nil {
		workspacePath = absPath
	}

	// TODO: Migrate to OpenWithConfig once ListFiltered is added to interface
	memStore, err := rc.Stores.Memory(ctx)
	if err != nil {
		return nil, skillerr.WrapIO("open memory store", err)
	}

	var scoredEntries []storage.ScoredEntry

	if in.Query == "" && in.File == "" && (strings.TrimSpace(in.SessionID) != "" || len(typeFilters) > 0) {
		entries, total, err := memStore.ListFiltered(ctx, workspacePath, memory.ListFilter{Types: typeFilters, SessionID: in.SessionID}, in.Limit, in.Offset)
		if err != nil {
			return nil, skillerr.WrapIO("list memories with filters", err)
		}

		out.Stats.SearchMethod = "filter"
		out.Stats.TotalFound = total
		out.Stats.Filtered = total
		out.Pagination.Total = total
		out.Pagination.HasMore = in.Offset+in.Limit < total

		for _, entry := range entries {
			result := MemoryResult{
				Name:      entry.Name,
				Type:      entry.Type,
				Summary:   skillout.TruncateString(entry.Summary, 500),
				Score:     1.0,
				SessionID: entry.SessionID,
			}
			if !entry.CreatedAt.IsZero() {
				result.CreatedAt = entry.CreatedAt.Format(time.RFC3339)
			}
			if !entry.UpdatedAt.IsZero() {
				result.UpdatedAt = entry.UpdatedAt.Format(time.RFC3339)
			}
			result.File = extractFileFromEntry(entry)
			out.Memories = append(out.Memories, result)
		}

		out.Stats.LatencyMS = int(time.Since(start).Milliseconds())
		if len(out.Memories) == 0 {
			out.Stats.Hint = "no memories match the filters"
		}
		return out, nil
	}

	if in.Query != "" {
		scoredEntries, err = searchWithEmbeddings(ctx, memStore, rc.Config, workspacePath, in, skillmain.EmbeddingGuard(rc))
		if err != nil {
			out.Stats.Hint = fmt.Sprintf("vector search failed: %v; using BM25", err)
			scoredEntries, err = memStore.Search(ctx, workspacePath, in.Query, in.Limit*3)
			if err != nil {
				return nil, skillerr.WrapIO("search memories", err)
			}
			out.Stats.SearchMethod = "bm25"
		} else {
			out.Stats.SearchMethod = "vector"
		}
	} else {
		entries, err := memStore.List(ctx, workspacePath, in.Limit*3)
		if err != nil {
			return nil, skillerr.WrapIO("list memories", err)
		}
		for _, entry := range entries {
			scoredEntries = append(scoredEntries, storage.ScoredEntry{
				Entry: entry,
				Score: 1.0,
			})
		}
		out.Stats.SearchMethod = "filter"
	}

	out.Stats.TotalFound = len(scoredEntries)

	filtered := make([]storage.ScoredEntry, 0, len(scoredEntries))
	for _, scored := range scoredEntries {
		entry := scored.Entry

		if entry.Type == "symbol" || entry.Type == "code_symbol" {
			continue
		}

		if len(typeFilters) > 0 {
			typeMatch := false
			for _, t := range typeFilters {
				if entry.Type == t {
					typeMatch = true
					break
				}
			}
			if !typeMatch {
				continue
			}
		}

		if strings.TrimSpace(in.SessionID) != "" && entry.SessionID != strings.TrimSpace(in.SessionID) {
			continue
		}

		if in.File != "" {
			if !isFileAssociated(entry, in.File) {
				continue
			}
		}

		if in.Query != "" && scored.Score < in.MinSimilarity {
			continue
		}

		filtered = append(filtered, scored)
	}

	out.Stats.Filtered = len(filtered)
	out.Pagination.Total = len(filtered)
	out.Pagination.HasMore = in.Offset+in.Limit < len(filtered)

	endIdx := in.Offset + in.Limit
	if endIdx > len(filtered) {
		endIdx = len(filtered)
	}
	if in.Offset < len(filtered) {
		filtered = filtered[in.Offset:endIdx]
	} else {
		filtered = nil
	}

	for _, scored := range filtered {
		entry := scored.Entry
		result := MemoryResult{
			Name:      entry.Name,
			Type:      entry.Type,
			Summary:   skillout.TruncateString(entry.Summary, 500),
			Score:     scored.Score,
			SessionID: entry.SessionID,
		}

		if !entry.CreatedAt.IsZero() {
			result.CreatedAt = entry.CreatedAt.Format(time.RFC3339)
		}
		if !entry.UpdatedAt.IsZero() {
			result.UpdatedAt = entry.UpdatedAt.Format(time.RFC3339)
		}

		result.File = extractFileFromEntry(entry)

		if in.IncludeContent && entry.Result != nil {
			var content any
			if json.Unmarshal(entry.Result, &content) == nil {
				result.Content = content
			}
		}

		out.Memories = append(out.Memories, result)
	}

	out.Stats.LatencyMS = int(time.Since(start).Milliseconds())

	if len(out.Memories) == 0 && out.Stats.Hint == "" {
		if in.Query != "" {
			out.Stats.Hint = "no matching memories found; try a broader query or different types"
		} else {
			out.Stats.Hint = "no memories match the filters; check types and file path"
		}
	}

	return out, nil
}

// searchWithEmbeddings performs vector similarity search using embeddings with query enrichment.
func searchWithEmbeddings(ctx context.Context, memStore *memory.Store, cfg config.Config, workspacePath string, in *Input, embedOpts ...semantic.EmbedderOption) ([]storage.ScoredEntry, error) {
	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeMemory, cfg, embedOpts...)
	if err != nil {
		return nil, skillerr.WrapRuntime("create embedder", err)
	}

	// Enrich query with temporal/type patterns for better matching
	enrichedQuery := semantic.EnrichQuery(in.Query)
	result, err := embedder.Embed(ctx, enrichedQuery)
	if err != nil {
		return nil, skillerr.WrapRuntime("generate query embedding", err)
	}

	results, err := memStore.SearchSimilar(ctx, workspacePath, result.Vec, in.Limit*3)
	if err != nil {
		return nil, skillerr.WrapIO("vector search", err)
	}

	return results, nil
}

// isFileAssociated checks if a memory entry is associated with a specific file path with flexible matching.
func isFileAssociated(entry storage.NamedEntry, filePath string) bool {
	filePath = normalizePath(filePath)

	if strings.Contains(strings.ToLower(entry.Name), strings.ToLower(filePath)) {
		return true
	}

	if strings.Contains(strings.ToLower(entry.Summary), strings.ToLower(filePath)) {
		return true
	}

	if entry.Result != nil {
		var data map[string]any
		if json.Unmarshal(entry.Result, &data) == nil {
			for _, key := range []string{"file", "path", "file_path", "filePath"} {
				if val, ok := data[key].(string); ok {
					if strings.Contains(strings.ToLower(val), strings.ToLower(filePath)) {
						return true
					}
				}
			}
		}
	}

	if strings.HasPrefix(strings.ToLower(filePath), strings.ToLower(normalizePath(entry.Name))) {
		return true
	}

	return false
}

// extractFileFromEntry extracts file path information from memory entry with multiple field detection.
func extractFileFromEntry(entry storage.NamedEntry) string {
	if strings.HasPrefix(entry.Name, "edit:") {
		parts := strings.SplitN(entry.Name, ":", 3)
		if len(parts) >= 2 {
			return parts[1]
		}
	}

	if entry.Result != nil {
		var data map[string]any
		if json.Unmarshal(entry.Result, &data) == nil {
			for _, key := range []string{"file", "path", "file_path", "filePath"} {
				if val, ok := data[key].(string); ok && val != "" {
					return val
				}
			}
		}
	}

	return ""
}

// normalizePath normalizes file paths by removing common prefixes for consistent comparison.
func normalizePath(p string) string {
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	return p
}
