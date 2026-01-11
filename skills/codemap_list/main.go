// Package main implements the codemap/list skill with pagination.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
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

type Input struct {
	Limit           int    `json:"limit,omitempty"`
	Offset          int    `json:"offset,omitempty"`
	Query           string `json:"query,omitempty"`
	SummaryOnly     *bool  `json:"summary_only,omitempty"`
	MaxSummaryChars int    `json:"max_summary_chars,omitempty"`
}

type Output struct {
	Codemaps   []CodemapEntry `json:"codemaps"`
	Pagination Pagination     `json:"pagination"`
	Stats      Stats          `json:"stats"`
}

type CodemapEntry struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	CreatedAt  string `json:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	TraceCount int    `json:"trace_count,omitempty"`
}

type Pagination struct {
	Total   int  `json:"total"`
	Offset  int  `json:"offset"`
	Limit   int  `json:"limit"`
	HasMore bool `json:"has_more"`
}

type Stats struct {
	LatencyMS    int    `json:"latency_ms"`
	SearchMethod string `json:"search_method,omitempty"`
}

func main() {
	skillmain.Main(command, run)
}

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

	memStore, err := memory.Open(ctx, rc.Config.Storage.Root, rc.Config.Paths.CAS)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer func() { errs.Ignore(memStore.Close(), "close memory store") }()

	var allEntries []storage.ScoredEntry

	if in.Query != "" {
		allEntries, err = searchCodemaps(ctx, memStore, rc.Workspace, in.Query, in.Limit+in.Offset+10)
		if err != nil {
			entries, listErr := memStore.List(ctx, rc.Workspace, 500)
			if listErr != nil {
				return fmt.Errorf("list memories: %w", listErr)
			}
			for _, e := range entries {
				if e.Type == "codemap" {
					allEntries = append(allEntries, storage.ScoredEntry{Entry: e, Score: 1.0})
				}
			}
			out.Stats.SearchMethod = "filter"
		} else {
			out.Stats.SearchMethod = "vector"
		}
	} else {
		entries, err := memStore.List(ctx, rc.Workspace, 500)
		if err != nil {
			return fmt.Errorf("list memories: %w", err)
		}
		for _, e := range entries {
			if e.Type == "codemap" {
				allEntries = append(allEntries, storage.ScoredEntry{Entry: e, Score: 1.0})
			}
		}
		out.Stats.SearchMethod = "filter"
	}

	codemapEntries := make([]storage.ScoredEntry, 0, len(allEntries))
	for _, scored := range allEntries {
		if scored.Entry.Type == "codemap" {
			codemapEntries = append(codemapEntries, scored)
		}
	}

	out.Pagination.Total = len(codemapEntries)
	out.Pagination.HasMore = in.Offset+in.Limit < len(codemapEntries)

	endIdx := in.Offset + in.Limit
	if endIdx > len(codemapEntries) {
		endIdx = len(codemapEntries)
	}
	if in.Offset < len(codemapEntries) {
		codemapEntries = codemapEntries[in.Offset:endIdx]
	} else {
		codemapEntries = nil
	}

	for _, scored := range codemapEntries {
		entry := scored.Entry
		cm := CodemapEntry{
			ID:      entry.Name,
			Summary: truncate(entry.Summary, in.MaxSummaryChars),
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

func searchCodemaps(ctx context.Context, memStore *memory.Store, workspacePath, query string, limit int) ([]storage.ScoredEntry, error) {
	embedder, err := semantic.NewEmbedder(semantic.ScopeCodemaps)
	if err != nil {
		return nil, fmt.Errorf("create embedder: %w", err)
	}

	result, err := embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}

	results, err := memStore.SearchSimilar(ctx, workspacePath, result.Vec, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	filtered := make([]storage.ScoredEntry, 0, len(results))
	for _, r := range results {
		if r.Entry.Type == "codemap" {
			filtered = append(filtered, r)
		}
	}

	return filtered, nil
}

func extractTitleFromName(name string) string {
	name = strings.TrimPrefix(name, "codemap://")
	runes := []rune(name)
	if len(runes) > 26 {
		return string(runes[:26])
	}
	return name
}

func truncate(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
