package filesummary

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/intelligence/retrieval"
	"github.com/joshka0/foxctl/internal/storage"
)

// SearchFileSummaries searches file_summary entries using vector search when available,
// falling back to BM25. Results are filtered to file_summary entries only.
func SearchFileSummaries(
	ctx context.Context,
	store storage.MemoryStore,
	workspace string,
	query string,
	queryEmbedding []float32,
	limit int,
) ([]retrieval.FileEntry, error) {
	if limit <= 0 {
		limit = 20
	}

	oversample := limit * 4
	if oversample < 50 {
		oversample = 50
	}

	var scored []storage.ScoredEntry
	var err error
	var entries []retrieval.FileEntry

	if queryEmbedding != nil {
		scored, err = store.SearchSimilar(ctx, workspace, queryEmbedding, oversample)
		if err != nil {
			scored = nil
		}
		entries = filterFileSummaryEntries(scored, limit)
	}
	if len(entries) == 0 {
		scored, err = store.Search(ctx, workspace, query, oversample)
		if err != nil {
			return nil, err
		}
		entries = filterFileSummaryEntries(scored, limit)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		return entries[i].Path < entries[j].Path
	})

	return entries, nil
}

func filterFileSummaryEntries(scored []storage.ScoredEntry, limit int) []retrieval.FileEntry {
	entries := make([]retrieval.FileEntry, 0, limit)
	seen := make(map[string]bool)
	for _, s := range scored {
		entry := s.Entry
		if entry.Type != symbol.FileSummaryType && !strings.HasPrefix(entry.Name, "file://") {
			continue
		}
		filePath := extractFilePath(entry.Name)
		if filePath == "" {
			filePath = extractFilePathFromEntryPayload(entry.Result)
		}
		if filePath == "" || seen[filePath] {
			continue
		}
		seen[filePath] = true

		score := s.Score
		if score > 1.0 {
			score = 1.0
		}
		entries = append(entries, retrieval.FileEntry{
			Path:    filePath,
			Score:   score,
			Summary: entry.Summary,
		})
		if len(entries) >= limit {
			break
		}
	}
	return entries
}

func extractFilePath(entryName string) string {
	parts := strings.SplitN(entryName, "://", 2)
	if len(parts) != 2 {
		return ""
	}
	trimmed := parts[1]
	slash := strings.Index(trimmed, "/")
	if slash < 0 {
		return ""
	}
	trimmed = trimmed[slash+1:]
	nextSlash := strings.Index(trimmed, "/")
	if nextSlash < 0 {
		return ""
	}
	return trimmed[nextSlash+1:]
}

func extractFilePathFromEntryPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}

	var result symbol.FileSummaryResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return ""
	}
	return strings.TrimSpace(result.FilePath)
}
