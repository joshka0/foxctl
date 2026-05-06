// Package main implements the fs/find skill.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	fshelpers "github.com/joshka0/foxctl/internal/adapters/skillslib/fs"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/sliceutil"
)

const command = "fs/find"

// input is the skill input schema for fs/find operations.
type input struct {
	Query         string `json:"query"`
	Path          string `json:"path"`
	Pattern       string `json:"pattern"`
	Type          string `json:"type"`
	MinSize       int64  `json:"min_size"`
	MaxSize       int64  `json:"max_size"`
	ModifiedSince string `json:"modified_since"`
	MaxDepth      int    `json:"max_depth"`
	Hidden        bool   `json:"hidden"`
	SortBy        string `json:"sort_by"`
	MaxResults    int    `json:"max_results"`
}

// fileResult represents a found file or directory with metadata.
type fileResult struct {
	Path         string   `json:"path"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Size         int64    `json:"size"`
	Modified     string   `json:"modified"`
	ModifiedUnix int64    `json:"modified_unix"`
	Score        float64  `json:"score,omitempty"`
	Matches      []string `json:"matches,omitempty"`
}

// main is the skill entry point for fs/find.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates file system search with filtering, fuzzy matching, and result persistence.
//
// Index:
//   Purpose: Find files and directories with advanced filtering, fuzzy search, and result ranking
//   Flow: validate input → resolve path → walk directory → apply filters → score results → sort → emit results
//   SideEffects: directory traversal; file system access; CAS storage for large result sets
//   FailureModes: invalid paths, permission errors, time filter parsing errors
//   Observability: emits search results with scores, match types, and artifact hints for large sets
//   Related: parseTimeFilter, fuzzyScore, sortResults
//   Keywords: fs/find, file_search, fuzzy_matching, directory_traversal, filtering
//
// [[domain:file-system-search]]
// [[protocol:fuzzy-file-ranking]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.Type == "" {
		in.Type = "file"
	}
	if in.SortBy == "" {
		in.SortBy = "relevance"
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 100
	}

	// Resolve workspace and search path
	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	// Parse time filter
	var modifiedSince time.Time
	if in.ModifiedSince != "" {
		parsed, err := parseTimeFilter(in.ModifiedSince)
		if err != nil {
			return err
		}
		modifiedSince = parsed
	}

	// Walk directory tree and collect matching files
	var results []fileResult
	err = filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // Skip files/dirs we can't read
		}

		// Skip symlinks to avoid cycles and inconsistent behavior
		if fsutil.IsSymlinkMode(d.Type()) {
			return nil
		}

		// Check depth limit
		if in.MaxDepth > 0 {
			depth := strings.Count(pathutil.RelTo(searchPath, path), "/")
			if depth > in.MaxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip hidden files/directories
		if fshelpers.ShouldSkipHidden(d.Name(), in.Hidden) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip common directories
		if d.IsDir() && fsutil.IsCommonExclude(d.Name()) {
			return filepath.SkipDir
		}

		// Type filter
		if in.Type == "file" && d.IsDir() {
			return nil
		}
		if in.Type == "directory" && !d.IsDir() {
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Size filters
		if in.MinSize > 0 && info.Size() < in.MinSize {
			return nil
		}
		if in.MaxSize > 0 && info.Size() > in.MaxSize {
			return nil
		}

		// Modified time filter
		if !modifiedSince.IsZero() && info.ModTime().Before(modifiedSince) {
			return nil
		}

		// Pattern matching
		if in.Pattern != "" {
			matched, err := filepath.Match(in.Pattern, d.Name())
			if err != nil || !matched {
				return nil
			}
		}

		// Create result
		relPath := pathutil.RelTo(workspace, path)
		result := fileResult{
			Path:         relPath,
			Name:         d.Name(),
			Type:         getFileType(d),
			Size:         info.Size(),
			Modified:     info.ModTime().Format(time.RFC3339),
			ModifiedUnix: info.ModTime().Unix(),
		}

		// Fuzzy scoring if query provided
		if in.Query != "" {
			score, matches := fuzzyScore(in.Query, relPath, d.Name())
			if score > 0 {
				result.Score = score
				result.Matches = matches
				results = append(results, result)
			}
		} else {
			results = append(results, result)
		}

		return nil
	})
	if err != nil {
		return skillerr.WrapIO("directory walk failed", err)
	}

	// Sort results
	sortResults(results, in.SortBy)

	// Limit results
	results = sliceutil.Limit(results, in.MaxResults)

	// Prepare preview and persist full results if truncated
	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, results, rc.MaxPreview, "fs_find", true)
	if err != nil {
		return skillerr.WrapIO("preview and persist results", err)
	}

	// Build response
	data := map[string]any{
		"result_count": previewResult.Total,
		"preview":      previewResult.Preview,
		"sort_by":      in.SortBy,
		"max_results":  in.MaxResults,
	}
	if in.Query != "" {
		data["query"] = in.Query
	}
	skillout.AddArtifact(data, previewResult.Artifact)

	return skillout.Emit(rc, command, data)
}

// parseTimeFilter converts time filter strings to time.Time.
func parseTimeFilter(since string) (time.Time, error) {
	now := time.Now()
	hint := "Use Nd, Nw, Nm, or Ny (e.g., \"7d\")."

	if strings.HasSuffix(since, "d") {
		days := strings.TrimSuffix(since, "d")
		var d int
		if _, err := fmt.Sscanf(days, "%d", &d); err != nil {
			return time.Time{}, skillerr.WrapValidation("modified_since must be in form Nd, Nw, Nm, or Ny", err, skillerr.WithHint(hint))
		}
		return now.AddDate(0, 0, -d), nil
	}
	if strings.HasSuffix(since, "w") {
		weeks := strings.TrimSuffix(since, "w")
		var w int
		if _, err := fmt.Sscanf(weeks, "%d", &w); err != nil {
			return time.Time{}, skillerr.WrapValidation("modified_since must be in form Nd, Nw, Nm, or Ny", err, skillerr.WithHint(hint))
		}
		return now.AddDate(0, 0, -w*7), nil
	}
	if strings.HasSuffix(since, "m") {
		months := strings.TrimSuffix(since, "m")
		var m int
		if _, err := fmt.Sscanf(months, "%d", &m); err != nil {
			return time.Time{}, skillerr.WrapValidation("modified_since must be in form Nd, Nw, Nm, or Ny", err, skillerr.WithHint(hint))
		}
		return now.AddDate(0, -m, 0), nil
	}
	if strings.HasSuffix(since, "y") {
		years := strings.TrimSuffix(since, "y")
		var y int
		if _, err := fmt.Sscanf(years, "%d", &y); err != nil {
			return time.Time{}, skillerr.WrapValidation("modified_since must be in form Nd, Nw, Nm, or Ny", err, skillerr.WithHint(hint))
		}
		return now.AddDate(-y, 0, 0), nil
	}

	return time.Time{}, skillerr.Validation("modified_since must be in form Nd, Nw, Nm, or Ny", skillerr.WithHint(hint))
}

// fuzzyScore calculates match score and identifies match types for fuzzy search.
func fuzzyScore(query, path, name string) (float64, []string) {
	query = strings.ToLower(query)
	pathLower := strings.ToLower(path)
	nameLower := strings.ToLower(name)

	var score float64
	var matches []string

	// Exact name match (highest score)
	if nameLower == query {
		score += 100
		matches = append(matches, "exact_name")
	}

	// Name contains query
	if strings.Contains(nameLower, query) {
		score += 50
		matches = append(matches, "name_contains")
	}

	// Path contains query
	if strings.Contains(pathLower, query) {
		score += 30
		matches = append(matches, "path_contains")
	}

	// Name starts with query
	if strings.HasPrefix(nameLower, query) {
		score += 40
		matches = append(matches, "name_prefix")
	}

	// Fuzzy match on initials (e.g., "mp" matches "main.py")
	if fuzzyInitials(query, nameLower) {
		score += 20
		matches = append(matches, "fuzzy_initials")
	}

	// Word boundary matching
	words := strings.Fields(strings.ReplaceAll(pathLower, "/", " "))
	for _, word := range words {
		if strings.HasPrefix(word, query) {
			score += 15
			matches = append(matches, "word_prefix")
			break
		}
	}

	return score, matches
}

// fuzzyInitials checks if query matches initials of target string.
func fuzzyInitials(query, target string) bool {
	if len(query) == 0 {
		return false
	}

	queryIdx := 0
	for _, ch := range target {
		if queryIdx < len(query) && ch == rune(query[queryIdx]) {
			queryIdx++
		}
		if queryIdx == len(query) {
			return true
		}
	}

	return queryIdx == len(query)
}

// sortResults sorts file results by specified criteria.
func sortResults(results []fileResult, sortBy string) {
	switch sortBy {
	case "name":
		sort.Slice(results, func(i, j int) bool {
			return results[i].Name < results[j].Name
		})
	case "size":
		sort.Slice(results, func(i, j int) bool {
			return results[i].Size > results[j].Size
		})
	case "modified":
		sort.Slice(results, func(i, j int) bool {
			return results[i].ModifiedUnix > results[j].ModifiedUnix
		})
	case "relevance":
		sort.Slice(results, func(i, j int) bool {
			if results[i].Score == results[j].Score {
				return results[i].Name < results[j].Name
			}
			return results[i].Score > results[j].Score
		})
	}
}

// getFileType determines if a directory entry is a file or directory.
func getFileType(d fs.DirEntry) string {
	if d.IsDir() {
		return "directory"
	}
	return "file"
}
