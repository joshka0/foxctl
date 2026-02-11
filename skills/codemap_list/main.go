// Package main implements the codemap/list skill with pagination.
package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

const command = "codemap/list"

const (
	DefaultLimit          = 10
	DefaultMaxSummaryChar = 200
	DefaultTimeout        = 5 * time.Second
	MaxLimit              = 50
)

// Input is the expected JSON input for codemap/list operations.
type Input struct {
	Limit           int    `json:"limit,omitempty"`
	Offset          int    `json:"offset,omitempty"`
	Query           string `json:"query,omitempty"`
	SummaryOnly     *bool  `json:"summary_only,omitempty"`
	MaxSummaryChars int    `json:"max_summary_chars,omitempty"`
}

// Output contains paginated codemap listings with search capabilities.
type Output struct {
	Codemaps   []CodemapEntry `json:"codemaps"`
	Pagination Pagination     `json:"pagination"`
	Stats      Stats          `json:"stats"`
}

// CodemapEntry represents a codemap in the listing with metadata.
type CodemapEntry struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	CreatedAt  string `json:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	TraceCount int    `json:"trace_count,omitempty"`
}

// Pagination provides pagination metadata for codemap listings.
type Pagination struct {
	Total   int  `json:"total"`
	Offset  int  `json:"offset"`
	Limit   int  `json:"limit"`
	HasMore bool `json:"has_more"`
}

// Stats provides performance and method information for the operation.
type Stats struct {
	LatencyMS    int    `json:"latency_ms"`
	SearchMethod string `json:"search_method,omitempty"`
}

// main is the skill entry point for codemap/list.
func main() {
	skillmain.Main(command, run)
}

// run lists codemaps with pagination, search, and filtering capabilities.
//
// Index:
// - Purpose: List stored codemaps with pagination, optional semantic search, and summary truncation
// - Flow: apply defaults → open memory store → search (vector/filter) → paginate → extract metadata → build output
// - SideEffects: database queries; vector search; content truncation; metadata extraction
// - FailureModes: database errors, search errors, timeout errors, parse errors
// - Observability: emits paginated results with search method, timing metrics, and trace counts
// - Related: searchCodemaps, extractTitleFromName
// - Keywords: codemap/list, pagination, vector_search, filtering, metadata
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	if in.Limit <= 0 {
		in.Limit = DefaultLimit
	}
	if in.Limit > MaxLimit {
		in.Limit = MaxLimit
	}
	if in.Offset < 0 {
		in.Offset = 0
	}
	if in.MaxSummaryChars <= 0 {
		in.MaxSummaryChars = DefaultMaxSummaryChar
	}
	if in.SummaryOnly == nil {
		defaultTrue := true
		in.SummaryOnly = &defaultTrue
	}

	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	start := time.Now()
	out := &Output{
		Codemaps: []CodemapEntry{},
		Pagination: Pagination{
			Offset: in.Offset,
			Limit:  in.Limit,
		},
	}

	memStore, err := memory.OpenWithConfig(ctx, rc.Config)
	if err != nil {
		return skillerr.WrapIO("open memory store", err)
	}
	defer func() { errs.Ignore(memStore.Close(), "close memory store") }()

	workspaceID := workspace.ID(rc.Workspace)
	if workspaceID == "" {
		workspaceID = rc.Workspace
	}

	var codemapEntries []storage.ScoredEntry
	total := 0
	paged := false

	if in.Query != "" {
		codemapEntries, err = searchCodemaps(ctx, memStore, rc.Config, workspaceID, in.Query, in.Limit+in.Offset+10, skillmain.EmbeddingGuard(rc))
		if err != nil {
			entries, listTotal, listErr := memStore.ListFiltered(ctx, workspaceID, storage.MemoryListFilter{Types: []string{"codemap"}}, in.Limit, in.Offset)
			if listErr != nil {
				return skillerr.WrapIO("list memories", listErr)
			}
			total = listTotal
			paged = true
			for _, e := range entries {
				codemapEntries = append(codemapEntries, storage.ScoredEntry{Entry: e, Score: 1.0})
			}
			out.Stats.SearchMethod = "filter"
		} else {
			out.Stats.SearchMethod = "vector"
			total = len(codemapEntries)
		}
	} else {
		entries, listTotal, err := memStore.ListFiltered(ctx, workspaceID, storage.MemoryListFilter{Types: []string{"codemap"}}, in.Limit, in.Offset)
		if err != nil {
			return skillerr.WrapIO("list memories", err)
		}
		total = listTotal
		paged = true
		for _, e := range entries {
			codemapEntries = append(codemapEntries, storage.ScoredEntry{Entry: e, Score: 1.0})
		}
		out.Stats.SearchMethod = "filter"
	}

	out.Pagination.Total = total
	out.Pagination.HasMore = in.Offset+in.Limit < total

	if !paged {
		endIdx := in.Offset + in.Limit
		if endIdx > len(codemapEntries) {
			endIdx = len(codemapEntries)
		}
		if in.Offset < len(codemapEntries) {
			codemapEntries = codemapEntries[in.Offset:endIdx]
		} else {
			codemapEntries = nil
		}
	}

	for _, scored := range codemapEntries {
		entry := scored.Entry
		cm := CodemapEntry{
			ID:      entry.Name,
			Summary: skillout.TruncateRunes(entry.Summary, in.MaxSummaryChars),
		}

		if !entry.CreatedAt.IsZero() {
			cm.CreatedAt = entry.CreatedAt.Format(time.RFC3339)
		}
		if !entry.UpdatedAt.IsZero() {
			cm.UpdatedAt = entry.UpdatedAt.Format(time.RFC3339)
		}

		if entry.Result != nil {
			var data map[string]any
			if json.Unmarshal(entry.Result, &data) == nil {
				if title, ok := data["title"].(string); ok {
					cm.Title = title
				}
				if traces, ok := data["traces"].([]any); ok {
					cm.TraceCount = len(traces)
				}
			}
		}

		if cm.Title == "" {
			cm.Title = extractTitleFromName(entry.Name)
		}

		out.Codemaps = append(out.Codemaps, cm)
	}

	out.Stats.LatencyMS = int(time.Since(start).Milliseconds())

	return skillout.Emit(rc, command, out)
}

// searchCodemaps performs vector search for codemaps using semantic embeddings.
func searchCodemaps(ctx context.Context, memStore storage.MemoryStore, cfg config.Config, workspaceID, query string, limit int, embedOpts ...semantic.EmbedderOption) ([]storage.ScoredEntry, error) {
	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeCodemaps, cfg, embedOpts...)
	if err != nil {
		return nil, skillerr.WrapRuntime("create embedder", err)
	}

	result, err := embedder.Embed(ctx, query)
	if err != nil {
		return nil, skillerr.WrapRuntime("generate query embedding", err)
	}

	results, err := memStore.SearchSimilar(ctx, workspaceID, result.Vec, limit)
	if err != nil {
		return nil, skillerr.WrapIO("vector search", err)
	}

	filtered := make([]storage.ScoredEntry, 0, len(results))
	for _, r := range results {
		if r.Entry.Type == "codemap" {
			filtered = append(filtered, r)
		}
	}

	return filtered, nil
}

// extractTitleFromName extracts a readable title from codemap storage name.
func extractTitleFromName(name string) string {
	name = strings.TrimPrefix(name, "codemap://")
	runes := []rune(name)
	if len(runes) > 26 {
		return string(runes[:26])
	}
	return name
}
