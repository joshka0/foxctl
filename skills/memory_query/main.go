// Package main implements the memory/query skill for filtered memory access.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

const (
	DefaultLimit         = 10
	DefaultMinSimilarity = 0.3
	DefaultTimeout       = 5 * time.Second
)

// Input defines the skill input parameters.
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

// Output defines the skill output.
type Output struct {
	Memories   []MemoryResult `json:"memories"`
	Pagination Pagination     `json:"pagination"`
	Stats      QueryStats     `json:"stats"`
}

// Pagination provides pagination metadata.
type Pagination struct {
	Total   int  `json:"total"`
	Offset  int  `json:"offset"`
	Limit   int  `json:"limit"`
	HasMore bool `json:"has_more"`
}

// MemoryResult represents a single memory entry in results.
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

// QueryStats provides query statistics.
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

func main() {
	skillmain.Main("memory/query", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Validate: at least one search criteria must be provided
	if in.Query == "" && in.File == "" && in.Types == "" {
		return fmt.Errorf("at least one of query, file, or types must be provided")
	}

	// Apply defaults
	normalizeInput(&in, rc)

	out, err := query(ctx, rc, &in)
	if err != nil {
		return err
	}

	return skillout.Emit(rc, "memory/query", out)
}

func normalizeInput(in *Input, rc *skillmain.RunContext) {
	if in.Limit <= 0 {
		in.Limit = DefaultLimit
	}
	if in.Limit > 100 {
		in.Limit = 100
	}
	if in.Offset < 0 {
		in.Offset = 0
	}
	if in.MinSimilarity <= 0 {
		in.MinSimilarity = DefaultMinSimilarity
	}
	if in.Workspace == "" {
		in.Workspace = rc.Workspace
	}
}

func query(ctx context.Context, rc *skillmain.RunContext, in *Input) (*Output, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

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

	agentctlHome := os.Getenv("AGENTCTL_HOME")
	if agentctlHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine home directory: %w", err)
		}
		agentctlHome = filepath.Join(home, ".agentctl")
	}
	storageRoot := filepath.Join(agentctlHome, "storage")
	casRoot := filepath.Join(agentctlHome, "cas")

	workspacePath := in.Workspace
	if absPath, err := filepath.Abs(workspacePath); err == nil {
		workspacePath = absPath
	}

	memStore, err := memory.Open(ctx, storageRoot, casRoot)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	defer func() { errs.Ignore(memStore.Close(), "close memory store") }()

	var scoredEntries []storage.ScoredEntry

	if in.Query == "" && in.File == "" && (strings.TrimSpace(in.SessionID) != "" || len(typeFilters) > 0) {
		entries, total, err := memStore.ListFiltered(ctx, workspacePath, memory.ListFilter{Types: typeFilters, SessionID: in.SessionID}, in.Limit, in.Offset)
		if err != nil {
			return nil, fmt.Errorf("list memories with filters: %w", err)
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
				Summary:   truncate(entry.Summary, 500),
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
		scoredEntries, err = searchWithEmbeddings(ctx, memStore, workspacePath, in)
		if err != nil {
			out.Stats.Hint = fmt.Sprintf("vector search failed: %v; using BM25", err)
			scoredEntries, err = memStore.Search(ctx, workspacePath, in.Query, in.Limit*3)
			if err != nil {
				return nil, fmt.Errorf("search memories: %w", err)
			}
			out.Stats.SearchMethod = "bm25"
		} else {
			out.Stats.SearchMethod = "vector"
		}
	} else {
		entries, err := memStore.List(ctx, workspacePath, in.Limit*3)
		if err != nil {
			return nil, fmt.Errorf("list memories: %w", err)
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
			Summary:   truncate(entry.Summary, 500),
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

func searchWithEmbeddings(ctx context.Context, memStore *memory.Store, workspacePath string, in *Input) ([]storage.ScoredEntry, error) {
	embedder, err := semantic.NewEmbedder(semantic.ScopeMemory)
	if err != nil {
		return nil, fmt.Errorf("create embedder: %w", err)
	}

	result, err := embedder.Embed(ctx, in.Query)
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}

	results, err := memStore.SearchSimilar(ctx, workspacePath, result.Vec, in.Limit*3)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	return results, nil
}

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

func normalizePath(p string) string {
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	return p
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
